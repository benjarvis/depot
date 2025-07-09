package docker

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/depot/depot/internal/storage"
	"github.com/depot/depot/pkg/models"
)

// Manager manages Docker registry instances
type Manager struct {
	registries map[string]*Registry
	storage    storage.Storage
	tlsConfig  *tls.Config
	logger     *logrus.Logger
	mu         sync.RWMutex
}

// NewManager creates a new Docker registry manager
func NewManager(storage storage.Storage, tlsConfig *tls.Config, logger *logrus.Logger) *Manager {
	return &Manager{
		registries: make(map[string]*Registry),
		storage:    storage,
		tlsConfig:  tlsConfig,
		logger:     logger,
	}
}

// StartRegistry starts a Docker registry for the given repository
func (m *Manager) StartRegistry(repo *models.Repository, config *models.DockerRepositoryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if registry already exists
	if _, exists := m.registries[repo.Name]; exists {
		return fmt.Errorf("registry already running for repository %s", repo.Name)
	}

	// Validate port configuration
	if config.HTTPPort == 0 && config.HTTPSPort == 0 {
		return fmt.Errorf("either HTTPPort or HTTPSPort must be specified")
	}

	// Check for port conflicts
	for name, reg := range m.registries {
		if (config.HTTPPort > 0 && config.HTTPPort == reg.config.HTTPPort) ||
			(config.HTTPSPort > 0 && config.HTTPSPort == reg.config.HTTPSPort) {
			return fmt.Errorf("port conflict with repository %s", name)
		}
	}

	// Create new registry
	registry := NewRegistry(repo, config, m.storage, m.logger)

	// Determine which server to start
	var tlsConfig *tls.Config
	if config.HTTPSPort > 0 {
		tlsConfig = m.tlsConfig
	}

	// Start registry in background
	errCh := make(chan error, 1)
	go func() {
		if err := registry.Start(tlsConfig); err != nil {
			m.logger.WithFields(logrus.Fields{
				"repository": repo.Name,
				"error":      err,
			}).Error("Registry failed to start")
			errCh <- err
		}
	}()

	// Wait briefly to check if server started successfully
	select {
	case err := <-errCh:
		return fmt.Errorf("failed to start registry: %w", err)
	default:
		// Registry started successfully
		m.registries[repo.Name] = registry
		m.logger.WithFields(logrus.Fields{
			"repository": repo.Name,
			"http_port":  config.HTTPPort,
			"https_port": config.HTTPSPort,
		}).Info("Docker registry started")
		return nil
	}
}

// StopRegistry stops a Docker registry
func (m *Manager) StopRegistry(repoName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	registry, exists := m.registries[repoName]
	if !exists {
		return fmt.Errorf("no registry running for repository %s", repoName)
	}

	// Stop the registry server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := registry.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop registry: %w", err)
	}

	delete(m.registries, repoName)
	m.logger.WithField("repository", repoName).Info("Docker registry stopped")
	return nil
}

// GetRegistry returns the registry for a repository
func (m *Manager) GetRegistry(repoName string) (*Registry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	registry, exists := m.registries[repoName]
	return registry, exists
}

// StopAll stops all running registries
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var errs []error
	for name, registry := range m.registries {
		if err := registry.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop registry %s: %w", name, err))
		}
	}

	// Clear all registries
	m.registries = make(map[string]*Registry)

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop some registries: %v", errs)
	}
	return nil
}

// IsPortInUse checks if a port is already in use by a registry
func (m *Manager) IsPortInUse(httpPort, httpsPort int) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, reg := range m.registries {
		if (httpPort > 0 && httpPort == reg.config.HTTPPort) ||
			(httpsPort > 0 && httpsPort == reg.config.HTTPSPort) {
			return true, name
		}
	}
	return false, ""
}