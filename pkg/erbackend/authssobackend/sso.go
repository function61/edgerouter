// function61 Single Sign-On authentication (github.com/function61/id)
package authssobackend

import (
	"errors"
	"net/http"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/id/pkg/idclient"
)

func New(
	opts erconfig.BackendOptsAuthSso,
	authorizedBackend http.Handler,
) (http.Handler, error) {
	serverURL := opts.IDServerURL
	if serverURL == "" {
		serverURL = idclient.Function61
	}

	idpClient := idclient.New(serverURL)

	router := http.NewServeMux()

	if opts.Audience == "" { // accidental empty could be dangerous
		return nil, errors.New("empty audience")
	}

	authGateway := idpClient.CreateAuthGateway(router, opts.Audience)

	backendAuthorizer := authGateway.Protect(
		idclient.UserListAuthorizer(opts.AllowedUserIds...),
		authorizedBackend)

	// catch-all route; ServeMux selects the more specific auth gateway endpoints first.
	router.Handle("/", backendAuthorizer)

	return router, nil
}
