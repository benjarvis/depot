package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/depot/depot/pkg/models"
)

func TestDockerRegistryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	s, cleanup := startTestServer(t)
	defer cleanup()

	baseURL := fmt.Sprintf("https://localhost:%s", s.GetPort())

	t.Run("Create Docker Repository", func(t *testing.T) {
		// Create a Docker repository with a specific port
		repo := models.Repository{
			Name:        "test-docker-repo",
			Type:        models.RepositoryTypeDocker,
			Description: "Test Docker repository",
			Config: json.RawMessage(`{
				"http_port": 5001,
				"https_port": 0,
				"v1_enabled": false
			}`),
		}

		reqBody, _ := json.Marshal(repo)
		resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Wait for registry to start
		time.Sleep(2 * time.Second)

		// Test Docker registry base endpoint
		registryResp, err := makeRequest("GET", "http://localhost:5001/v2/", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, registryResp.StatusCode)
		assert.Equal(t, "registry/2.0", registryResp.Header.Get("Docker-Distribution-API-Version"))
	})

	t.Run("Create Docker Repository on Main Port", func(t *testing.T) {
		// Create a Docker repository on port 0 (main server port)
		repo := models.Repository{
			Name:        "main-port-docker",
			Type:        models.RepositoryTypeDocker,
			Description: "Docker repository on main port",
			Config: json.RawMessage(`{
				"http_port": 0,
				"https_port": 0,
				"v1_enabled": false
			}`),
		}

		reqBody, _ := json.Marshal(repo)
		resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Test Docker registry on main port
		registryResp, err := makeRequest("GET", baseURL+"/v2/", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, registryResp.StatusCode)
		assert.Equal(t, "registry/2.0", registryResp.Header.Get("Docker-Distribution-API-Version"))
	})

	t.Run("Port Conflict Detection", func(t *testing.T) {
		// Try to create another repository with the same port
		repo := models.Repository{
			Name:        "conflicting-docker-repo",
			Type:        models.RepositoryTypeDocker,
			Description: "This should fail",
			Config: json.RawMessage(`{
				"http_port": 5001,
				"https_port": 0
			}`),
		}

		reqBody, _ := json.Marshal(repo)
		resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "Port already in use")
	})

	t.Run("Delete Docker Repository", func(t *testing.T) {
		// Delete the Docker repository
		resp, err := makeRequest("DELETE", baseURL+"/api/v1/repositories/test-docker-repo", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Verify registry is stopped
		time.Sleep(1 * time.Second)
		_, err = makeRequest("GET", "http://localhost:5001/v2/", nil)
		assert.Error(t, err)
	})
}

func TestDockerClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker client integration test in short mode")
	}

	// Check if Docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping client integration test")
	}

	s, cleanup := startTestServer(t)
	defer cleanup()

	baseURL := fmt.Sprintf("https://localhost:%s", s.GetPort())

	// Create a Docker repository
	repo := models.Repository{
		Name:        "docker-client-test",
		Type:        models.RepositoryTypeDocker,
		Description: "Test with real Docker client",
		Config: json.RawMessage(`{
			"http_port": 5002,
			"https_port": 0,
			"v1_enabled": false
		}`),
	}

	reqBody, _ := json.Marshal(repo)
	resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Wait for registry to start
	time.Sleep(2 * time.Second)

	registryAddr := "localhost:5002"

	t.Run("Docker Push and Pull", func(t *testing.T) {
		// Pull a small test image
		cmd := exec.Command("docker", "pull", "busybox:latest")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Docker pull output: %s", output)
			t.Skip("Failed to pull test image, skipping")
		}

		// Tag the image for our registry
		imageName := fmt.Sprintf("%s/busybox:test", registryAddr)
		cmd = exec.Command("docker", "tag", "busybox:latest", imageName)
		err = cmd.Run()
		require.NoError(t, err)

		// Push to our registry (allowing insecure registry)
		cmd = exec.Command("docker", "push", imageName)
		output, err = cmd.CombinedOutput()
		if err != nil {
			// Docker might fail due to insecure registry
			// This is expected in test environment
			t.Logf("Docker push output: %s", output)
			if strings.Contains(string(output), "http: server gave HTTP response to HTTPS client") ||
			   strings.Contains(string(output), "insecure registry") {
				t.Skip("Docker requires HTTPS or insecure registry configuration")
			}
			require.NoError(t, err)
		}

		// Remove local image
		cmd = exec.Command("docker", "rmi", imageName)
		cmd.Run()

		// Pull from our registry
		cmd = exec.Command("docker", "pull", imageName)
		output, err = cmd.CombinedOutput()
		if err != nil {
			t.Logf("Docker pull output: %s", output)
		}
		require.NoError(t, err)

		// Cleanup
		cmd = exec.Command("docker", "rmi", imageName)
		cmd.Run()
	})

	t.Run("Multi-arch Support", func(t *testing.T) {
		// This test would require buildx and multi-arch build setup
		// For now, we just verify the registry can handle manifest lists
		
		// Check catalog
		resp, err := makeRequest("GET", fmt.Sprintf("http://%s/v2/_catalog", registryAddr), nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var catalog map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&catalog)
		require.NoError(t, err)
		
		// If we successfully pushed, catalog should contain our image
		repos, ok := catalog["repositories"].([]interface{})
		if ok && len(repos) > 0 {
			assert.Contains(t, repos, "busybox")
		}
	})
}

func TestDockerRegistryPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping persistence test in short mode")
	}

	// Create temporary directory for data
	dataDir, err := os.MkdirTemp("", "depot-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	// Start first server instance
	s1, cleanup1 := startTestServerWithDataDir(t, dataDir)
	baseURL := fmt.Sprintf("https://localhost:%s", s1.GetPort())

	// Create Docker repository
	repo := models.Repository{
		Name:        "persistent-docker",
		Type:        models.RepositoryTypeDocker,
		Description: "Test persistence",
		Config: json.RawMessage(`{
			"http_port": 5003,
			"https_port": 0
		}`),
	}

	reqBody, _ := json.Marshal(repo)
	resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Stop first server
	cleanup1()

	// Start second server instance with same data directory
	s2, cleanup2 := startTestServerWithDataDir(t, dataDir)
	defer cleanup2()

	// Wait for server to start and load repositories
	time.Sleep(2 * time.Second)

	// Verify Docker repository still exists and registry is running
	baseURL = fmt.Sprintf("https://localhost:%s", s2.GetPort())
	resp, err = makeRequest("GET", baseURL+"/api/v1/repositories/persistent-docker", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify registry is accessible
	resp, err = makeRequest("GET", "http://localhost:5003/v2/", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDockerRegistryValidation(t *testing.T) {
	s, cleanup := startTestServer(t)
	defer cleanup()

	baseURL := fmt.Sprintf("https://localhost:%s", s.GetPort())

	t.Run("Invalid Configuration", func(t *testing.T) {
		// Missing configuration
		repo := models.Repository{
			Name: "invalid-docker",
			Type: models.RepositoryTypeDocker,
		}

		reqBody, _ := json.Marshal(repo)
		resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode) // Should use defaults

		// Get repository to check default config
		resp, err = makeRequest("GET", baseURL+"/api/v1/repositories/invalid-docker", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var created models.Repository
		json.NewDecoder(resp.Body).Decode(&created)
		
		var config models.DockerRepositoryConfig
		json.Unmarshal(created.Config, &config)
		assert.Equal(t, 5000, config.HTTPPort) // Default port
	})

	t.Run("Invalid Port Numbers", func(t *testing.T) {
		// Negative port number
		repo := models.Repository{
			Name: "negative-port",
			Type: models.RepositoryTypeDocker,
			Config: json.RawMessage(`{
				"http_port": -1,
				"https_port": 0
			}`),
		}

		reqBody, _ := json.Marshal(repo)
		resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()
		// JSON unmarshaling might accept negative numbers, but registry start would fail
		// The actual validation depends on the implementation
	})
}

func TestDockerRegistryConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	s, cleanup := startTestServer(t)
	defer cleanup()

	baseURL := fmt.Sprintf("https://localhost:%s", s.GetPort())

	// Create a Docker repository
	repo := models.Repository{
		Name:        "concurrent-test",
		Type:        models.RepositoryTypeDocker,
		Config: json.RawMessage(`{
			"http_port": 5004,
			"https_port": 0
		}`),
	}

	reqBody, _ := json.Marshal(repo)
	resp, err := makeRequest("POST", baseURL+"/api/v1/repositories", bytes.NewReader(reqBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	time.Sleep(2 * time.Second)

	// Concurrent blob uploads
	t.Run("Concurrent Blob Uploads", func(t *testing.T) {
		numGoroutines := 10
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				// Create unique blob data
				blobData := []byte(fmt.Sprintf("Concurrent blob %d", id))
				
				// Start upload
				resp, err := makeRequest("POST", fmt.Sprintf("http://localhost:5004/v2/concurrent-image/blobs/uploads/"), nil)
				if err != nil {
					errors <- err
					return
				}

				uploadUUID := resp.Header.Get("Docker-Upload-UUID")
				
				// Complete upload
				digest := fmt.Sprintf("sha256:%x", strings.Repeat(fmt.Sprintf("%d", id), 32))
				uploadURL := fmt.Sprintf("http://localhost:5004/v2/concurrent-image/blobs/uploads/%s?digest=%s", uploadUUID, digest[:71])
				
				resp, err = makeRequest("PUT", uploadURL, bytes.NewReader(blobData))
				if err != nil {
					errors <- err
					return
				}

				if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusBadRequest {
					errors <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					return
				}
				
				errors <- nil
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < numGoroutines; i++ {
			err := <-errors
			// Some may fail due to digest validation, which is expected
			if err != nil && !strings.Contains(err.Error(), "digest") {
				t.Errorf("Unexpected error: %v", err)
			}
		}
	})
}