// Static bearer token (+ basic auth) -based authentication
package authv0backend

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/httputils"
)

const (
	authorizationHeaderKey = "Authorization"
)

func New(opts erconfig.BackendOptsAuthV0, authorizedBackend http.Handler) http.Handler {
	return &backend{
		expectedBearerToken: opts.BearerToken,
		authorizedBackend:   authorizedBackend,
	}
}

type backend struct {
	expectedBearerToken string
	authorizedBackend   http.Handler
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if authorize(r, b.expectedBearerToken) {
		// the origin system might not need or even expect (it'd fail the response) us sending
		// authorization header.
		// we default to stripping it and we'll implement opt-out if this is ever needed.
		// opt-out is better for secure-by-default anyway.
		r.Header.Del(authorizationHeaderKey)

		b.authorizedBackend.ServeHTTP(w, r)
	} else {
		w.Header().Set("WWW-Authenticate", `Basic realm="Use Authorization: Bearer or provide it as password"`)
		httputils.Error(w, http.StatusUnauthorized)
	}
}

func authorize(r *http.Request, expectedBearerToken string) bool {
	authorizationHeader := r.Header.Get(authorizationHeaderKey)
	if authorizationHeader == "" {
		return false
	}

	// grab:
	// - bearer token OR
	// - token as pass from basic auth
	givenToken := func() string {
		if strings.HasPrefix(authorizationHeader, "Bearer ") {
			return authorizationHeader[len("Bearer "):]
		}

		// try basic auth
		username, password, ok := r.BasicAuth()
		if !ok {
			return ""
		}

		// basic auth is used as a hack to provide bearer token. expecting empty username,
		// or in case username is mandatory it should be "x"
		if username != "" && username != "x" {
			return ""
		}

		return password
	}()

	return subtle.ConstantTimeCompare([]byte(givenToken), []byte(expectedBearerToken)) == 1
}
