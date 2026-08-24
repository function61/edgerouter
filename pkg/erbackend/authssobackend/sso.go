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

	if opts.Audience == "t-1/loppi.org-assets" && opts.Subroot == "" { // FIXME: temporary hack. identifying app "loppi.org-assets"
		opts.Subroot = "/assets"
	}

	authGateway := idpClient.CreateAuthGateway(router, opts.Audience, opts.Subroot)

	backendAuthorizer := authGateway.Protect(
		idclient.UserListAuthorizer(opts.AllowedUserIds...),
		authorizedBackend)

	// catch-all route; ServeMux selects the more specific auth gateway endpoints first.
	// we now have something like this:
	// - /_auth/redirect => take auth token from SSH, put it to cookie and redirect to "next" (where we tried to go before requiring login)
	// - /_auth/logout => delete auth cookie and go to logged out page
	// - (catch-all) => if user logged in passthrough to authorized handler. if not redirect to SSO otherwise
	router.Handle("/", backendAuthorizer)

	return router, nil
}
