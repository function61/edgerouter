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

func TestGoImportBackendValidation(t *testing.T) {
	tests := []struct {
		name                string
		repositoryURLPrefix string
		wantError           bool
	}{
		{"valid", "https://github.com/myorg", false},
		{"empty", "", true},
		{"relative", "/myorg", true},
		{"unsupported scheme", "git://github.com/myorg", true},
		{"query", "https://github.com/myorg?branch=main", true},
		{"fragment", "https://github.com/myorg#readme", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := GoImportBackend(test.repositoryURLPrefix).GoImportOpts.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, want error = %t", err, test.wantError)
			}
		})
	}
}
