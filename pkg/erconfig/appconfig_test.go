package erconfig

import (
	"testing"

	"github.com/function61/gokit/assert"
)

func TestSelfOrNilIfNoMeaningfulContent(t *testing.T) {
	emptyConf := TLSConfig{}

	assert.Assert(t, !emptyConf.HasMeaningfulContent())

	assert.Assert(t, emptyConf.SelfOrNilIfNoMeaningfulContent() == nil)

	nonEmptyConf1 := TLSConfig{InsecureSkipVerify: true}
	nonEmptyConf2 := TLSConfig{ServerName: "foobar"}

	assert.Assert(t, nonEmptyConf1.SelfOrNilIfNoMeaningfulContent() != nil)
	assert.Assert(t, nonEmptyConf1.HasMeaningfulContent())

	assert.Assert(t, nonEmptyConf2.SelfOrNilIfNoMeaningfulContent() != nil)
	assert.Assert(t, nonEmptyConf2.HasMeaningfulContent())
}
