package erserver

import (
	"net"
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
