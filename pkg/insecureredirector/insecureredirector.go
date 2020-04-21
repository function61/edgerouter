// redirects all http -> https
package insecureredirector

import (
	"context"
	"log"
	"net/http"

	"github.com/function61/gokit/httputils"
	"github.com/function61/gokit/taskrunner"
)

func Serve(ctx context.Context, logger *log.Logger) error {
	mux := &http.ServeMux{}
	mux.HandleFunc("/", redirectAllHttpToHttps)

	srv := &http.Server{
		Addr:    ":80",
		Handler: mux,
	}

	tasks := taskrunner.New(ctx, logger)

	tasks.Start("listener "+srv.Addr, func(_ context.Context) error {
		return httputils.RemoveGracefulServerClosedError(srv.ListenAndServe())
	})

	tasks.Start("listenershutdowner", httputils.ServerShutdownTask(srv))

	return tasks.Wait()
}

func redirectAllHttpToHttps(w http.ResponseWriter, r *http.Request) {
	target := "https://" + r.Host + r.URL.Path
	if len(r.URL.RawQuery) > 0 {
		target += "?" + r.URL.RawQuery
	}

	// come back when you have TLS, bro
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}
