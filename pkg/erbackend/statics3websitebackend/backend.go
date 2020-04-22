// Serves an atomically deployed website from S3
package statics3websitebackend

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/ezhttp"
)

type s3Backend struct {
	appId                    string
	opts                     erconfig.BackendOptsS3StaticWebsite
	expectedETag             string
	reverseProxy             *httputil.ReverseProxy
	custom404PageCached      []byte // only fetch once
	custom404PageContentType string
	custom404PageCachedMu    sync.Mutex
}

func New(appId string, opts erconfig.BackendOptsS3StaticWebsite) http.Handler {
	var s *s3Backend
	s = &s3Backend{
		expectedETag: makeETag(opts.DeployedVersion),
		appId:        appId,
		opts:         opts,
		reverseProxy: &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				if s.opts.DeployedVersion == "" {
					r.URL = nil
					log.Printf("no deployed version for %s", appId)
					return
				}

				// "/favicon.ico" =>
				//   https://s3.us-east-1.amazonaws.com/myorg-websites/sites/joonasfi-blog/versionid/favicon.ico
				bucketUrl, err := url.Parse(s.reroute(r.URL.Path))
				if err != nil {
					panic(err)
				}

				r.Host = bucketUrl.Host
				r.URL = bucketUrl
			},
			ModifyResponse: func(resp *http.Response) error {
				switch resp.StatusCode {
				case http.StatusOK, http.StatusNotModified:
					// "Note that the server generating a 304 response MUST generate any of
					// the following header fields that would have been sent in a 200 (OK)
					// response to the same request: Cache-Control, Content-Location, Date,
					// ETag, Expires, and Vary."
					resp.Header.Set("ETag", s.expectedETag)
				case http.StatusNotFound:
					// S3 404s look like:
					// <Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message><Key>sites/joonasfi-blog/2019-01-11/404</Key><RequestId>...</RequestId><HostId>...</HostId></Error>
					//
					// which reveals too much information => rewrite them
					notFoundContentType, notFoundPage := s.getCached404Page(resp.Request.Context())

					resp.Header.Set("Content-Type", notFoundContentType)
					resp.Body = notFoundPage
				}

				return nil
			},
		},
	}

	return s
}

func (s *s3Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// cache hit?
	// the header can list many ETags. instead of parsing it, string search is fine
	// because ETags are enclosed in quotes
	if strings.Contains(r.Header.Get("If-None-Match"), s.expectedETag) {
		w.Header().Set("ETag", s.expectedETag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	s.reverseProxy.ServeHTTP(w, r)
}

func (s *s3Backend) reroute(path string) string {
	// looks like "sites/joonasfi-blog/versionid"
	pathPrefix := bucketPrefix(s.appId, s.opts.DeployedVersion)

	// "/favicon.ico" => "sites/joonasfi-blog/versionid/favicon.ico"
	rerouted := pathPrefix + path

	// "/" => "/index.html"
	// "/foo/bar/" => "/foo/bar/index.html"
	if strings.HasSuffix(rerouted, "/") {
		rerouted += "index.html"
	}

	return "https://s3." + s.opts.RegionId + ".amazonaws.com/" + s.opts.BucketName + "/" + rerouted
}

func (s *s3Backend) getCached404Page(ctx context.Context) (string, io.ReadCloser) {
	s.custom404PageCachedMu.Lock()
	defer s.custom404PageCachedMu.Unlock()

	if s.custom404PageCached == nil { // cache miss
		s.custom404PageContentType, s.custom404PageCached = s.fetchCustom404Page(ctx)
	}

	return s.custom404PageContentType, ioutil.NopCloser(bytes.NewReader(s.custom404PageCached))
}

func (s *s3Backend) fetchCustom404Page(ctx context.Context) (string, []byte) {
	if s.opts.NotFoundPage == "" {
		return "text/plain", []byte("404 page not found")
	}

	// "404.html" => "/404.html" (no-op if already begins with slash)
	custom404PageFromRoot := "/" + strings.TrimPrefix(s.opts.NotFoundPage, "/")

	// return error heading (without detailed error) as 404 page (to cache).
	// not optimal but somewhat reasonable.
	handleError := func(msg string, err error) (string, []byte) {
		log.Printf("%s: %v", msg, err)

		return "text/plain", []byte(msg)
	}

	notFoundHtmlResponse, err := ezhttp.Get(ctx, s.reroute(custom404PageFromRoot))
	if err != nil {
		return handleError("error requesting 404 page", err)
	}
	defer notFoundHtmlResponse.Body.Close()

	notFoundHtml, err := ioutil.ReadAll(notFoundHtmlResponse.Body)
	if err != nil {
		return handleError("error reading 404 page", err)
	}

	return "text/html", notFoundHtml
}

// https://stackoverflow.com/questions/34846016/is-it-ok-for-several-paths-to-share-the-same-etag
func makeETag(deployedVersion string) string {
	// even if ETag syntax allowed us use deployedVersion as-is, best to hash it to
	// not leak deployedVersion (at least in not in entirely obvious way)
	sha1Sum := sha1.Sum([]byte(deployedVersion))

	// GitHub displays 7 hexits for Git commits, so I figure 8 hexits is fine
	// (4 bytes - 32 bits of entropy)
	return fmt.Sprintf(`"%x"`, sha1Sum[0:4])
}
