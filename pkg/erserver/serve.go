// Actual server implementation of Edgerouter
package erserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/function61/certbus/pkg/certbus"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/dockerdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/ehdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/s3discovery"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/gokit/envvar"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/taskrunner"
)

func Serve(ctx context.Context, logger *log.Logger) error {
	logl := logex.Levels(logger)

	metrics := initMetrics()

	certBus, err := makeCertBus(ctx, logex.Prefix("certbus", logger))
	if err != nil {
		return err
	}

	discovery, err := configureDiscovery(logger)
	if err != nil {
		return err
	}

	currentConfig := newAtomicConfig()

	// initial sync so we won't start dealing out 404s when HTTP server starts
	initialConfig, err := syncAppsFromDiscovery(ctx, discovery, currentConfig, logl)
	if err != nil {
		// not treating this as a fatal error though
		logl.Error.Printf("initial sync failed: %v", err)
	} else {
		currentConfig.Store(initialConfig)
	}

	// TODO: if these rules have syntax error, it'd be good if it came up before other async tasks
	//       are started.
	ipRules, err := loadIpRules()
	if err != nil {
		return err
	}

	// returns mount (i.e. application) that the URL matched.
	// NOTE: does not imply the request entered the application (e.g. IP filtering or HTTPS-only rule might've blocked the request)
	// nil mount if no URL matched means "no application found"
	serveRequest := func(w http.ResponseWriter, r *http.Request) *Mount {
		// load latest config in threadsafe manner
		config := currentConfig.Load().(*frontendMatchers)

		hostname, _, err := nonStupidSplitHostPort(r.Host)
		if err != nil {
			http.Error(w, "failed to parse hostname header: "+err.Error(), http.StatusBadRequest)
			return nil
		}

		mount := resolveMount(hostname, r.URL.Path, config)
		if mount == nil {
			http.Error(w, "no website for hostname: "+hostname, http.StatusNotFound)
			return nil
		}

		notSecure := r.TLS == nil

		if notSecure && !mount.allowInsecureHTTP { // important that this is done before stripPrefix
			redirectHTTPToHTTPS(w, r) // come back when you have TLS, bro
			return mount
		}

		// todo: respect x-forwarded-for headers but only if configured as trusted
		if allowed, errStr := ipAllowed(r.RemoteAddr, mount.App.Id, ipRules); !allowed {
			http.Error(w, errStr, http.StatusForbidden)
			return mount
		}

		if mount.stripPrefix {
			// path=/files/foobar.txt stripPrefix=/files/
			// => "foobar.txt"
			newPath := r.URL.Path[len(mount.prefix):]
			if !strings.HasPrefix(newPath, "/") { // "foobar.txt" => "/foobar.txt"
				newPath = "/" + newPath
			}

			r.URL.Path = newPath

		}

		mount.backend.ServeHTTP(w, r)

		return mount
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var mount *Mount
		// see for greatly written rationale https://github.com/felixge/httpsnoop
		// tl;dr: response snooping is hard without losing Websocket etc. support
		stats := httpsnoop.CaptureMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mount = serveRequest(w, r)
		}), w, r)

		appId := func() string {
			if mount != nil {
				return mount.App.Id
			} else {
				return "appNotFound"
			}
		}()

		if stats.Code < 400 {
			incAppCodeMethodCounter(metrics.requestsOk, appId, strconv.Itoa(stats.Code), r.Method)
		} else {
			incAppCodeMethodCounter(metrics.requestsFail, appId, strconv.Itoa(stats.Code), r.Method)
		}

		metrics.requestDuration.WithLabelValues(appId).Observe(stats.Duration.Seconds())
		metrics.requestDuration.WithLabelValues(allAppKey).Observe(stats.Duration.Seconds())
	})

	configUpdated := make(chan *frontendMatchers, 1)

	tasks := taskrunner.New(ctx, logger)
	tasks.Start("listener :443", func(ctx context.Context) error {
		srv := &http.Server{
			Addr: ":443",
			TLSConfig: &tls.Config{
				// this integrates CertBus into your server - certificates are fetched
				// dynamically from CertBus's dynamically managed state
				GetCertificate: certBus.GetCertificateAdapter(),
			},
			Handler: handler,
		}

		return cancelableServer(ctx, srv, func() error { return srv.ListenAndServeTLS("", "") })
	})

	tasks.Start("listener :80", func(ctx context.Context) error {
		srv := &http.Server{
			Addr:    ":80",
			Handler: handler,
		}

		return cancelableServer(ctx, srv, srv.ListenAndServe)
	})

	tasks.Start("certbus sync", func(ctx context.Context) error { return certBus.Synchronizer(ctx) })

	tasks.Start("configsyncscheduler", func(ctx context.Context) error {
		return scheduledSync(
			ctx,
			discovery,
			configUpdated,
			currentConfig,
			logex.Prefix("configsyncscheduler", logger))
	})

	for {
		select {
		case err := <-tasks.Done():
			return err
		case config := <-configUpdated:
			currentConfig.Store(config)
		}
	}
}

