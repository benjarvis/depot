package test

import (
	"context"
	"crypto/tls"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/depot/depot/internal/server"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerStartStop(t *testing.T) {
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

	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-serverErrCh:
		t.Fatalf("Server failed to start: %v", err)
	default:
	}

	httpsURL := "https://" + config.Host + ":" + srv.GetPort() + "/api/v1/health"
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
	
	var resp *http.Response
	for i := 0; i < 10; i++ {
		resp, err = client.Get(httpsURL)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	if resp != nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return 200 OK")
	}

	cancel()

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down within timeout")
	case err := <-serverErrCh:
		assert.NoError(t, err, "Server should shut down without error")
	}
}