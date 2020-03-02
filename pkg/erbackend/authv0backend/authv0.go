// Static bearer token (+ basic auth) -based authentication
package authv0backend

import (
	"crypto/subtle"
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"net/http"
	"strings"
)

func New(opts erconfig.BackendOptsAuthV0, authorizedBackend erbackend.Backend) erbackend.Backend {
	return &backend{
		expectedBearerToken: opts.BearerToken,
		authorizedBackend:   authorizedBackend,
	}
}

type backend struct {
	expectedBearerToken string
	authorizedBackend   erbackend.Backend
}

func (b *backend) Serve(w http.ResponseWriter, r *http.Request) {
	if authorize(r, b.expectedBearerToken) {
		b.authorizedBackend.Serve(w, r)
	} else {
		w.Header().Set("WWW-Authenticate", `Basic realm="Use Authorization: Bearer or provide it as password"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
}

func authorize(r *http.Request, expectedBearerToken string) bool {
	authorizationHeader := r.Header.Get("Authorization")
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