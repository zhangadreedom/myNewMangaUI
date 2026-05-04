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
	Online   OnlineConfig   `json:"online"`
	LogLevel string         `json:"logLevel"`
}

type ServerConfig struct {
	Address              string   `json:"address"`
	AllowPrivateNetworks bool     `json:"allowPrivateNetworks"`
	PublicAccessToken    string   `json:"publicAccessToken"`
	TrustedProxyCIDRs    []string `json:"trustedProxyCIDRs"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type StorageConfig struct {
	LibraryRoots []string          `json:"libraryRoots"`
	Bookshelves  []BookshelfConfig `json:"bookshelves"`
	CachePath    string            `json:"cachePath"`
}

type OnlineConfig struct {
	Enabled               bool                 `json:"enabled"`
	CachePath             string               `json:"cachePath"`
	DownloadsPath         string               `json:"downloadsPath"`
	RequestTimeoutSeconds int                  `json:"requestTimeoutSeconds"`
	ImageProxyTTLSeconds  int                  `json:"imageProxyTtlSeconds"`
	Sources               []OnlineSourceConfig `json:"sources"`
}

type OnlineSourceConfig struct {
	ID                    string                           `json:"id"`
	Name                  string                           `json:"name"`
	BaseURL               string                           `json:"baseURL"`
	Enabled               bool                             `json:"enabled"`
	ProxyURL              string                           `json:"proxyURL"`
	Username              string                           `json:"username"`
	Password              string                           `json:"password"`
	SessionCookie         string                           `json:"sessionCookie"`
	CookieHeader          string                           `json:"cookieHeader"`
	UserAgent             string                           `json:"userAgent"`
	MaxConcurrentRequests int                              `json:"maxConcurrentRequests"`
	RequestIntervalMs     int                              `json:"requestIntervalMs"`
	DefaultDisplay        OnlineSourceDefaultDisplayConfig `json:"defaultDisplay"`
}

type OnlineSourceDefaultDisplayConfig struct {
	Mode        string `json:"mode"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Limit       int    `json:"limit"`
}

type BookshelfConfig struct {
	Name string `json:"name"`
	Path string `json:"path"`
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
			Address:              ":8080",
			AllowPrivateNetworks: true,
		},
		Database: DatabaseConfig{
			Path: "./data/app.db",
		},
		Storage: StorageConfig{
			LibraryRoots: []string{"./local"},
			Bookshelves: []BookshelfConfig{
				{Name: "榛樿涔︽灦", Path: "./local"},
			},
			CachePath: "./cache/thumbs",
		},
		Online: OnlineConfig{
			Enabled:               false,
			CachePath:             "./cache/online",
			DownloadsPath:         "./data/downloads",
			RequestTimeoutSeconds: 20,
			ImageProxyTTLSeconds:  3600,
			Sources: []OnlineSourceConfig{
				{
					ID:                    "18comic",
					Name:                  "18comic",
					BaseURL:               "https://18comic.vip",
					Enabled:               false,
					ProxyURL:              "http://127.0.0.1:10087",
					UserAgent:             "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36",
					MaxConcurrentRequests: 3,
					RequestIntervalMs:     800,
					DefaultDisplay: OnlineSourceDefaultDisplayConfig{
						Mode:        "latest",
						Title:       "\u6700\u65b0\u6f2b\u753b",
						Description: "\u9ed8\u8ba4\u5c55\u793a\u6765\u6e90\u6700\u65b0\u66f4\u65b0\u7684\u5728\u7ebf\u6f2b\u753b\u3002",
						Limit:       30,
					},
				},
				{
					ID:                    "ehentai",
					Name:                  "Ehentai",
					BaseURL:               "https://e-hentai.org",
					Enabled:               false,
					ProxyURL:              "http://127.0.0.1:10087",
					UserAgent:             "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36",
					MaxConcurrentRequests: 3,
					RequestIntervalMs:     800,
					DefaultDisplay: OnlineSourceDefaultDisplayConfig{
						Mode:        "latest",
						Title:       "Ehentai \u6700\u65b0\u753b\u5eca",
						Description: "\u9ed8\u8ba4\u5c55\u793a Ehentai \u6700\u65b0\u66f4\u65b0\u7684\u753b\u5eca\u3002",
						Limit:       30,
					},
				},
				{
					ID:                    "pica",
					Name:                  "Pica",
					BaseURL:               "https://picaapi.picacomic.com",
					Enabled:               false,
					ProxyURL:              "http://127.0.0.1:10087",
					UserAgent:             "okhttp/3.8.1",
					MaxConcurrentRequests: 3,
					RequestIntervalMs:     800,
					DefaultDisplay: OnlineSourceDefaultDisplayConfig{
						Mode:        "latest",
						Title:       "Pica \u6700\u65b0\u6f2b\u753b",
						Description: "\u9ed8\u8ba4\u5c55\u793a Pica \u6700\u65b0\u66f4\u65b0\u7684\u5728\u7ebf\u6f2b\u753b\u3002",
						Limit:       30,
					},
				},
			},
		},
		LogLevel: "info",
	}
}

func (c *Config) normalize() {
	if len(c.Storage.Bookshelves) == 0 {
		for _, root := range c.Storage.LibraryRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			name := filepath.Base(root)
			if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
				name = root
			}
			c.Storage.Bookshelves = append(c.Storage.Bookshelves, BookshelfConfig{
				Name: name,
				Path: root,
			})
		}
	}
}

func (c *Config) validate() error {
	c.normalize()

	if strings.TrimSpace(c.Server.Address) == "" {
		return fmt.Errorf("server.address is required")
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		return fmt.Errorf("database.path is required")
	}
	if strings.TrimSpace(c.Storage.CachePath) == "" {
		return fmt.Errorf("storage.cachePath is required")
	}
	if c.Online.Enabled {
		if strings.TrimSpace(c.Online.CachePath) == "" {
			return fmt.Errorf("online.cachePath is required when online is enabled")
		}
		if strings.TrimSpace(c.Online.DownloadsPath) == "" {
			return fmt.Errorf("online.downloadsPath is required when online is enabled")
		}
	}
	if len(c.Storage.Bookshelves) == 0 {
		return fmt.Errorf("storage.bookshelves is required")
	}
	for i, shelf := range c.Storage.Bookshelves {
		if strings.TrimSpace(shelf.Name) == "" {
			return fmt.Errorf("storage.bookshelves[%d].name is empty", i)
		}
		if strings.TrimSpace(shelf.Path) == "" {
			return fmt.Errorf("storage.bookshelves[%d].path is empty", i)
		}
	}
	for i, source := range c.Online.Sources {
		if strings.TrimSpace(source.ID) == "" {
			return fmt.Errorf("online.sources[%d].id is empty", i)
		}
		if strings.TrimSpace(source.Name) == "" {
			return fmt.Errorf("online.sources[%d].name is empty", i)
		}
		if strings.TrimSpace(source.BaseURL) == "" {
			return fmt.Errorf("online.sources[%d].baseURL is empty", i)
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
	if cfg.Online.CachePath != "" {
		if err := os.MkdirAll(cfg.Online.CachePath, 0o755); err != nil {
			return fmt.Errorf("create online cache path: %w", err)
		}
	}
	if cfg.Online.DownloadsPath != "" {
		if err := os.MkdirAll(cfg.Online.DownloadsPath, 0o755); err != nil {
			return fmt.Errorf("create online downloads path: %w", err)
		}
	}
	return nil
}
