package erserver

import (
	"testing"

	"github.com/function61/gokit/testing/assert"
)

func TestHostnameRegexpSyntaxToRegexp(t *testing.T) {
	re, err := hostnameRegexpSyntaxToRegexp("hellohttp.{[^.]+}.fn61.net")
	assert.Equal(t, err == nil, true)

	assert.Equal(t, re.String(), `^hellohttp\.[^.]+\.fn61\.net$`)

	assert.Equal(t, re.MatchString("hellohttp.dev.fn61.net") == true, true)
	assert.Equal(t, re.MatchString("xhellohttp.dev.fn61.net") == false, true)
}
