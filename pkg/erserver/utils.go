package erserver

import (
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

// implements http.ResponseWriter but stores the sent statusCode so we can access it later
// TODO: we lose support for hijacking (websockets etc.)
type wrappedResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (p *wrappedResponseWriter) WriteHeader(statusCode int) {
	p.statusCode = statusCode
	p.ResponseWriter.WriteHeader(statusCode)
}

func (p *wrappedResponseWriter) StatusCode() int {
	return p.statusCode
}

func createWrappedResponseWriter(inner http.ResponseWriter) *wrappedResponseWriter {
	return &wrappedResponseWriter{inner, http.StatusOK}
}
