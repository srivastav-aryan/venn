package cas

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrNotExist = errors.New("CAS: No such build output")

type Store struct {
	baseDir string
}

func New(baseDir string) (*Store, error) {
	err := os.MkdirAll(baseDir, 0o755)
	if err != nil {
		return nil, err
	}

	return &Store{baseDir: baseDir}, nil
}

func (s *Store) objectPath(digest string) string {
	return filepath.Join(s.baseDir, digest[:2], digest)
}

func (s *Store) Has(digest string) (bool, error) {
	finalPath := s.objectPath(digest)
	_, err := os.Stat(finalPath)

	if err == nil {
		return true, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	return false, err
}

func (s *Store) Put(digest string, data io.Reader) error {
	isThere, err := s.Has(digest)
	if err != nil {
		return err
	}

	if isThere {
		return nil
	}
	filePath := s.objectPath(digest)
	dirPath := filepath.Join(s.baseDir, digest[:2])

	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dirPath, "tmpHash-*")
	if err != nil {
		return err
	}

	defer func() {
		_ = os.Remove(f.Name())
	}()
	_, err1 := io.Copy(f, data)

	if err1 != nil {
		return err1
	}

	f.Close()

	// rename withing same file system rename
	err2 := os.Rename(f.Name(), filePath)

	if err2 != nil {
		return err2
	}

	return nil
}

func (s *Store) Get(digest string) (io.ReadCloser, error) {
	dataPath := s.objectPath(digest)

	f, err := os.Open(dataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotExist
		}

		return nil, err

	}

	return f, nil
}
