package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/depot/depot/internal/storage"
	"github.com/depot/depot/pkg/models"
	"github.com/sirupsen/logrus"
	"go.etcd.io/bbolt"
)

var (
	bucketRepositories    = []byte("repositories")
	ErrRepositoryExists   = errors.New("repository already exists")
	ErrRepositoryNotFound = errors.New("repository not found")
)

type Manager struct {
	db      *bbolt.DB
	storage storage.Storage
	logger  *logrus.Logger
}

func NewManager(db *bbolt.DB, storage storage.Storage, logger *logrus.Logger) *Manager {
	db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketRepositories)
		return err
	})

	return &Manager{
		db:      db,
		storage: storage,
		logger:  logger,
	}
}

func (m *Manager) Create(repo *models.Repository) error {
	repo.CreatedAt = time.Now()
	repo.UpdatedAt = repo.CreatedAt

	return m.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRepositories)
		
		if b.Get([]byte(repo.Name)) != nil {
			return ErrRepositoryExists
		}

		data, err := json.Marshal(repo)
		if err != nil {
			return fmt.Errorf("failed to marshal repository: %w", err)
		}

		return b.Put([]byte(repo.Name), data)
	})
}

func (m *Manager) Get(name string) (*models.Repository, error) {
	var repo models.Repository

	err := m.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRepositories)
		data := b.Get([]byte(name))
		
		if data == nil {
			return ErrRepositoryNotFound
		}

		return json.Unmarshal(data, &repo)
	})

	if err != nil {
		return nil, err
	}

	return &repo, nil
}

func (m *Manager) List() ([]*models.Repository, error) {
	var repos []*models.Repository

	err := m.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRepositories)
		
		return b.ForEach(func(k, v []byte) error {
			var repo models.Repository
			if err := json.Unmarshal(v, &repo); err != nil {
				return fmt.Errorf("failed to unmarshal repository %s: %w", k, err)
			}
			repos = append(repos, &repo)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return repos, nil
}

func (m *Manager) Delete(name string) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRepositories)
		
		if b.Get([]byte(name)) == nil {
			return ErrRepositoryNotFound
		}

		return b.Delete([]byte(name))
	})
}