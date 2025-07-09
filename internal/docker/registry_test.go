package docker

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sirupsen/logrus"

	"github.com/depot/depot/internal/storage"
	"github.com/depot/depot/pkg/models"
)

func TestDockerRegistryV2API(t *testing.T) {
	// Create test storage
	testStorage := storage.NewFileStorage(t.TempDir())
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create test repository
	repo := &models.Repository{
		Name:        "test-docker",
		Type:        models.RepositoryTypeDocker,
		Description: "Test Docker repository",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	config := &models.DockerRepositoryConfig{
		HTTPPort:  0, // Use main server port
		HTTPSPort: 0,
		V1Enabled: false,
	}

	// Create registry
	registry := NewRegistry(repo, config, testStorage, logger)

	t.Run("Base Endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v2/", nil)
		w := httptest.NewRecorder()
		
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "registry/2.0", w.Header().Get("Docker-Distribution-API-Version"))
	})

	t.Run("Catalog", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v2/_catalog", nil)
		w := httptest.NewRecorder()
		
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		
		repos, ok := response["repositories"].([]interface{})
		assert.True(t, ok)
		assert.Empty(t, repos)
	})

	t.Run("Upload and Retrieve Blob", func(t *testing.T) {
		blobData := []byte("This is a test blob")
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(blobData))

		// Start upload
		req := httptest.NewRequest("POST", "/v2/test-image/blobs/uploads/", nil)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusAccepted, w.Code)
		uploadUUID := w.Header().Get("Docker-Upload-UUID")
		assert.NotEmpty(t, uploadUUID)

		// Upload blob data
		uploadURL := fmt.Sprintf("/v2/test-image/blobs/uploads/%s?digest=%s", uploadUUID, digest)
		req = httptest.NewRequest("PUT", uploadURL, bytes.NewReader(blobData))
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, digest, w.Header().Get("Docker-Content-Digest"))

		// Retrieve blob
		req = httptest.NewRequest("GET", fmt.Sprintf("/v2/test-image/blobs/%s", digest), nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, digest, w.Header().Get("Docker-Content-Digest"))
		assert.Equal(t, blobData, w.Body.Bytes())
	})

	t.Run("Upload and Retrieve Manifest", func(t *testing.T) {
		// Create a simple manifest
		manifest := Manifest{
			SchemaVersion: 2,
			MediaType:     MediaTypeDockerSchema2Manifest,
			Config: &Descriptor{
				MediaType: MediaTypeDockerSchema2Config,
				Size:      1234,
				Digest:    "sha256:abcd1234",
			},
			Layers: []Descriptor{
				{
					MediaType: MediaTypeDockerSchema2Layer,
					Size:      5678,
					Digest:    "sha256:layer1234",
				},
			},
		}

		manifestData, err := json.Marshal(manifest)
		require.NoError(t, err)

		// Upload manifest
		req := httptest.NewRequest("PUT", "/v2/test-image/manifests/v1.0", bytes.NewReader(manifestData))
		req.Header.Set("Content-Type", MediaTypeDockerSchema2Manifest)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusCreated, w.Code)
		digest := w.Header().Get("Docker-Content-Digest")
		assert.NotEmpty(t, digest)

		// Retrieve manifest by tag
		req = httptest.NewRequest("GET", "/v2/test-image/manifests/v1.0", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, MediaTypeDockerSchema2Manifest, w.Header().Get("Content-Type"))
		assert.Equal(t, digest, w.Header().Get("Docker-Content-Digest"))

		// Retrieve manifest by digest
		req = httptest.NewRequest("GET", fmt.Sprintf("/v2/test-image/manifests/%s", digest), nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Multi-arch Manifest List", func(t *testing.T) {
		// Create a manifest list
		manifestList := Manifest{
			SchemaVersion: 2,
			MediaType:     MediaTypeDockerSchema2ManifestList,
			Manifests: []ManifestDescriptor{
				{
					Descriptor: Descriptor{
						MediaType: MediaTypeDockerSchema2Manifest,
						Size:      1234,
						Digest:    "sha256:amd64manifest",
					},
					Platform: &Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
				{
					Descriptor: Descriptor{
						MediaType: MediaTypeDockerSchema2Manifest,
						Size:      1234,
						Digest:    "sha256:arm64manifest",
					},
					Platform: &Platform{
						Architecture: "arm64",
						OS:           "linux",
					},
				},
			},
		}

		manifestData, err := json.Marshal(manifestList)
		require.NoError(t, err)

		// Upload manifest list
		req := httptest.NewRequest("PUT", "/v2/multi-arch-image/manifests/latest", bytes.NewReader(manifestData))
		req.Header.Set("Content-Type", MediaTypeDockerSchema2ManifestList)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusCreated, w.Code)

		// Retrieve manifest list
		req = httptest.NewRequest("GET", "/v2/multi-arch-image/manifests/latest", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, MediaTypeDockerSchema2ManifestList, w.Header().Get("Content-Type"))

		var retrieved Manifest
		err = json.NewDecoder(w.Body).Decode(&retrieved)
		require.NoError(t, err)
		assert.Len(t, retrieved.Manifests, 2)
	})

	t.Run("Tags List", func(t *testing.T) {
		// Upload multiple manifests with different tags
		manifest := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     MediaTypeDockerSchema2Manifest,
		}
		manifestData, _ := json.Marshal(manifest)

		for _, tag := range []string{"v1.0", "v1.1", "latest"} {
			req := httptest.NewRequest("PUT", fmt.Sprintf("/v2/tagged-image/manifests/%s", tag), bytes.NewReader(manifestData))
			req.Header.Set("Content-Type", MediaTypeDockerSchema2Manifest)
			w := httptest.NewRecorder()
			registry.GetRouter().ServeHTTP(w, req)
			assert.Equal(t, http.StatusCreated, w.Code)
		}

		// List tags
		req := httptest.NewRequest("GET", "/v2/tagged-image/tags/list", nil)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		
		assert.Equal(t, "tagged-image", response["name"])
		tags, ok := response["tags"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, tags, 3)
	})

	t.Run("Blob Chunked Upload", func(t *testing.T) {
		chunk1 := []byte("First chunk")
		chunk2 := []byte("Second chunk")
		fullData := append(chunk1, chunk2...)
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(fullData))

		// Start upload
		req := httptest.NewRequest("POST", "/v2/chunked-image/blobs/uploads/", nil)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusAccepted, w.Code)
		uploadUUID := w.Header().Get("Docker-Upload-UUID")

		// Upload first chunk
		req = httptest.NewRequest("PATCH", fmt.Sprintf("/v2/chunked-image/blobs/uploads/%s", uploadUUID), bytes.NewReader(chunk1))
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, fmt.Sprintf("bytes=0-%d", len(chunk1)-1), w.Header().Get("Range"))

		// Upload second chunk
		req = httptest.NewRequest("PATCH", fmt.Sprintf("/v2/chunked-image/blobs/uploads/%s", uploadUUID), bytes.NewReader(chunk2))
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusAccepted, w.Code)

		// Complete upload
		req = httptest.NewRequest("PUT", fmt.Sprintf("/v2/chunked-image/blobs/uploads/%s?digest=%s", uploadUUID, digest), nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusCreated, w.Code)

		// Verify blob
		req = httptest.NewRequest("GET", fmt.Sprintf("/v2/chunked-image/blobs/%s", digest), nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, fullData, w.Body.Bytes())
	})

	t.Run("Delete Operations", func(t *testing.T) {
		// Upload a manifest
		manifest := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     MediaTypeDockerSchema2Manifest,
		}
		manifestData, _ := json.Marshal(manifest)

		req := httptest.NewRequest("PUT", "/v2/delete-test/manifests/v1.0", bytes.NewReader(manifestData))
		req.Header.Set("Content-Type", MediaTypeDockerSchema2Manifest)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// Delete manifest
		req = httptest.NewRequest("DELETE", "/v2/delete-test/manifests/v1.0", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusAccepted, w.Code)

		// Verify manifest is gone
		req = httptest.NewRequest("GET", "/v2/delete-test/manifests/v1.0", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Error Cases", func(t *testing.T) {
		// Non-existent manifest
		req := httptest.NewRequest("GET", "/v2/nonexistent/manifests/latest", nil)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code)

		// Invalid digest format
		req = httptest.NewRequest("PUT", "/v2/test/blobs/uploads/123?digest=invalid", strings.NewReader("data"))
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code) // Upload UUID not found

		// Missing digest parameter
		req = httptest.NewRequest("POST", "/v2/test/blobs/uploads/", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		uploadUUID := w.Header().Get("Docker-Upload-UUID")
		
		req = httptest.NewRequest("PUT", fmt.Sprintf("/v2/test/blobs/uploads/%s", uploadUUID), strings.NewReader("data"))
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDockerRegistryManager(t *testing.T) {
	testStorage := storage.NewFileStorage(t.TempDir())
	logger := logrus.New()
	
	manager := NewManager(testStorage, nil, logger)

	t.Run("Start and Stop Registry", func(t *testing.T) {
		repo := &models.Repository{
			Name: "test-repo",
			Type: models.RepositoryTypeDocker,
		}
		config := &models.DockerRepositoryConfig{
			HTTPPort:  15000,
			HTTPSPort: 0,
		}

		// Start registry
		err := manager.StartRegistry(repo, config)
		assert.NoError(t, err)

		// Verify registry is running
		reg, exists := manager.GetRegistry("test-repo")
		assert.True(t, exists)
		assert.NotNil(t, reg)

		// Stop registry
		err = manager.StopRegistry("test-repo")
		assert.NoError(t, err)

		// Verify registry is stopped
		_, exists = manager.GetRegistry("test-repo")
		assert.False(t, exists)
	})

	t.Run("Port Conflict Detection", func(t *testing.T) {
		repo1 := &models.Repository{
			Name: "repo1",
			Type: models.RepositoryTypeDocker,
		}
		config1 := &models.DockerRepositoryConfig{
			HTTPPort: 15001,
		}

		repo2 := &models.Repository{
			Name: "repo2",
			Type: models.RepositoryTypeDocker,
		}
		config2 := &models.DockerRepositoryConfig{
			HTTPPort: 15001, // Same port
		}

		// Start first registry
		err := manager.StartRegistry(repo1, config1)
		assert.NoError(t, err)

		// Try to start second registry with same port
		err = manager.StartRegistry(repo2, config2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "port conflict")

		// Cleanup
		manager.StopRegistry("repo1")
	})
}

func TestOCIMediaTypes(t *testing.T) {
	testStorage := storage.NewFileStorage(t.TempDir())
	logger := logrus.New()

	repo := &models.Repository{
		Name: "oci-test",
		Type: models.RepositoryTypeDocker,
	}
	config := &models.DockerRepositoryConfig{}

	registry := NewRegistry(repo, config, testStorage, logger)

	t.Run("OCI Manifest", func(t *testing.T) {
		// Create OCI manifest
		manifest := Manifest{
			SchemaVersion: 2,
			MediaType:     MediaTypeOCIManifest,
			Config: &Descriptor{
				MediaType: MediaTypeOCIConfig,
				Size:      1234,
				Digest:    "sha256:config123",
			},
			Layers: []Descriptor{
				{
					MediaType: MediaTypeOCILayer,
					Size:      5678,
					Digest:    "sha256:layer123",
				},
			},
		}

		manifestData, err := json.Marshal(manifest)
		require.NoError(t, err)

		// Upload OCI manifest
		req := httptest.NewRequest("PUT", "/v2/oci-image/manifests/v1.0", bytes.NewReader(manifestData))
		req.Header.Set("Content-Type", MediaTypeOCIManifest)
		w := httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusCreated, w.Code)

		// Retrieve OCI manifest
		req = httptest.NewRequest("GET", "/v2/oci-image/manifests/v1.0", nil)
		w = httptest.NewRecorder()
		registry.GetRouter().ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, MediaTypeOCIManifest, w.Header().Get("Content-Type"))
	})
}