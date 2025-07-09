package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/depot/depot/internal/server"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	config := &server.Config{
		Host:         getEnv("DEPOT_HOST", "0.0.0.0"),
		Port:         getEnv("DEPOT_PORT", "8443"),
		DataDir:      getEnv("DEPOT_DATA_DIR", "/var/depot/data"),
		CertFile:     getEnv("DEPOT_CERT_FILE", "/var/depot/certs/server.crt"),
		KeyFile:      getEnv("DEPOT_KEY_FILE", "/var/depot/certs/server.key"),
		DatabasePath: getEnv("DEPOT_DB_PATH", "/var/depot/data/depot.db"),
	}

	srv, err := server.New(config, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	if err := srv.Start(ctx); err != nil {
		logger.WithError(err).Fatal("Server failed")
	}

	logger.Info("Server shutdown complete")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}