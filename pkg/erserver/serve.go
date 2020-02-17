package erserver

import (
	"context"
	"crypto/tls"
	"github.com/felixge/httpsnoop"
	"github.com/function61/certbus/pkg/certbus"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/s3discovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/swarmdiscovery"
	"github.com/function61/edgerouter/pkg/httputils"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/gokit/envvar"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/taskrunner"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
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

	// initial sync so we won't start dealing out 404s when HTTP server starts
	initialConfig, err := syncAppsFromDiscovery(ctx, discovery, logl)
	if err != nil {
		// not treating this as a fatal error though
		logl.Error.Printf("initial sync failed: %v", err)

		initialConfig = newFrontendMatchers([]erconfig.Application{})
	}

	atomicConfig := atomic.Value{}
	atomicConfig.Store(initialConfig)

	serveRequest := func(w http.ResponseWriter, r *http.Request) *Mount {
		// load latest config in threadsafe manner
		config := atomicConfig.Load().(*frontendMatchers)

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

		if mount.stripPrefix {
			r.URL.Path = r.URL.Path[len(mount.prefix):]

			if !strings.HasPrefix(r.URL.Path, "/") {
				r.URL.Path = "/" + r.URL.Path
			}
		}

		mount.backend.Serve(w, r)

		return mount
	}

	srv := &http.Server{
		Addr: ":443",
		TLSConfig: &tls.Config{
			// this integrates CertBus into your server - certificates are fetched
			// dynamically from CertBus's dynamically managed state
			GetCertificate: certBus.GetCertificateAdapter(),
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var mount *Mount
			// see for greatly written rationale https://github.com/felixge/httpsnoop
			// tl;dr: response snooping is hard without losing Websocket etc. support
			stats := httpsnoop.CaptureMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mount = serveRequest(w, r)
			}), w, r)

			appId := func() string {
				if mount == nil {
					return "appNotFound"
				} else {
					return mount.App.Id
				}
			}()

			if stats.Code < 400 {
				incAppCodeMethodCounter(metrics.requestsOk, appId, strconv.Itoa(stats.Code), r.Method)
			} else {
				incAppCodeMethodCounter(metrics.requestsFail, appId, strconv.Itoa(stats.Code), r.Method)
			}

			metrics.requestDuration.WithLabelValues(appId).Observe(stats.Duration.Seconds())
			metrics.requestDuration.WithLabelValues(allAppKey).Observe(stats.Duration.Seconds())
		}),
	}

	configUpdated := make(chan *frontendMatchers, 1)

	tasks := taskrunner.New(ctx, logger)

	tasks.Start("listener "+srv.Addr, func(_ context.Context, _ string) error {
		return httputils.RemoveGracefulServerClosedError(srv.ListenAndServeTLS("", ""))
	})

	tasks.Start("listenershutdowner", httputils.ServerShutdownTask(srv))

	tasks.Start("certbus sync", func(ctx context.Context, _ string) error { return certBus.Synchronizer(ctx) })

	tasks.Start("configsyncscheduler", func(ctx context.Context, taskName string) error {
		return scheduledSync(
			ctx,
			discovery,
			configUpdated,
			logex.Prefix(taskName, logger))
	})

	for {
		select {
		case err := <-tasks.Done():
			return err
		case config := <-configUpdated:
			atomicConfig.Store(config)
		}
	}
}

func syncAppsFromDiscovery(
	ctx context.Context,
	discovery erdiscovery.Reader,
	logl *logex.Leveled,
) (*frontendMatchers, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	apps, err := discovery.ReadApplications(ctx)
	if err != nil {
		return nil, err
	}

	logl.Info.Printf("discovered %d app(s)", len(apps))

	return appsToFrontendMatchers(apps)
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

	if swarmdiscovery.HasConfigInEnv() {
		swarmDiscovery, err := swarmdiscovery.New()
		if err != nil {
			return nil, err
		}

		readers = append(readers, swarmDiscovery)
	}

	defaultDiscovery, err := defaultdiscovery.New(logger)
	if err != nil {
		return nil, err
	}

	readers = append(readers, defaultDiscovery)

	return erdiscovery.MultiDiscovery(readers), nil
}

func makeCertBus(ctx context.Context, logger *log.Logger) (*certbus.App, error) {
	// loadbalancer's CertBus private key for which the certificate private keys are encrypted
	certBusPrivateKey, err := envvar.RequiredFromBase64Encoded("CERTBUS_CLIENT_PRIVKEY")
	if err != nil {
		return nil, err
	}

	tenantCtx, err := ehreader.TenantConfigFromEnv()
	if err != nil {
		return nil, err
	}

	certBus, err := certbus.New(
		ctx,
		tenantCtx,
		string(certBusPrivateKey),
		logger)
	if err != nil {
		return nil, err
	}

	return certBus, nil
}
