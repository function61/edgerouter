// function61 Single Sign-On authentication (github.com/function61/id)
package authssobackend

import (
	"errors"
	"net/http"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/id/pkg/idclient"
	"github.com/gorilla/mux"
)

func New(
	opts erconfig.BackendOptsAuthSso,
	authorizedBackend http.Handler,
) (http.Handler, error) {
	serverUrl := opts.IdServerUrl
	if serverUrl == "" {
		serverUrl = idclient.Function61
	}

	idpClient := idclient.New(serverUrl)

	router := mux.NewRouter()

	if opts.Audience == "" { // accidental empty could be dangerous
		return nil, errors.New("empty audience")
	}

	authGateway := idpClient.CreateAuthGateway(router, opts.Audience)

	backendAuthorizer := authGateway.Protect(
		idclient.UserListAuthorizer(opts.AllowedUserIds...),
		authorizedBackend)

	// catch-all route (after auth gateway's endpoints)
	router.PathPrefix("/").Handler(backendAuthorizer)

	return router, nil
}
