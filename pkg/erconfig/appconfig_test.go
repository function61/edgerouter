package erconfig

import (
	"testing"

	"github.com/function61/gokit/testing/assert"
)

func TestSelfOrNilIfNoMeaningfulContent(t *testing.T) {
	emptyConf := TLSConfig{}

	assert.Equal(t, !emptyConf.HasMeaningfulContent(), true)

	assert.Equal(t, emptyConf.SelfOrNilIfNoMeaningfulContent() == nil, true)

	nonEmptyConf1 := TLSConfig{InsecureSkipVerify: true}
	nonEmptyConf2 := TLSConfig{ServerName: "foobar"}

	assert.Equal(t, nonEmptyConf1.SelfOrNilIfNoMeaningfulContent() != nil, true)
	assert.Equal(t, nonEmptyConf1.HasMeaningfulContent(), true)

	assert.Equal(t, nonEmptyConf2.SelfOrNilIfNoMeaningfulContent() != nil, true)
	assert.Equal(t, nonEmptyConf2.HasMeaningfulContent(), true)
}