func syncAppsFromDiscovery(
	ctx context.Context,
	discovery erdiscovery.Reader,
	currentConfig erconfig.CurrentConfigAccessor,
	logl *logex.Leveled,
) (*frontendMatchers, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	apps, err := discovery.ReadApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("ReadApplications: %w", err)
	}

	if metricsEndpoint := os.Getenv("METRICS_ENDPOINT"); metricsEndpoint != "" {
		prom := erconfig.SimpleApplication(
			"prom-metrics",
			erconfig.CatchAllHostnamesFrontend(erconfig.PathPrefix(metricsEndpoint), erconfig.AllowInsecureHTTP),
			erconfig.PromMetricsBackend())

		apps = append(apps, prom)
	}

	logl.Info.Printf("discovered %d app(s)", len(apps))

	matchers, err := appConfigToHandlersAndMatchers(apps, currentConfig, time.Now())
	if err != nil {
		return nil, fmt.Errorf("appConfigToHandlersAndMatchers: %w", err)
	}

	return matchers, nil
}

func configureDiscovery(logger *log.Logger) (erdiscovery.Reader, error) {
	readers := []erdiscovery.Reader{}

	if s3discovery.HasConfigInEnv() {
		s3Discovery, err := s3discovery.New()
		if err != nil {
			return nil, err
		}

		readers = append(readers, s3Discovery)
	}

	if dockerdiscovery.HasConfigInEnv() {
		docker, err := dockerdiscovery.New()
		if err != nil {
			return nil, err
		}

		readers = append(readers, docker)
	}

	if ehdiscovery.HasConfigInEnv() {
		tenantCtx, err := ehreader.TenantCtxFrom(ehreader.ConfigFromEnv)
		if err != nil {
			return nil, fmt.Errorf("ehdiscovery: %w", err)
		}

		ehDiscovery, err := ehdiscovery.New(*tenantCtx, logex.Prefix("ehdiscovery", logger))
		if err != nil {
			return nil, fmt.Errorf("ehdiscovery: %w", err)
		}

		readers = append(readers, ehDiscovery)
	}

	return erdiscovery.MultiDiscovery(readers), nil
}

func makeCertBus(ctx context.Context, logger *log.Logger) (*certbus.App, error) {
	// loadbalancer's CertBus private key for which the certificate private keys are encrypted
	certBusPrivateKey, err := envvar.RequiredFromBase64Encoded("CERTBUS_CLIENT_PRIVKEY")
	if err != nil {
		return nil, err
	}

	tenantCtx, err := ehreader.TenantCtxFrom(ehreader.ConfigFromEnv)
	if err != nil {
		return nil, err
	}

	certBus, err := certbus.New(
		ctx,
		*tenantCtx,
		string(certBusPrivateKey),
		logger)
	if err != nil {
		return nil, err
	}

	return certBus, nil
}

type atomicConfig struct {
	atomic.Value // stores *frontendMatchers
}

var _ erconfig.CurrentConfigAccessor = (*atomicConfig)(nil)

func newAtomicConfig() *atomicConfig {
	// dummy value for "not updated yet"
	year2000 := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) // youtu.be/kmzpdd4pWvM
	a := &atomicConfig{}
	a.Store(newFrontendMatchers([]erconfig.Application{}, year2000)) // start with empty
	return a
}

// atomically read configuration at any time
func (a *atomicConfig) Apps() []erconfig.Application {
	return a.Load().(*frontendMatchers).Apps
}

func (a *atomicConfig) LastUpdated() time.Time {
	return a.Load().(*frontendMatchers).timestamp
}

func redirectHTTPToHTTPS(w http.ResponseWriter, r *http.Request) {
	target := "https://" + r.Host + r.URL.Path
	if len(r.URL.RawQuery) > 0 {
		target += "?" + r.URL.RawQuery
	}

	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}
