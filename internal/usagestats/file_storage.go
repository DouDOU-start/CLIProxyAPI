package usagestats

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type fileSnapshotStorage struct {
	path string
}

func newFileSnapshotStorage(path string) snapshotStorage {
	return &fileSnapshotStorage{path: filepath.Clean(path)}
}

func (s *fileSnapshotStorage) Load(context.Context) ([]byte, error) {
	data, errRead := os.ReadFile(s.path)
	if errors.Is(errRead, os.ErrNotExist) {
		return nil, nil
	}
	if errRead != nil {
		return nil, fmt.Errorf("read file: %w", errRead)
	}
	return data, nil
}

func (s *fileSnapshotStorage) Save(_ context.Context, data []byte) error {
	directory := filepath.Dir(s.path)
	if errMkdir := os.MkdirAll(directory, 0o755); errMkdir != nil {
		return fmt.Errorf("create directory: %w", errMkdir)
	}
	temporary, errCreate := os.CreateTemp(directory, ".usage-statistics-*.tmp")
	if errCreate != nil {
		return fmt.Errorf("create temporary file: %w", errCreate)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		if errRemove := os.Remove(temporaryPath); errRemove != nil && !errors.Is(errRemove, os.ErrNotExist) {
			log.WithError(errRemove).Debug("usage statistics: failed to remove temporary file")
		}
	}
	if errChmod := temporary.Chmod(0o600); errChmod != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("secure temporary file: %w", errChmod)
	}
	if _, errWrite := temporary.Write(data); errWrite != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write file: %w", errWrite)
	}
	if errSync := temporary.Sync(); errSync != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("sync file: %w", errSync)
	}
	if errClose := temporary.Close(); errClose != nil {
		cleanup()
		return fmt.Errorf("close temporary file: %w", errClose)
	}
	if errRename := os.Rename(temporaryPath, s.path); errRename != nil {
		cleanup()
		return fmt.Errorf("replace file: %w", errRename)
	}
	return nil
}

func (s *fileSnapshotStorage) Close() error {
	return nil
}
