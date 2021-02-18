// Reverse proxies traffic to a set of origins. Probably the most powerful building block of Edgerouter -
// used as backend for Docker discoveries, S3 static websites, fronting S3 buckets etc.
package reverseproxybackend

import (
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cozy/httpcache"
	"github.com/cozy/httpcache/diskcache"
	"github.com/function61/edgerouter/pkg/erconfig"
)

// using fork of gregjones/httpcache because the project is "done" and it disastrously caches 304
// responses. replication:
//   1) request something that goes in cache
//   2) stop Edgerouter, empty cache. start Edgerouter
//   3) press F5 from browser. this'll inject 304 Not Modified into cache (browser expects 304 but CACHE NOT)
//   4) now use cURL to request the same resource (= without caching), and you'll get 304 🤦

func init() {
	rand.Seed(time.Now().Unix())
}

func New(appId string, opts erconfig.BackendOptsReverseProxy) (http.Handler, error) {
	originUrls, err := parseOriginUrls(opts.Origins) // guarantees >= 1 items
	if err != nil {
		return nil, fmt.Errorf("reverseproxybackend: %w", err)
	}

	// transport that has optional TLS customizations and maybe caching (depending on options)
	transport, err := maybeWrapWithCache(appId, opts, func() http.RoundTripper {
		if opts.TlsConfig != nil { // got custom TLS config?
			return &http.Transport{
				TLSClientConfig: &tls.Config{
					ServerName:         opts.TlsConfig.ServerName,
					InsecureSkipVerify: opts.TlsConfig.InsecureSkipVerify,
				},
			}
		} else {
			return http.DefaultTransport
		}
	}())
	if err != nil {
		return nil, err
	}

	return &httputil.ReverseProxy{
		Transport: transport,
		Director: func(req *http.Request) {
			randomOriginIdx := rand.Intn(len(originUrls))

			originUrl := originUrls[randomOriginIdx]

			maybeIndexSuffix := func() string { // "/foo/" => "/foo/index.html" (if configured)
				if opts.IndexDocument != "" && strings.HasSuffix(req.URL.Path, "/") {
					return opts.IndexDocument
				} else {
					return ""
				}
			}()

			req.URL.Scheme = originUrl.Scheme // "http" | "https"

			// this specifies the host we're connecting to
			req.URL.Host = originUrl.Host

			// sometimes we want the outgoing request to include the original "Host: ..." header, so
			// the backend can see what hostname is in browser's address bar
			if !opts.PassHostHeader {
				req.Host = originUrl.Host
			}

			// origin's Path is "normally" empty (e.g. "http://example.com"), but can be used to add a prefix
			req.URL.Path = originUrl.Path + req.URL.Path + maybeIndexSuffix

			// remove query string if we know we're serving static content and the output does
			// not vary based on query string. someone malicious could even be trying to flood our
			// origin with requests knowing varying the query is a cache miss
			if opts.RemoveQueryString {
				req.URL.RawQuery = ""
			}
		},
	}, nil
}

func maybeWrapWithCache(
	appId string,
	opts erconfig.BackendOptsReverseProxy,
	inner http.RoundTripper,
) (http.RoundTripper, error) {
	if !opts.Caching {
		return inner, nil
	}

	// there's no abstraction for getting system-level cache dir in Go
	cacheLocation := filepath.Join("/var/cache/edgerouter", appId)

	if err := os.MkdirAll(cacheLocation, 0700); err != nil {
		return nil, fmt.Errorf("cachingreverseproxy: %w", err)
	}

	cache := httpcache.NewTransport(diskcache.New(cacheLocation))
	cache.Transport = inner
	cache.MarkCachedResponses = true // (for debugging) X-From-Cache header

	return cache, nil
}

func parseOriginUrls(originUrlStrs []string) ([]url.URL, error) {
	originUrls := []url.URL{}

	for _, originUrlStr := range originUrlStrs {
		originUrl, err := url.Parse(originUrlStr)
		if err != nil {
			return nil, err
		}

		originUrls = append(originUrls, *originUrl)
	}

	if len(originUrls) == 0 {
		return nil, errors.New("empty origin list")
	}

	return originUrls, nil
}
