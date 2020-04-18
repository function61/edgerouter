package erconfig

import (
	"testing"

	"github.com/function61/gokit/assert"
)

func TestSelfOrNilIfNoMeaningfulContent(t *testing.T) {
	emptyConf := TlsConfig{}

	assert.Assert(t, !emptyConf.HasMeaningfulContent())

	assert.Assert(t, emptyConf.SelfOrNilIfNoMeaningfulContent() == nil)

	nonEmptyConf1 := TlsConfig{InsecureSkipVerify: true}
	nonEmptyConf2 := TlsConfig{ServerName: "foobar"}

	assert.Assert(t, nonEmptyConf1.SelfOrNilIfNoMeaningfulContent() != nil)
	assert.Assert(t, nonEmptyConf1.HasMeaningfulContent())

	assert.Assert(t, nonEmptyConf2.SelfOrNilIfNoMeaningfulContent() != nil)
	assert.Assert(t, nonEmptyConf2.HasMeaningfulContent())
}
