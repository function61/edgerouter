package turbocharger

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/function61/gokit/atomicfilewrite"
	"github.com/function61/gokit/fileexists"
)

// a store that use the filesystem as the backing store for objects.
// usually used for local caching.
type fileStore struct {
	dir string
}

func newFileStore(dir string) (CAS, error) {
	return &fileStore{dir}, os.MkdirAll(dir, 0770)
}

var _ CAS = (*fileStore)(nil)

func (d *fileStore) GetObject(ctx context.Context, id ObjectID) (io.ReadCloser, error) {
	return os.Open(d.path(id)) // important to pass err as-is so it's fs.ErrNotExist
}

func (d *fileStore) InsertObject(ctx context.Context, id ObjectID, content io.Reader, contentType string) error {
	exists, err := fileexists.Exists(d.path(id))
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return atomicfilewrite.Write(d.path(id), func(sink io.Writer) error {
		_, err := io.Copy(sink, content)
		return err
	})
}

func (d *fileStore) path(id ObjectID) string {
	return filepath.Join(d.dir, id.String())
}
