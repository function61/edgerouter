package erserver

import (
	"context"
	"log"
	"net/http"

	"github.com/function61/gokit/httputils"
	"github.com/function61/gokit/taskrunner"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func MetricsServer(ctx context.Context, logger *log.Logger) error {
	mux := &http.ServeMux{}
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}

	tasks := taskrunner.New(ctx, logger)

	tasks.Start("listener "+srv.Addr, func(_ context.Context) error {
		return httputils.RemoveGracefulServerClosedError(srv.ListenAndServe())
	})

	tasks.Start("listenershutdowner", httputils.ServerShutdownTask(srv))

	return tasks.Wait()
}
