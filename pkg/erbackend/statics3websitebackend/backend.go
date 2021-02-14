// Serves an atomically deployed website from S3
package statics3websitebackend

import (
	"fmt"
	"net/http"

	"github.com/function61/edgerouter/pkg/erbackend/reverseproxybackend"
	"github.com/function61/edgerouter/pkg/erconfig"
)

func New(appId string, opts erconfig.BackendOptsS3StaticWebsite) (http.Handler, error) {
	if opts.DeployedVersion == "" {
		errMsg := fmt.Sprintf("no deployed version for %s", appId)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, errMsg, http.StatusNotFound)
		}), nil
	}

	bucketHost := "https://s3." + opts.RegionId + ".amazonaws.com/" + opts.BucketName + "/"

	// looks like "sites/joonasfi-blog/versionid"
	pathPrefix := bucketPrefix(appId, opts.DeployedVersion)

	// FIXME: it uses appId as cache key, thus we synthetize new cache each time a
	//        new version is deployed
	return reverseproxybackend.New(appId+"-"+opts.DeployedVersion, erconfig.BackendOptsReverseProxy{
		// "/favicon.ico" =>
		//   https://s3.us-east-1.amazonaws.com/myorg-websites/sites/joonasfi-blog/versionid/favicon.ico
		Origins: []string{bucketHost + pathPrefix},

		Caching: true, // we'll get easy wins with caching

		RemoveQueryString: true, // reduce cache misses (S3 object responses don't vary by query string)

		IndexDocument: "index.html", // "/foo/" => "/foo/index.html"
	})
}
