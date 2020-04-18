// Redirects to somewhere else
package redirectbackend

import (
	"net/http"

	"github.com/function61/edgerouter/pkg/erconfig"
)

func New(opts erconfig.BackendOptsRedirect) http.Handler {
	return &backend{
		to: opts.To,
	}
}

type backend struct {
	to string
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, b.to, http.StatusFound)
}
