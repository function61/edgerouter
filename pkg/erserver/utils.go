package erserver

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// net.SplitHostPort() does not support case where port is not defined...
// this should not ever fail
func nonStupidSplitHostPort(maybeHostPort string) (string, string, error) {
	if !strings.Contains(maybeHostPort, ":") {
		return maybeHostPort, "", nil
	}

	return net.SplitHostPort(maybeHostPort)
}

// helper for adapting context cancellation to shutdown the HTTP listener
func cancelableServer(ctx context.Context, srv *http.Server, listener func() error) error {
	shutdownerCtx, cancel := context.WithCancel(ctx)

	shutdownResult := make(chan error, 1)

	// this is the actual shutdowner
	go func() {
		// triggered by parent cancellation
		// (or below for cleanup if ListenAndServe() failed by itself)
		<-shutdownerCtx.Done()

		// can't use parent ctx b/c it'd cancel the Shutdown() itself
		shutdownResult <- srv.Shutdown(context.Background())
	}()

	err := listener()

	// ask shutdowner to stop. this is useful only for cleanup where listener failed before
	// it was requested to shut down b/c parent cancellation didn't happen and thus the
	// shutdowner would still wait.
	cancel()

	if err == http.ErrServerClosed { // expected for graceful shutdown (not actually error)
		return <-shutdownResult // should be nil, unless shutdown fails
	} else {
		// some other error
		// (or nil, but http server should always exit with non-nil error)
		return err
	}
}
