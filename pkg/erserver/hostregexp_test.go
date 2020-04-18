package erserver

import (
	"testing"

	"github.com/function61/gokit/assert"
)

func TestHostnameRegexpSyntaxToRegexp(t *testing.T) {
	re, err := hostnameRegexpSyntaxToRegexp("hellohttp.{[^.]+}.fn61.net")
	assert.Assert(t, err == nil)

	assert.EqualString(t, re.String(), `^hellohttp\.[^.]+\.fn61\.net$`)

	assert.Assert(t, re.MatchString("hellohttp.dev.fn61.net") == true)
	assert.Assert(t, re.MatchString("xhellohttp.dev.fn61.net") == false)
}
