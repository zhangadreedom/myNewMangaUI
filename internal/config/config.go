package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	Storage  StorageConfig  `json:"storage"`
	LogLevel string         `json:"logLevel"`
}

type ServerConfig struct {
	Address string `json:"address"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type StorageConfig struct {
	LibraryRoots []string `json:"libraryRoots"`
	CachePath    string   `json:"cachePath"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := defaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Address: ":8080",
		},
		Database: DatabaseConfig{
			Path: "./data/app.db",
		},
		Storage: StorageConfig{
			LibraryRoots: []string{},
			CachePath:    "./cache/thumbs",
		},
		LogLevel: "info",
	}
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Server.Address) == "" {
		return fmt.Errorf("server.address is required")
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		return fmt.Errorf("database.path is required")
	}
	if strings.TrimSpace(c.Storage.CachePath) == "" {
		return fmt.Errorf("storage.cachePath is required")
	}
	for i, root := range c.Storage.LibraryRoots {
		if strings.TrimSpace(root) == "" {
			return fmt.Errorf("storage.libraryRoots[%d] is empty", i)
		}
	}
	return nil
}

func EnsurePaths(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		return fmt.Errorf("create database dir: %w", err)
	}
	if err := os.MkdirAll(cfg.Storage.CachePath, 0o755); err != nil {
		return fmt.Errorf("create cache path: %w", err)
	}
	return nil
}
