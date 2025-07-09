package docker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/depot/depot/internal/storage"
	"github.com/depot/depot/pkg/models"
)

// Registry represents a Docker registry instance
type Registry struct {
	repo     *models.Repository
	config   *models.DockerRepositoryConfig
	storage  storage.Storage
	server   *http.Server
	router   *mux.Router
	logger   *logrus.Logger
	mu       sync.RWMutex
	manifests map[string]map[string]*Manifest // repo -> tag/digest -> manifest
	uploads   map[string]*Upload               // uuid -> upload session
}

// Manifest represents a Docker manifest
type Manifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        *Descriptor            `json:"config,omitempty"`
	Layers        []Descriptor           `json:"layers,omitempty"`
	Manifests     []ManifestDescriptor   `json:"manifests,omitempty"` // For manifest lists
	Annotations   map[string]string      `json:"annotations,omitempty"`
	Raw           []byte                 `json:"-"`
}

// Descriptor represents a content descriptor
type Descriptor struct {
	MediaType string            `json:"mediaType"`
	Size      int64             `json:"size"`
	Digest    string            `json:"digest"`
	URLs      []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ManifestDescriptor extends Descriptor with platform information
type ManifestDescriptor struct {
	Descriptor
	Platform *Platform `json:"platform,omitempty"`
}

// Platform represents platform-specific information
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
}

// Upload represents an in-progress blob upload
type Upload struct {
	UUID      string
	RepoName  string
	StartedAt time.Time
	Size      int64
	Data      []byte
}

// MediaTypes for Docker/OCI content
const (
	MediaTypeDockerSchema2Manifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerSchema2ManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeOCIManifest               = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIManifestList           = "application/vnd.oci.image.index.v1+json"
	MediaTypeDockerSchema2Config       = "application/vnd.docker.container.image.v1+json"
	MediaTypeOCIConfig                 = "application/vnd.oci.image.config.v1+json"
	MediaTypeDockerSchema2Layer        = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	MediaTypeOCILayer                  = "application/vnd.oci.image.layer.v1.tar+gzip"
)

// NewRegistry creates a new Docker registry instance
func NewRegistry(repo *models.Repository, config *models.DockerRepositoryConfig, storage storage.Storage, logger *logrus.Logger) *Registry {
	r := &Registry{
		repo:      repo,
		config:    config,
		storage:   storage,
		logger:    logger,
		manifests: make(map[string]map[string]*Manifest),
		uploads:   make(map[string]*Upload),
	}

	r.setupRoutes()
	return r
}

// Start starts the registry server
func (r *Registry) Start(tlsConfig *tls.Config) error {
	addr := fmt.Sprintf(":%d", r.config.HTTPSPort)
	if r.config.HTTPPort > 0 && tlsConfig == nil {
		addr = fmt.Sprintf(":%d", r.config.HTTPPort)
	}

	r.server = &http.Server{
		Addr:         addr,
		Handler:      r.router,
		TLSConfig:    tlsConfig,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	r.logger.WithFields(logrus.Fields{
		"repository": r.repo.Name,
		"address":    addr,
		"tls":        tlsConfig != nil,
	}).Info("Starting Docker registry")

	if tlsConfig != nil {
		return r.server.ListenAndServeTLS("", "")
	}
	return r.server.ListenAndServe()
}

// Stop stops the registry server
func (r *Registry) Stop(ctx context.Context) error {
	if r.server != nil {
		return r.server.Shutdown(ctx)
	}
	return nil
}

// GetRouter returns the registry's router for mounting on another server
func (r *Registry) GetRouter() *mux.Router {
	return r.router
}

// setupRoutes configures the Docker Registry V2 API routes
func (r *Registry) setupRoutes() {
	r.router = mux.NewRouter()

	// Add logging middleware
	r.router.Use(r.loggingMiddleware)

	// Docker Registry V2 API endpoints
	r.router.HandleFunc("/v2/", r.handleBase).Methods("GET")
	r.router.HandleFunc("/v2/_catalog", r.handleCatalog).Methods("GET")
	r.router.HandleFunc("/v2/{name:.*}/tags/list", r.handleTagsList).Methods("GET")
	r.router.HandleFunc("/v2/{name:.*}/manifests/{reference}", r.handleManifestGet).Methods("GET", "HEAD")
	r.router.HandleFunc("/v2/{name:.*}/manifests/{reference}", r.handleManifestPut).Methods("PUT")
	r.router.HandleFunc("/v2/{name:.*}/manifests/{reference}", r.handleManifestDelete).Methods("DELETE")
	r.router.HandleFunc("/v2/{name:.*}/blobs/{digest}", r.handleBlobGet).Methods("GET", "HEAD")
	r.router.HandleFunc("/v2/{name:.*}/blobs/{digest}", r.handleBlobDelete).Methods("DELETE")
	r.router.HandleFunc("/v2/{name:.*}/blobs/uploads/", r.handleBlobUploadPost).Methods("POST")
	r.router.HandleFunc("/v2/{name:.*}/blobs/uploads/{uuid}", r.handleBlobUploadPatch).Methods("PATCH")
	r.router.HandleFunc("/v2/{name:.*}/blobs/uploads/{uuid}", r.handleBlobUploadPut).Methods("PUT")
	r.router.HandleFunc("/v2/{name:.*}/blobs/uploads/{uuid}", r.handleBlobUploadGet).Methods("GET")
	r.router.HandleFunc("/v2/{name:.*}/blobs/uploads/{uuid}", r.handleBlobUploadDelete).Methods("DELETE")
}

// loggingMiddleware logs HTTP requests
func (r *Registry) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		
		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		next.ServeHTTP(wrapped, req)
		
		r.logger.WithFields(logrus.Fields{
			"method":   req.Method,
			"path":     req.URL.Path,
			"status":   wrapped.statusCode,
			"duration": time.Since(start),
		}).Info("Docker registry request")
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// errorResponse represents a Docker registry error response
type errorResponse struct {
	Errors []registryError `json:"errors"`
}

// registryError represents a single error in the response
type registryError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Detail  map[string]interface{} `json:"detail,omitempty"`
}

// writeError writes an error response
func (r *Registry) writeError(w http.ResponseWriter, code int, errorCode, message string, detail map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	
	resp := errorResponse{
		Errors: []registryError{
			{
				Code:    errorCode,
				Message: message,
				Detail:  detail,
			},
		},
	}
	
	// Encode response (ignoring error for simplicity)
	_ = json.NewEncoder(w).Encode(resp)
}