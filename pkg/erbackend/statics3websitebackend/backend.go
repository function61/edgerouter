// Serves an atomically deployed website from S3
package statics3websitebackend

import (
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func New(app erconfig.Application) erbackend.Backend {
	s3StaticWebsiteOpts := app.Backend.S3StaticWebsiteOpts

	// looks like "sites/joonasfi-blog/versionid"
	pathPrefix := bucketPrefix(app.Id, s3StaticWebsiteOpts.DeployedVersion)

	return &s3Backend{
		reverseProxy: &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				if s3StaticWebsiteOpts.DeployedVersion == "" {
					r.URL = nil
					log.Printf("no deployed version for %s", app.Id)
					return
				}

				// "/favicon.ico" => "sites/joonasfi-blog/versionid/favicon.ico"
				rerouted := pathPrefix + r.URL.Path

				// "/" => "/index.html"
				// "/foo/bar/" => "/foo/bar/index.html"
				if strings.HasSuffix(rerouted, "/") {
					rerouted += "index.html"
				}

				bucketUrl, err := url.Parse("https://s3." + s3StaticWebsiteOpts.RegionId + ".amazonaws.com/" + s3StaticWebsiteOpts.BucketName + "/" + rerouted)
				if err != nil {
					panic(err)
				}

				r.Host = bucketUrl.Host
				r.URL = bucketUrl
			},
			ModifyResponse: func(resp *http.Response) error {
				// S3 404s look like:
				// <Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message><Key>sites/joonasfi-blog/2019-01-11/404</Key><RequestId>...</RequestId><HostId>...</HostId></Error>
				//
				// which reveals too much information => rewrite them
				if resp.StatusCode == http.StatusNotFound {
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
}

func (s *s3Backend) Serve(w http.ResponseWriter, r *http.Request) {
	s.reverseProxy.ServeHTTP(w, r)
}
