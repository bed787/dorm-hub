package filestore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var filenamePattern = regexp.MustCompile(`^[a-f0-9]{64}\.jpg$`)

type Storage struct{ dir string }

func New(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Storage{dir}, nil
}
func (s *Storage) path(name string) (string, error) {
	if !filenamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid storage filename")
	}
	return filepath.Join(s.dir, name), nil
}
func (s *Storage) Save(name string, data []byte) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".upload-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (s *Storage) Open(name string) (*os.File, error) {
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (s *Storage) Delete(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Storage) Size(name string) (int64, error) {
	path, err := s.path(name)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
