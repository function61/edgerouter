package erserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/function61/edgerouter/pkg/erbackend/authssobackend"
	"github.com/function61/edgerouter/pkg/erbackend/authv0backend"
	"github.com/function61/edgerouter/pkg/erbackend/edgerouteradminbackend"
	"github.com/function61/edgerouter/pkg/erbackend/lambdabackend"
	"github.com/function61/edgerouter/pkg/erbackend/redirectbackend"
	"github.com/function61/edgerouter/pkg/erbackend/reverseproxybackend"
	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var bendCache = newBackendCache()

func makeBackend(
	appId string,
	backendConf erconfig.Backend,
	currentConfig erconfig.CurrentConfigAccessor,
) (http.Handler, error) {
	configDigest, err := json.Marshal(backendConf)
	if err != nil {
		return nil, err
	}

	// only make new instance if config JSON has changed for this app ID
	cached := bendCache.Find(appId, configDigest)
	if cached == nil {
		backend, err := makeBackendInternal(appId, backendConf, currentConfig)
		if err != nil {
			return nil, err
		}

		cached = &cacheEntry{
			backend:      backend,
			configDigest: configDigest,
		}
		bendCache.perAppId[appId] = cached
	}

	return cached.backend, nil
}

// called when actually making a new backend, instead of using a cached one
func makeBackendInternal(
	appId string,
	backendConf erconfig.Backend,
	currentConfig erconfig.CurrentConfigAccessor,
) (http.Handler, error) {
	switch backendConf.Kind {
	case erconfig.BackendKindS3StaticWebsite:
		return statics3websitebackend.New(appId, *backendConf.S3StaticWebsiteOpts)
	case erconfig.BackendKindReverseProxy:
		return reverseproxybackend.New(appId, *backendConf.ReverseProxyOpts)
	case erconfig.BackendKindAwsLambda:
		return lambdabackend.New(*backendConf.AwsLambdaOpts)
	case erconfig.BackendKindRedirect:
		return redirectbackend.New(*backendConf.RedirectOpts), nil
	case erconfig.BackendKindEdgerouterAdmin:
		return edgerouteradminbackend.New(currentConfig)
	case erconfig.BackendKindAuthV0:
		authorizedBackend, err := makeBackendInternal(
			appId,
			*backendConf.AuthV0Opts.AuthorizedBackend,
			currentConfig)
		if err != nil {
			return nil, fmt.Errorf("authorizedBackend: %w", err)
		}

		return authv0backend.New(*backendConf.AuthV0Opts, authorizedBackend), nil
	case erconfig.BackendKindAuthSso:
		authorizedBackend, err := makeBackendInternal(
			appId,
			*backendConf.AuthSsoOpts.AuthorizedBackend,
			currentConfig)
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

	perAppId map[string]*cacheEntry
}

type cacheEntry struct {
	backend      http.Handler
	configDigest []byte
}

func newBackendCache() *backendCache {
	return &backendCache{
		perAppId: map[string]*cacheEntry{},
	}
}

func (b *backendCache) Find(appId string, configDigest []byte) *cacheEntry {
	cached, found := b.perAppId[appId]
	if !found {
		return nil
	}

	if !bytes.Equal(configDigest, cached.configDigest) {
		return nil
	}

	return cached
}
