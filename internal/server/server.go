package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/depot/depot/internal/api"
	"github.com/depot/depot/internal/storage"
	"github.com/sirupsen/logrus"
	"go.etcd.io/bbolt"
)

type Server struct {
	config     *Config
	logger     *logrus.Logger
	router     *mux.Router
	httpServer *http.Server
	db         *bbolt.DB
	storage    storage.Storage
}

func New(config *Config, logger *logrus.Logger) (*Server, error) {
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := bbolt.Open(config.DatabasePath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	fileStorage := storage.NewFileStorage(filepath.Join(config.DataDir, "artifacts"))
	
	s := &Server{
		config:  config,
		logger:  logger,
		router:  mux.NewRouter(),
		db:      db,
		storage: fileStorage,
	}

	s.setupRoutes()

	return s, nil
}

func (s *Server) setupRoutes() {
	apiHandler := api.NewHandler(s.db, s.storage, s.logger)
	
	apiRouter := s.router.PathPrefix("/api/v1").Subrouter()
	apiRouter.HandleFunc("/health", apiHandler.Health).Methods("GET")
	apiRouter.HandleFunc("/repositories", apiHandler.ListRepositories).Methods("GET")
	apiRouter.HandleFunc("/repositories", apiHandler.CreateRepository).Methods("POST")
	apiRouter.HandleFunc("/repositories/{name}", apiHandler.GetRepository).Methods("GET")
	apiRouter.HandleFunc("/repositories/{name}", apiHandler.DeleteRepository).Methods("DELETE")
	
	repoRouter := s.router.PathPrefix("/repository").Subrouter()
	repoRouter.PathPrefix("/").HandlerFunc(apiHandler.HandleRepository)
}

func (s *Server) Start(ctx context.Context) error {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%s", s.config.Host, s.config.Port),
		Handler:      s.router,
		TLSConfig:    tlsConfig,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	if s.config.Port == "0" {
		addr := listener.Addr().(*net.TCPAddr)
		s.config.Port = fmt.Sprintf("%d", addr.Port)
		s.logger.Infof("Using dynamic port: %s", s.config.Port)
	}

	tlsListener := tls.NewListener(listener, s.httpServer.TLSConfig)

	errChan := make(chan error, 1)

	go func() {
		s.logger.Infof("Starting HTTPS server on %s", listener.Addr().String())
		
		// Load certificate
		cert, err := tls.LoadX509KeyPair(s.config.CertFile, s.config.KeyFile)
		if err != nil {
			errChan <- fmt.Errorf("failed to load certificates: %w", err)
			return
		}
		
		// Update TLS config with certificate
		s.httpServer.TLSConfig.Certificates = []tls.Certificate{cert}
		
		// Use Serve instead of ServeTLS since we already have a TLS listener
		if err := s.httpServer.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
			errChan <- err
		} else {
			// Server closed normally, send nil to indicate success
			errChan <- nil
		}
	}()

	select {
	case <-ctx.Done():
		if err := s.shutdown(); err != nil {
			return err
		}
		// Wait for server goroutine to finish
		<-errChan
		return nil
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.WithError(err).Error("Failed to shutdown HTTP server")
	}

	if err := s.db.Close(); err != nil {
		s.logger.WithError(err).Error("Failed to close database")
		return err
	}

	return nil
}

func (s *Server) GetPort() string {
	return s.config.Port
}