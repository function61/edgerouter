// Redirects to somewhere else
package redirectbackend

import (
	"net/http"

	"github.com/function61/edgerouter/pkg/erconfig"
)

func New(opts erconfig.BackendOptsRedirect) http.Handler {
	return &redirector{
		to: opts.To,
	}
}

type redirector struct {
	to string
}

func (b *redirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, b.to, http.StatusFound)
}
