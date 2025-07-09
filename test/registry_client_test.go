package test

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/depot/depot/pkg/models"
)

// RegistryClient is a simple Docker Registry V2 client for testing
type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRegistryClient creates a new registry client
func NewRegistryClient(baseURL string) *RegistryClient {
	return &RegistryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}

// Ping checks if the registry is accessible
func (c *RegistryClient) Ping() error {
	resp, err := c.httpClient.Get(c.baseURL + "/v2/")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	
	return nil
}

// PushBlob uploads a blob to the registry
func (c *RegistryClient) PushBlob(repo string, data []byte) (string, error) {
	// Calculate digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	
	// Start upload
	resp, err := c.httpClient.Post(
		fmt.Sprintf("%s/v2/%s/blobs/uploads/", c.baseURL, repo),
		"application/octet-stream",
		nil,
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("failed to start upload: %d", resp.StatusCode)
	}
	
	uploadURL := resp.Header.Get("Location")
	if uploadURL == "" {
		return "", fmt.Errorf("no upload location provided")
	}
	
	// Complete upload
	fullURL := c.baseURL + uploadURL
	if !strings.Contains(uploadURL, "?") {
		fullURL += "?"
	} else {
		fullURL += "&"
	}
	fullURL += "digest=" + url.QueryEscape(digest)
	
	req, err := http.NewRequest("PUT", fullURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to complete upload: %d - %s", resp.StatusCode, body)
	}
	
	return digest, nil
}

// PushManifest uploads a manifest to the registry
func (c *RegistryClient) PushManifest(repo, tag string, manifest interface{}) (string, error) {
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	
	req, err := http.NewRequest(
		"PUT",
		fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repo, tag),
		bytes.NewReader(manifestData),
	)
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to push manifest: %d - %s", resp.StatusCode, body)
	}
	
	return resp.Header.Get("Docker-Content-Digest"), nil
}

// PullManifest retrieves a manifest from the registry
func (c *RegistryClient) PullManifest(repo, reference string) ([]byte, error) {
	resp, err := c.httpClient.Get(
		fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repo, reference),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to pull manifest: %d", resp.StatusCode)
	}
	
	return io.ReadAll(resp.Body)
}

// GetCatalog lists repositories in the registry
func (c *RegistryClient) GetCatalog() ([]string, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/v2/_catalog")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get catalog: %d", resp.StatusCode)
	}
	
	var result struct {
		Repositories []string `json:"repositories"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result.Repositories, nil
}

// GetTags lists tags for a repository
func (c *RegistryClient) GetTags(repo string) ([]string, error) {
	resp, err := c.httpClient.Get(
		fmt.Sprintf("%s/v2/%s/tags/list", c.baseURL, repo),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get tags: %d", resp.StatusCode)
	}
	
	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result.Tags, nil
}

func TestDockerRegistryWithClient(t *testing.T) {
	s, cleanup := startTestServer(t)
	defer cleanup()

	baseURL := fmt.Sprintf("https://localhost:%s", s.GetPort())

	// Create a Docker repository
	repo := models.Repository{
		Name:        "test-registry",
		Type:        models.RepositoryTypeDocker,
		Description: "Test Docker registry",
		Config: json.RawMessage(`{
			"http_port": 5555,
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

	// Create registry client
	client := NewRegistryClient("http://localhost:5555")

	t.Run("Ping Registry", func(t *testing.T) {
		err := client.Ping()
		assert.NoError(t, err)
	})

	t.Run("Push and Pull Complete Image", func(t *testing.T) {
		// 1. Push config blob
		configData := []byte(`{
			"architecture": "amd64",
			"os": "linux",
			"config": {
				"Env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],
				"Cmd": ["/bin/sh"]
			},
			"rootfs": {
				"type": "layers",
				"diff_ids": ["sha256:1234567890abcdef"]
			}
		}`)
		
		configDigest, err := client.PushBlob("test-image", configData)
		require.NoError(t, err)
		assert.NotEmpty(t, configDigest)

		// 2. Push layer blob
		layerData := []byte("This is a fake layer content for testing")
		layerDigest, err := client.PushBlob("test-image", layerData)
		require.NoError(t, err)
		assert.NotEmpty(t, layerDigest)

		// 3. Create and push manifest
		manifest := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
			"config": map[string]interface{}{
				"mediaType": "application/vnd.docker.container.image.v1+json",
				"size":      int64(len(configData)),
				"digest":    configDigest,
			},
			"layers": []map[string]interface{}{
				{
					"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
					"size":      int64(len(layerData)),
					"digest":    layerDigest,
				},
			},
		}

		manifestDigest, err := client.PushManifest("test-image", "v1.0", manifest)
		require.NoError(t, err)
		assert.NotEmpty(t, manifestDigest)

		// 4. Pull manifest back
		pulledManifest, err := client.PullManifest("test-image", "v1.0")
		require.NoError(t, err)
		assert.NotEmpty(t, pulledManifest)

		// 5. Verify manifest content
		var retrieved map[string]interface{}
		err = json.Unmarshal(pulledManifest, &retrieved)
		require.NoError(t, err)
		assert.Equal(t, float64(2), retrieved["schemaVersion"])
	})

	t.Run("Multi-arch Image", func(t *testing.T) {
		// Push manifests for different architectures
		architectures := []string{"amd64", "arm64"}
		manifestDigests := make(map[string]string)

		for _, arch := range architectures {
			configData := []byte(fmt.Sprintf(`{
				"architecture": "%s",
				"os": "linux"
			}`, arch))
			
			configDigest, err := client.PushBlob("multi-arch-image", configData)
			require.NoError(t, err)

			manifest := map[string]interface{}{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
				"config": map[string]interface{}{
					"mediaType": "application/vnd.docker.container.image.v1+json",
					"size":      int64(len(configData)),
					"digest":    configDigest,
				},
				"layers": []map[string]interface{}{},
			}

			digest, err := client.PushManifest("multi-arch-image", arch, manifest)
			require.NoError(t, err)
			manifestDigests[arch] = digest
		}

		// Create and push manifest list
		manifestList := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.docker.distribution.manifest.list.v2+json",
			"manifests": []map[string]interface{}{
				{
					"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
					"size":      1234, // Placeholder
					"digest":    manifestDigests["amd64"],
					"platform": map[string]interface{}{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				{
					"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
					"size":      1234, // Placeholder
					"digest":    manifestDigests["arm64"],
					"platform": map[string]interface{}{
						"architecture": "arm64",
						"os":           "linux",
					},
				},
			},
		}

		_, err := client.PushManifest("multi-arch-image", "latest", manifestList)
		require.NoError(t, err)

		// Verify we can pull the manifest list
		pulled, err := client.PullManifest("multi-arch-image", "latest")
		require.NoError(t, err)
		
		var pulledList map[string]interface{}
		err = json.Unmarshal(pulled, &pulledList)
		require.NoError(t, err)
		
		manifests, ok := pulledList["manifests"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, manifests, 2)
	})

	t.Run("Catalog and Tags", func(t *testing.T) {
		// Get catalog
		repos, err := client.GetCatalog()
		require.NoError(t, err)
		assert.Contains(t, repos, "test-image")
		assert.Contains(t, repos, "multi-arch-image")

		// Get tags
		tags, err := client.GetTags("test-image")
		require.NoError(t, err)
		assert.Contains(t, tags, "v1.0")
	})
}