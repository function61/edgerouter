package erbackend

import (
	"net/http"
)

// FIXME: just use http.Handler?
type Backend interface {
	Serve(w http.ResponseWriter, r *http.Request)
}
