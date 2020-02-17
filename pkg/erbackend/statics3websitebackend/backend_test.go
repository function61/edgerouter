package statics3websitebackend

import (
	"github.com/function61/gokit/assert"
	"testing"
)

func TestMakeETag(t *testing.T) {
	assert.EqualString(t, makeETag("v1"), `"5a6df720"`)
	assert.EqualString(t, makeETag("v2"), `"a1047eab"`)
}
