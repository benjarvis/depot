package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Storage interface {
	Store(repo, path string, reader io.Reader) error
	Retrieve(repo, path string) (io.ReadCloser, error)
	Delete(repo, path string) error
	Exists(repo, path string) (bool, error)
}

type FileStorage struct {
	basePath string
}

func NewFileStorage(basePath string) *FileStorage {
	return &FileStorage{
		basePath: basePath,
	}
}

func (fs *FileStorage) Store(repo, path string, reader io.Reader) error {
	fullPath := filepath.Join(fs.basePath, repo, path)
	dir := filepath.Dir(fullPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	if err != nil {
		os.Remove(fullPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (fs *FileStorage) Retrieve(repo, path string) (io.ReadCloser, error) {
	fullPath := filepath.Join(fs.basePath, repo, path)
	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

func (fs *FileStorage) Delete(repo, path string) error {
	fullPath := filepath.Join(fs.basePath, repo, path)
	err := os.Remove(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (fs *FileStorage) Exists(repo, path string) (bool, error) {
	fullPath := filepath.Join(fs.basePath, repo, path)
	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}