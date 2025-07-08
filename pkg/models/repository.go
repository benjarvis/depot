package models

import (
	"encoding/json"
	"time"
)

type RepositoryType string

const (
	RepositoryTypeDocker RepositoryType = "docker"
	RepositoryTypeRaw    RepositoryType = "raw"
)

type Repository struct {
	Name        string         `json:"name"`
	Type        RepositoryType `json:"type"`
	Description string         `json:"description,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Config      json.RawMessage `json:"config,omitempty"`
}

type DockerRepositoryConfig struct {
	HTTPPort  int  `json:"http_port,omitempty"`
	HTTPSPort int  `json:"https_port,omitempty"`
	V1Enabled bool `json:"v1_enabled"`
}

type RawRepositoryConfig struct {
	ContentTypes []string `json:"content_types,omitempty"`
}