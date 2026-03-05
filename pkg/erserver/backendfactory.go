package erserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/function61/edgerouter/pkg/erbackend/authssobackend"
	"github.com/function61/edgerouter/pkg/erbackend/authv0backend"
	"github.com/function61/edgerouter/pkg/erbackend/edgerouteradminbackend"
	"github.com/function61/edgerouter/pkg/erbackend/lambdabackend"
	"github.com/function61/edgerouter/pkg/erbackend/redirectbackend"
	"github.com/function61/edgerouter/pkg/erbackend/reverseproxybackend"
	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erbackend/turbochargerbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var bendCache = newBackendCache()

func makeBackend(
	ctx context.Context,
	appID string,
	backendConf erconfig.Backend,
	currentConfig erconfig.CurrentConfigAccessor,
	parentLogger *slog.Logger,
) (http.Handler, error) {
	configDigest, err := json.Marshal(backendConf)
	if err != nil {
		return nil, err
	}

	// only make new instance if config JSON has changed for this app ID
	cached := bendCache.Find(appID, configDigest)
	if cached == nil {
		backend, err := makeBackendInternal(ctx, appID, backendConf, currentConfig, parentLogger)
		if err != nil {
			return nil, err
		}

		cached = &cacheEntry{
			backend:      backend,
			configDigest: configDigest,
		}
		bendCache.perAppID[appID] = cached
	}

	return cached.backend, nil
}

// called when actually making a new backend, instead of using a cached one
func makeBackendInternal(
	ctx context.Context,
	appID string,
	backendConf erconfig.Backend,
	currentConfig erconfig.CurrentConfigAccessor,
	parentLogger *slog.Logger,
) (http.Handler, error) {
	appSpecificLogger := func() *slog.Logger { // helper
		return parentLogger.With("app", appID)
	}

	switch backendConf.Kind {
	case erconfig.BackendKindS3StaticWebsite:
		return statics3websitebackend.New(appID, *backendConf.S3StaticWebsiteOpts)
	case erconfig.BackendKindReverseProxy:
		return reverseproxybackend.New(ctx, appID, *backendConf.ReverseProxyOpts, appSpecificLogger())
	case erconfig.BackendKindAwsLambda:
		return lambdabackend.New(ctx, *backendConf.AwsLambdaOpts, appSpecificLogger())
	case erconfig.BackendKindRedirect:
		return redirectbackend.New(*backendConf.RedirectOpts), nil
	case erconfig.BackendKindTurbocharger:
		return turbochargerbackend.New(ctx, backendConf.TurbochargerOpts.Manifest, appSpecificLogger())
	case erconfig.BackendKindEdgerouterAdmin:
		return edgerouteradminbackend.New(currentConfig)
	case erconfig.BackendKindAuthV0:
		authorizedBackend, err := makeBackendInternal(
			ctx,
			appID,
			*backendConf.AuthV0Opts.AuthorizedBackend,
			currentConfig,
			parentLogger)
		if err != nil {
			return nil, fmt.Errorf("authorizedBackend: %w", err)
		}

		return authv0backend.New(*backendConf.AuthV0Opts, authorizedBackend), nil
	case erconfig.BackendKindAuthSso:
		authorizedBackend, err := makeBackendInternal(
			ctx,
			appID,
			*backendConf.AuthSsoOpts.AuthorizedBackend,
			currentConfig,
			parentLogger)
		if err != nil {
			return nil, fmt.Errorf("authorizedBackend: %w", err)
		}

		return authssobackend.New(*backendConf.AuthSsoOpts, authorizedBackend)
	case erconfig.BackendKindPromMetrics:
		return promhttp.Handler(), nil
	default:
		return nil, fmt.Errorf("unsupported backend kind: %s", backendConf.Kind)
	}
}

// we need this because if we'd make new instances all the time, reverseproxybackend f.ex. makes
// new http.Transport instance each time, so this would end up with loads of half-open TCP
// connections since the connection cache is per http.Transport
// NOTE: no need for locking, because makeBackend() is not called concurrently
type backendCache struct {
	// the cache data structure might seem unusual. that's because we don't want multiple cache
	// entries per one app - we want GC to be able to clean up no-longer-used handlers

	perAppID map[string]*cacheEntry
}

type cacheEntry struct {
	backend      http.Handler
	configDigest []byte
}

func newBackendCache() *backendCache {
	return &backendCache{
		perAppID: map[string]*cacheEntry{},
	}
}

func (b *backendCache) Find(appID string, configDigest []byte) *cacheEntry {
	cached, found := b.perAppID[appID]
	if !found {
		return nil
	}

	if !bytes.Equal(configDigest, cached.configDigest) {
		return nil
	}

	return cached
}
