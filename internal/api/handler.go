package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/depot/depot/internal/docker"
	"github.com/depot/depot/internal/repository"
	"github.com/depot/depot/internal/storage"
	"github.com/depot/depot/pkg/models"
	"github.com/sirupsen/logrus"
	"go.etcd.io/bbolt"
)

type Handler struct {
	db            *bbolt.DB
	storage       storage.Storage
	logger        *logrus.Logger
	repoMgr       *repository.Manager
	dockerManager *docker.Manager
}

func NewHandler(db *bbolt.DB, storage storage.Storage, dockerManager *docker.Manager, logger *logrus.Logger) *Handler {
	return &Handler{
		db:            db,
		storage:       storage,
		logger:        logger,
		repoMgr:       repository.NewManager(db, storage, logger),
		dockerManager: dockerManager,
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().UTC(),
	})
}

func (h *Handler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := h.repoMgr.List()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to list repositories")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func (h *Handler) CreateRepository(w http.ResponseWriter, r *http.Request) {
	var repo models.Repository
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if repo.Name == "" {
		h.writeError(w, http.StatusBadRequest, "Repository name is required")
		return
	}

	if repo.Type != models.RepositoryTypeDocker && repo.Type != models.RepositoryTypeRaw {
		h.writeError(w, http.StatusBadRequest, "Invalid repository type")
		return
	}

	// For Docker repositories, validate and parse configuration
	if repo.Type == models.RepositoryTypeDocker {
		var config models.DockerRepositoryConfig
		if repo.Config != nil {
			if err := json.Unmarshal(repo.Config, &config); err != nil {
				h.writeError(w, http.StatusBadRequest, "Invalid Docker repository configuration")
				return
			}
		} else {
			// Set default configuration
			config = models.DockerRepositoryConfig{
				HTTPPort:  5000,
				HTTPSPort: 0,
				V1Enabled: false,
			}
		}
		
		// Validate port configuration
		if config.HTTPPort == 0 && config.HTTPSPort == 0 {
			// Use default port if none specified
			config.HTTPPort = 5000
		}
		
		// Check for port conflicts
		if inUse, conflictRepo := h.dockerManager.IsPortInUse(config.HTTPPort, config.HTTPSPort); inUse {
			h.writeError(w, http.StatusConflict, fmt.Sprintf("Port already in use by repository %s", conflictRepo))
			return
		}
		
		// Update repository config
		configBytes, _ := json.Marshal(config)
		repo.Config = configBytes
	}

	if err := h.repoMgr.Create(&repo); err != nil {
		if err == repository.ErrRepositoryExists {
			h.writeError(w, http.StatusConflict, "Repository already exists")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to create repository")
		return
	}
	
	// Start Docker registry if it's a Docker repository
	if repo.Type == models.RepositoryTypeDocker {
		var config models.DockerRepositoryConfig
		json.Unmarshal(repo.Config, &config)
		
		if err := h.dockerManager.StartRegistry(&repo, &config); err != nil {
			// Rollback repository creation
			h.repoMgr.Delete(repo.Name)
			h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to start Docker registry: %v", err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(repo)
}

func (h *Handler) GetRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	repo, err := h.repoMgr.Get(name)
	if err != nil {
		if err == repository.ErrRepositoryNotFound {
			h.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to get repository")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repo)
}

func (h *Handler) DeleteRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	// Get repository to check if it's a Docker repository
	repo, err := h.repoMgr.Get(name)
	if err != nil {
		if err == repository.ErrRepositoryNotFound {
			h.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to get repository")
		return
	}

	// Stop Docker registry if it's running
	if repo.Type == models.RepositoryTypeDocker {
		if err := h.dockerManager.StopRegistry(name); err != nil {
			h.logger.WithError(err).Errorf("Failed to stop Docker registry for %s", name)
			// Continue with deletion even if registry stop fails
		}
	}

	if err := h.repoMgr.Delete(name); err != nil {
		if err == repository.ErrRepositoryNotFound {
			h.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to delete repository")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HandleRepository(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		h.writeError(w, http.StatusBadRequest, "Invalid repository path")
		return
	}

	repoName := pathParts[2]
	repo, err := h.repoMgr.Get(repoName)
	if err != nil {
		if err == repository.ErrRepositoryNotFound {
			h.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to get repository")
		return
	}

	switch repo.Type {
	case models.RepositoryTypeDocker:
		h.handleDockerRepository(w, r, repo)
	case models.RepositoryTypeRaw:
		h.handleRawRepository(w, r, repo)
	default:
		h.writeError(w, http.StatusBadRequest, "Unsupported repository type")
	}
}

func (h *Handler) handleDockerRepository(w http.ResponseWriter, r *http.Request, repo *models.Repository) {
	// Docker repositories should be accessed via their dedicated ports
	var config models.DockerRepositoryConfig
	if err := json.Unmarshal(repo.Config, &config); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Invalid Docker repository configuration")
		return
	}
	
	// Provide information about the Docker registry endpoint
	port := config.HTTPPort
	scheme := "http"
	if config.HTTPSPort > 0 {
		port = config.HTTPSPort
		scheme = "https"
	}
	
	response := map[string]interface{}{
		"message": "Docker repository should be accessed via Docker Registry API",
		"endpoint": fmt.Sprintf("%s://localhost:%d/v2/", scheme, port),
		"repository": repo.Name,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) handleRawRepository(w http.ResponseWriter, r *http.Request, repo *models.Repository) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		h.writeError(w, http.StatusBadRequest, "Invalid artifact path")
		return
	}
	
	artifactPath := strings.Join(pathParts[3:], "/")

	switch r.Method {
	case http.MethodGet:
		h.getRawArtifact(w, r, repo.Name, artifactPath)
	case http.MethodPut:
		h.putRawArtifact(w, r, repo.Name, artifactPath)
	case http.MethodDelete:
		h.deleteRawArtifact(w, r, repo.Name, artifactPath)
	case http.MethodHead:
		h.headRawArtifact(w, r, repo.Name, artifactPath)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) getRawArtifact(w http.ResponseWriter, r *http.Request, repoName, artifactPath string) {
	reader, err := h.storage.Retrieve(repoName, artifactPath)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "Artifact not found")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, reader)
}

func (h *Handler) putRawArtifact(w http.ResponseWriter, r *http.Request, repoName, artifactPath string) {
	if err := h.storage.Store(repoName, artifactPath, r.Body); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to store artifact")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) deleteRawArtifact(w http.ResponseWriter, r *http.Request, repoName, artifactPath string) {
	if err := h.storage.Delete(repoName, artifactPath); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete artifact")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) headRawArtifact(w http.ResponseWriter, r *http.Request, repoName, artifactPath string) {
	exists, err := h.storage.Exists(repoName, artifactPath)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to check artifact")
		return
	}

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}