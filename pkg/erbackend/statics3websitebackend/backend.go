// Serves an atomically deployed website from S3.
// DEPRECATED: use turbocharger instead.
package statics3websitebackend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/function61/edgerouter/pkg/erbackend/reverseproxybackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/ezhttp"
)

func New(appId string, opts erconfig.BackendOptsS3StaticWebsite) (http.Handler, error) {
	if opts.DeployedVersion == "" {
		errMsg := fmt.Sprintf("no deployed version for %s", appId)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, errMsg, http.StatusNotFound)
		}), nil
	}

	// this is not much more than sugar on top of the much more powerful reverseproxybackend

	// bucketPrefix looks like "sites/joonasfi-blog/versionid"
	origin := "https://s3." + opts.RegionId + ".amazonaws.com/" + opts.BucketName + "/" + bucketPrefix(appId, opts.DeployedVersion)

	cacheNotFound := &cache404{}

	// FIXME: it uses appId as cache key, thus we synthetize new cache each time a
	//        new version is deployed
	return reverseproxybackend.NewWithModifyResponse(appId+"-"+opts.DeployedVersion, erconfig.BackendOptsReverseProxy{
		// "/favicon.ico" =>
		//   https://s3.us-east-1.amazonaws.com/myorg-websites/sites/joonasfi-blog/versionid/favicon.ico
		Origins: []string{origin},

		Caching: true, // we'll get easy wins with caching

		RemoveQueryString: true, // reduce cache misses (S3 object responses don't vary by query string)

		IndexDocument: "index.html", // "/foo/" => "/foo/index.html"

	}, func(r *http.Response) error {
		// S3 gives 403 for not found when the requester doesn't have permission to list files
		if opts.NotFoundPage != "" && (r.StatusCode == http.StatusNotFound || r.StatusCode == http.StatusForbidden) {
			body, contentType, err := serveCached404Page(origin+"/"+strings.TrimPrefix(opts.NotFoundPage, "/"), cacheNotFound)
			if err != nil {
				return err // TODO: how to best react?
			}

			r.Header.Set("Content-Type", contentType)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		return nil
	})
}

func serveCached404Page(url404 string, cacheNotFound *cache404) ([]byte, string, error) {
	cacheNotFound.mu.Lock()
	defer cacheNotFound.mu.Unlock()

	if cacheNotFound.contentType == "" {
		res, err := ezhttp.Get(context.Background(), url404)
		if err != nil {
			return nil, "", fmt.Errorf("failed ezhttp.Get 404 page: %w", err)
		}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, "", err
		}

		cacheNotFound.contentType = res.Header.Get("Content-Type")
		cacheNotFound.body = body
	}

	return cacheNotFound.body, cacheNotFound.contentType, nil
}

// these happen so often they've to be cheap
type cache404 struct {
	mu          sync.Mutex
	body        []byte
	contentType string
}
