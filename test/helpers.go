package test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/depot/depot/internal/server"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// startTestServer starts a test server with generated certificates
func startTestServer(t *testing.T) (*server.Server, func()) {
	tmpDir := t.TempDir()
	return startTestServerWithDataDir(t, tmpDir)
}

// startTestServerWithDataDir starts a test server with a specific data directory
func startTestServerWithDataDir(t *testing.T, dataDir string) (*server.Server, func()) {
	certFile := filepath.Join(dataDir, "server.crt")
	keyFile := filepath.Join(dataDir, "server.key")
	
	err := generateTestCertificate(certFile, keyFile)
	require.NoError(t, err, "Failed to generate test certificate")

	config := &server.Config{
		Host:         "127.0.0.1",
		Port:         "0", // Use random port
		DataDir:      filepath.Join(dataDir, "data"),
		CertFile:     certFile,
		KeyFile:      keyFile,
		DatabasePath: filepath.Join(dataDir, "depot.db"),
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	srv, err := server.New(config, logger)
	require.NoError(t, err, "Failed to create server")

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := srv.Start(ctx); err != nil {
			errChan <- err
		}
	}()

	// Wait for server to start
	select {
	case err := <-errChan:
		cancel()
		t.Fatalf("Server failed to start: %v", err)
	case <-time.After(2 * time.Second):
		// Server started successfully
	}

	cleanup := func() {
		cancel()
		// Wait a bit for server to shut down
		time.Sleep(500 * time.Millisecond)
	}

	return srv, cleanup
}

// makeRequest makes an HTTP request with TLS verification disabled (for test certificates)
func makeRequest(method, url string, body io.Reader) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	
	return client.Do(req)
}

// waitForServer waits for a server to be ready on the given address
func waitForServer(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		resp, err := makeRequest("GET", fmt.Sprintf("%s/api/v1/health", address), nil)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		
		time.Sleep(100 * time.Millisecond)
	}
	
	return fmt.Errorf("server not ready after %v", timeout)
}

// generateTestCertificate generates a self-signed certificate for testing
func generateTestCertificate(certFile, keyFile string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()

	privKeyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privKeyDER}); err != nil {
		return err
	}

	return nil
}