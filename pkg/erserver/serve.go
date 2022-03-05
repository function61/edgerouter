// Actual server implementation of Edgerouter
package erserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/function61/edgerouter/pkg/erdiscovery/filediscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/s3discovery"
	"github.com/function61/edgerouter/pkg/todoupgradegokit"
	"github.com/function61/edgerouter/pkg/turbocharger"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/gokit/envvar"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/taskrunner"
)

const (
	DefaultConfigDir ConfigDir = "/etc/edgerouter"
)

type GetCertificateFn func(*tls.ClientHelloInfo) (*tls.Certificate, error)

func Serve(ctx context.Context, configDir ConfigDir, logger *log.Logger) error {
	logl := logex.Levels(logger)

	metrics := initMetrics()

	waitAlreadyDoneFIXMENOTNEEDEDLONG := false

	tasksCtx, tasksCancel := context.WithCancel(ctx)
	tasks := taskrunner.New(tasksCtx, logger)
	defer func() {
		// this defer is only needed for early exits to stop certbus sync task. if we exit from happy
		// path at bottom of this fn, this is not needed (but does not hurt to run twice)

		if waitAlreadyDoneFIXMENOTNEEDEDLONG {
			return
		}

		// this currently has a bug which is fixed when we can update to new gokit
		if err := tasks.Wait(); err != nil {
			logl.Error.Printf("taskrunner early-exit Wait(): %v", err)
		}
	}()
	defer tasksCancel()

	getCertificateFn, err := func() (GetCertificateFn, error) {
		if os.Getenv("CERTBUS_CLIENT_PRIVKEY") != "" {
			certBus, err := makeCertBus(ctx, logex.Prefix("certbus", logger))
			if err != nil {
				return nil, err
			}

			tasks.Start("certbus sync", func(ctx context.Context) error { return certBus.Synchronizer(ctx) })

			return certBus.GetCertificateAdapter(), nil
		} else {
			logl.Info.Printf("CertBus not configured - assuming local dev-server & using %s", configDir.DevelopmentCertificate())

			// this is expected to be configured with mkcert or similar
			keyPair, err := tls.LoadX509KeyPair(configDir.DevelopmentCertificate(), configDir.DevelopmentCertificate())
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf(
						"certificate not found - run '$ %s setup-devcerts' first: %w",
						os.Args[0],
						err)
				} else {
					return nil, err
				}
			}

			// we're assuming the user only needs one hostname or that it's a wildcard certificate
			return alwaysReturnSameCertificate(&keyPair), nil
		}
	}()
	if err != nil {
		return err
	}

	discovery, err := configureDiscovery(logger)
	if err != nil {
		return fmt.Errorf("configureDiscovery: %w", err)
	}

	currentConfig := newAtomicConfig()

	// initial sync so we won't start dealing out 404s when HTTP server starts
	initialConfig, err := syncAppsFromDiscovery(ctx, discovery, currentConfig, logger, logl)
	if err != nil {
		// not treating this as a fatal error though
		logl.Error.Printf("initial sync failed: %v", err)
	} else {
		currentConfig.Store(initialConfig)
	}

	// TODO: if these rules have syntax error, it'd be good if it came up before other async tasks
	//       are started.
	//
	// file is a temporary solution - these will have to live in EventHorizon
	ipRules, err := loadIpRules("/etc/edgerouter/ip-rules.hcl")
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

		// pass the request to the concrete application where the actually interesting things happen.
		// the path (reversed) looks like this:
		//
		// Application
		// └── serveRequest (app routing/resolving, HTTP-to-HTTPS redirection, IP filtering)
		//     └── serveRequestWithMetricsCapture
		//         ├── listener :443
		//         └── listener :80
		mount.backend.ServeHTTP(w, r)

		return mount
	}

	// shared handler for both HTTPS and HTTP
	serveRequestWithMetricsCapture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	logl.Info.Printf("turbocharger middleware activated=%v", turbocharger.MiddlewareConfigAvailable())

	configUpdated := make(chan *frontendMatchers, 1)

	tasks.Start("listener :443", func(ctx context.Context) error {
		srv := &http.Server{
			Addr: ":443",
			// lint complains about too low MinVersion (the default, in Go sets it as TLS 1.0).
			// purposefully leaving MinVersion as default because I feel Go stdlib's default MinVersion
			// in the long run aligns with loadbalancer use case of conservatively having to support a wide base of users.
			// https://developers.cloudflare.com/ssl/edge-certificates/additional-options/minimum-tls#decide-what-version-to-use
			//
			//nolint:gosec // rationale above
			TLSConfig: &tls.Config{
				// MinVersion: ... // purposefully unset to follow Go stdlib MinVersion
				GetCertificate: getCertificateFn,
			},
			Handler:           serveRequestWithMetricsCapture,
			ReadHeaderTimeout: todoupgradegokit.DefaultReadHeaderTimeout,
		}

		return cancelableServer(ctx, srv, func() error { return srv.ListenAndServeTLS("", "") })
	})

	tasks.Start("listener :80", func(ctx context.Context) error {
		srv := &http.Server{
			Addr:              ":80",
			Handler:           serveRequestWithMetricsCapture,
			ReadHeaderTimeout: todoupgradegokit.DefaultReadHeaderTimeout,
		}

		return cancelableServer(ctx, srv, srv.ListenAndServe)
	})

	tasks.Start("configsyncscheduler", func(ctx context.Context) error {
		return scheduledSync(
			ctx,
			discovery,
			configUpdated,
			currentConfig,
			logger,
			logex.Prefix("configsyncscheduler", logger))
	})

	for {
		select {
		case err := <-tasks.Done():
			waitAlreadyDoneFIXMENOTNEEDEDLONG = true
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
	parentLogger *log.Logger,
	logl *logex.Leveled,
) (*frontendMatchers, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	apps, err := discovery.ReadApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("ReadApplications: %w", err)
	}

	if metricsEndpoint := os.Getenv("METRICS_ENDPOINT"); metricsEndpoint != "" {
		// rationale for "don't care about hostname" -frontend: Prometheus usually autodiscovers its
		// targets and usually container-based autodiscovery's primary currency is IP addresses.
		// in container land IP addresses are mostly dynamic, so we'll expect a config like this:
		// METRICS_ENDPOINT=/.edgerouter/metrics/QSuJqc6YY-H-5T4y and the random-looking token
		// makes sure we use unique endpoint AND conveniently also acts as an auth token in URL
		// (which is safer b/c while Prometheus supports explicit auth token but its config data format
		//  makes it close to impossible to have per-target tokens so it prefers to spray the same
		//  shared token everywhere.)
		prom := erconfig.SimpleApplication(
			"prom-metrics",
			erconfig.PathPrefixFrontend(metricsEndpoint, erconfig.AllowInsecureHTTP),
			erconfig.PromMetricsBackend())

		apps = append(apps, prom)
	}

	logl.Info.Printf("discovered %d app(s)", len(apps))

	matchers, err := appConfigToHandlersAndMatchers(apps, currentConfig, time.Now(), parentLogger)
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

	maybeFromFile, err := newFileDiscoveryIfFileExists(filediscovery.DefaultFilename)
	if err != nil {
		return nil, err
	}

	if maybeFromFile != nil {
		readers = append(readers, maybeFromFile)
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

func alwaysReturnSameCertificate(keyPair *tls.Certificate) GetCertificateFn {
	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return keyPair, nil
	}
}

type ConfigDir string

func (c ConfigDir) DevelopmentCertificate() string {
	return c.File("dev-cert.pem")
}

func (c ConfigDir) String() string {
	return string(c)
}

// makes path to any file in the configuration directory
// use sparingly
func (c ConfigDir) File(filename string) string {
	return filepath.Join(string(c), filename)
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
