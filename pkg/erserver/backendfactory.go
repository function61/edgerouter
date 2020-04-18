package erserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/function61/edgerouter/pkg/erbackend/authv0backend"
	"github.com/function61/edgerouter/pkg/erbackend/lambdabackend"
	"github.com/function61/edgerouter/pkg/erbackend/peersetbackend"
	"github.com/function61/edgerouter/pkg/erbackend/redirectbackend"
	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erconfig"
)

var bendCache = newBackendCache()

// TODO: make "fem" parameteter unnecessary
func makeBackend(
	appId string,
	backendConf erconfig.Backend,
	fem *frontendMatchers,
) (http.Handler, error) {
	configDigest, err := json.Marshal(backendConf)
	if err != nil {
		return nil, err
	}

	// only make new instance if config JSON has changed for this app ID
	cached := bendCache.Find(appId, configDigest)
	if cached == nil {
		backend, err := makeBackendInternal(appId, backendConf, fem)
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

func makeBackendInternal(appId string, backendConf erconfig.Backend, fem *frontendMatchers) (http.Handler, error) {
	switch backendConf.Kind {
	case erconfig.BackendKindS3StaticWebsite:
		return statics3websitebackend.New(appId, *backendConf.S3StaticWebsiteOpts), nil
	case erconfig.BackendKindPeerSet:
		return peersetbackend.New(*backendConf.PeerSetOpts)
	case erconfig.BackendKindAwsLambda:
		return lambdabackend.New(*backendConf.AwsLambdaOpts)
	case erconfig.BackendKindRedirect:
		return redirectbackend.New(*backendConf.RedirectOpts), nil
	case erconfig.BackendKindEdgerouterAdmin:
		return newAdminBackend(fem), nil
	case erconfig.BackendKindAuthV0:
		authorizedBackend, err := makeBackendInternal(
			appId,
			backendConf.AuthV0Opts.AuthorizedBackend,
			fem)
		if err != nil {
			return nil, fmt.Errorf("authorizedBackend: %w", err)
		}

		return authv0backend.New(*backendConf.AuthV0Opts, authorizedBackend), nil
	default:
		return nil, fmt.Errorf("unsupported backend kind: %s", backendConf.Kind)
	}
}

// we need this because if we'd make new instances all the time, peersetbackend f.ex. makes
// new http.Transport instance each time, so this would end up with loads of half-open TCP
// connections since the connection cache is per http.Transport
// NOTE: no need for locking, because makeBackend() is not called concurrently
type backendCache struct {
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
