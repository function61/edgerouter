// Great use case: act as a front for S3 bucket and cache its responses (S3 bandwidth is very expensive)
package cachingreverseproxybackend

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
)

func New(appId string, opts erconfig.BackendOptsCachingReverseProxy) (http.Handler, error) {
	originUrl, err := url.Parse(opts.Origin)
	if err != nil {
		return nil, fmt.Errorf("cachingreverseproxy: %w", err)
	}

	// there's no abstraction for getting system-level cache dir in Go
	cacheLocation := filepath.Join("/var/cache/edgerouter", appId)

	if err := os.MkdirAll(cacheLocation, 0700); err != nil {
		return nil, fmt.Errorf("cachingreverseproxy: %w", err)
	}

	cache := httpcache.NewTransport(diskcache.New(cacheLocation))
	cache.MarkCachedResponses = true

	return &httputil.ReverseProxy{
		Transport: cache,
		Director: func(req *http.Request) {
			req.URL.Scheme = originUrl.Scheme
			req.URL.Host = originUrl.Host
			req.Host = req.URL.Host // needs to be present both in the URL and the request

			// origin's Path is "normally" empty, but can be used to add prefix
			req.URL.Path = originUrl.Path + req.URL.Path

			// remove query string, because we know we're serving static content and the output does
			// not vary based on query string. someone malicious could even be trying to flood our
			// origin with requests knowing varying the query is a cache miss
			req.URL.RawQuery = ""
		},
	}, nil
}
