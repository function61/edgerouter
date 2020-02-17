// Serves an atomically deployed website from S3
package statics3websitebackend

import (
	"crypto/sha1"
	"fmt"
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func New(appId string, opts erconfig.BackendOptsS3StaticWebsite) erbackend.Backend {
	// looks like "sites/joonasfi-blog/versionid"
	pathPrefix := bucketPrefix(appId, opts.DeployedVersion)

	expectedETag := makeETag(opts.DeployedVersion)

	return &s3Backend{
		expectedETag: expectedETag,
		reverseProxy: &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				if opts.DeployedVersion == "" {
					r.URL = nil
					log.Printf("no deployed version for %s", appId)
					return
				}

				// "/favicon.ico" => "sites/joonasfi-blog/versionid/favicon.ico"
				rerouted := pathPrefix + r.URL.Path

				// "/" => "/index.html"
				// "/foo/bar/" => "/foo/bar/index.html"
				if strings.HasSuffix(rerouted, "/") {
					rerouted += "index.html"
				}

				bucketUrl, err := url.Parse("https://s3." + opts.RegionId + ".amazonaws.com/" + opts.BucketName + "/" + rerouted)
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
					resp.Header.Set("ETag", expectedETag)
				case http.StatusNotFound:
					// S3 404s look like:
					// <Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message><Key>sites/joonasfi-blog/2019-01-11/404</Key><RequestId>...</RequestId><HostId>...</HostId></Error>
					//
					// which reveals too much information => rewrite them
					resp.Header.Set("Content-Type", "text/plain")
					resp.Body = ioutil.NopCloser(strings.NewReader("404 page not found"))
				}

				return nil
			},
		},
	}
}

type s3Backend struct {
	reverseProxy *httputil.ReverseProxy
	expectedETag string
}

func (s *s3Backend) Serve(w http.ResponseWriter, r *http.Request) {
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

// https://stackoverflow.com/questions/34846016/is-it-ok-for-several-paths-to-share-the-same-etag
func makeETag(deployedVersion string) string {
	// even if ETag syntax allowed us use deployedVersion as-is, best to hash it to
	// not leak deployedVersion (at least in not in entirely obvious way)
	sha1Sum := sha1.Sum([]byte(deployedVersion))

	// GitHub displays 7 hexits for Git commits, so I figure 8 hexits is fine
	// (4 bytes - 32 bits of entropy)
	return fmt.Sprintf(`"%x"`, sha1Sum[0:4])
}
