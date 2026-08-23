package erserver

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/felixge/httpsnoop"
)

func logAccess(logger *slog.Logger, r *http.Request, appID string, stats httpsnoop.Metrics) {
	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		clientIP = host
	}

	requestURI := r.RequestURI
	if requestURI == "" {
		requestURI = r.URL.RequestURI()
	}

	logger.InfoContext(r.Context(), "HTTP access",
		"app", appID,
		"client_ip", clientIP,
		"method", r.Method,
		"host", r.Host,
		"request_uri", requestURI,
		"protocol", r.Proto,
		"status", stats.Code,
		"response_bytes", stats.Written,
		"duration_ms", stats.Duration.Seconds()*1000,
		"referer", r.Referer(),
		"user_agent", r.UserAgent())
}
