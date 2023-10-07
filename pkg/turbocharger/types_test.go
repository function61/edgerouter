package turbocharger

import (
	"testing"

	"github.com/function61/gokit/assert"
)

func TestObjectIDFromString(t *testing.T) {
	id, err := ObjectIDFromString("bkL0DwZiwOdWij766bl0qyZDrsj4zy-EqmL25fNaBAM")
	assert.Ok(t, err)
	assert.EqualString(t, id.String(), "bkL0DwZiwOdWij766bl0qyZDrsj4zy-EqmL25fNaBAM")

	// shouldn't accept too long data
	_, err = ObjectIDFromString("bkL0DwZiwOdWij766bl0qyZDrsj4zy-EqmL25fNaBAMddd")
	assert.EqualString(t, err.Error(), "invalid length for ObjectID; got 46")
}
