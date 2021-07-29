package turbocharger

// In-RAM CAS, use only for testing

import (
	"bytes"
	"context"
	"io"
	"io/fs"
)

type opCounters struct {
	gets int
	puts int
}

func (o opCounters) subtract(other opCounters) opCounters {
	return opCounters{
		gets: o.gets - other.gets,
		puts: o.puts - other.puts,
	}
}

type inMemoryStore struct {
	files    map[ObjectID][]byte
	counters opCounters
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{files: map[ObjectID][]byte{}}
}

var _ CAS = (*inMemoryStore)(nil)

func (d *inMemoryStore) GetObject(ctx context.Context, id ObjectID) (io.ReadCloser, error) {
	d.counters.gets++

	buf, found := d.files[id]
	if !found {
		return nil, fs.ErrNotExist
	}

	return io.NopCloser(bytes.NewReader(buf)), nil
}

func (d *inMemoryStore) InsertObject(ctx context.Context, id ObjectID, content io.Reader, contentType string) error {
	d.counters.puts++

	buf, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	d.files[id] = buf

	return nil
}
