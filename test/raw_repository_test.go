package test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/depot/depot/internal/server"
	"github.com/depot/depot/pkg/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawRepositoryOperations(t *testing.T) {
	// Setup server
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")
	
	err := generateTestCertificate(certFile, keyFile)
	require.NoError(t, err, "Failed to generate test certificate")

	config := &server.Config{
		Host:         "127.0.0.1",
		Port:         "0", // Use random port
		DataDir:      filepath.Join(tmpDir, "data"),
		CertFile:     certFile,
		KeyFile:      keyFile,
		DatabasePath: filepath.Join(tmpDir, "depot.db"),
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	srv, err := server.New(config, logger)
	require.NoError(t, err, "Failed to create server")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErrCh := make(chan error, 1)
	go func() {
		err := srv.Start(ctx)
		serverErrCh <- err
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Create HTTP client
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	baseURL := fmt.Sprintf("https://%s:%s", config.Host, srv.GetPort())

	// Test 1: Create a raw repository
	t.Run("CreateRawRepository", func(t *testing.T) {
		repo := models.Repository{
			Name:        "test-raw-repo",
			Type:        models.RepositoryTypeRaw,
			Description: "Test raw repository for artifacts",
		}

		body, err := json.Marshal(repo)
		require.NoError(t, err)

		req, err := http.NewRequest("POST", baseURL+"/api/v1/repositories", bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var createdRepo models.Repository
		err = json.NewDecoder(resp.Body).Decode(&createdRepo)
		require.NoError(t, err)
		assert.Equal(t, repo.Name, createdRepo.Name)
		assert.Equal(t, repo.Type, createdRepo.Type)
	})

	// Test 2: Upload artifacts with various paths
	artifacts := map[string][]byte{
		"simple.txt":                      []byte("Simple artifact content"),
		"path/to/nested.jar":              []byte("Nested JAR content"),
		"deep/path/with/many/slashes.zip": []byte("Deep path ZIP content"),
		"version/1.0.0/app.tar.gz":        []byte("Versioned artifact content"),
	}

	t.Run("UploadArtifacts", func(t *testing.T) {
		for path, content := range artifacts {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				req, err := http.NewRequest("PUT", url, bytes.NewBuffer(content))
				require.NoError(t, err)

				resp, err := client.Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusCreated, resp.StatusCode)
			})
		}
	})

	// Test 3: Check artifacts exist using HEAD
	t.Run("CheckArtifactsExist", func(t *testing.T) {
		for path := range artifacts {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				req, err := http.NewRequest("HEAD", url, nil)
				require.NoError(t, err)

				resp, err := client.Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)
			})
		}
	})

	// Test 4: Download artifacts and verify content
	t.Run("DownloadArtifacts", func(t *testing.T) {
		for path, expectedContent := range artifacts {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				resp, err := client.Get(url)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)

				downloadedContent, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Equal(t, expectedContent, downloadedContent)
			})
		}
	})

	// Test 5: Test non-existent artifact
	t.Run("DownloadNonExistentArtifact", func(t *testing.T) {
		url := fmt.Sprintf("%s/repository/test-raw-repo/does/not/exist.txt", baseURL)
		resp, err := client.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// Test 6: Delete artifacts
	t.Run("DeleteArtifacts", func(t *testing.T) {
		// Delete only some artifacts
		artifactsToDelete := []string{"simple.txt", "deep/path/with/many/slashes.zip"}
		
		for _, path := range artifactsToDelete {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				req, err := http.NewRequest("DELETE", url, nil)
				require.NoError(t, err)

				resp, err := client.Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusNoContent, resp.StatusCode)
			})
		}
	})

	// Test 7: Verify deleted artifacts are gone
	t.Run("VerifyDeletedArtifacts", func(t *testing.T) {
		deletedPaths := []string{"simple.txt", "deep/path/with/many/slashes.zip"}
		
		for _, path := range deletedPaths {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				resp, err := client.Get(url)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			})
		}
	})

	// Test 8: Verify remaining artifacts still exist
	t.Run("VerifyRemainingArtifacts", func(t *testing.T) {
		remainingPaths := []string{"path/to/nested.jar", "version/1.0.0/app.tar.gz"}
		
		for _, path := range remainingPaths {
			t.Run(path, func(t *testing.T) {
				url := fmt.Sprintf("%s/repository/test-raw-repo/%s", baseURL, path)
				resp, err := client.Get(url)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)
			})
		}
	})

	// Test 9: Test operations on non-existent repository
	t.Run("NonExistentRepository", func(t *testing.T) {
		url := fmt.Sprintf("%s/repository/non-existent-repo/some/file.txt", baseURL)
		
		// Try to upload
		req, err := http.NewRequest("PUT", url, bytes.NewBuffer([]byte("test")))
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		// Try to download
		resp, err = client.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// Cleanup: Delete the repository
	t.Run("DeleteRepository", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/repositories/test-raw-repo", baseURL)
		req, err := http.NewRequest("DELETE", url, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	// Shutdown server
	cancel()
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down within timeout")
	case err := <-serverErrCh:
		assert.NoError(t, err, "Server should shut down without error")
	}
}