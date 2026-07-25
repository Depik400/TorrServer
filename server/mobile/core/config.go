package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ApplicationSupportPath         string `json:"applicationSupportPath"`
	CachePath                      string `json:"cachePath"`
	ListenHost                     string `json:"listenHost"`
	ListenPort                     int    `json:"listenPort"`
	CacheSizeBytes                 int64  `json:"cacheSizeBytes"`
	DiskCacheEnabled               bool   `json:"diskCacheEnabled"`
	DownloadRateLimitKB            int    `json:"downloadRateLimitKB"`
	UploadRateLimitKB              int    `json:"uploadRateLimitKB"`
	ConnectionsLimit               int    `json:"connectionsLimit"`
	TorrentDisconnectTimeoutSeconds int   `json:"torrentDisconnectTimeoutSeconds"`
	DisableUpload                  bool   `json:"disableUpload"`
	DisableUPnP                    bool   `json:"disableUPnP"`
	DisableIPv6                    bool   `json:"disableIPv6"`
	Debug                          bool   `json:"debug"`
}

func ParseConfig(jsonData string) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		return nil, newEngineError(ErrInvalidJSON, "failed to parse config JSON: "+err.Error())
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.ApplicationSupportPath == "" || !filepath.IsAbs(c.ApplicationSupportPath) {
		return newEngineError(ErrInvalidArgument, "applicationSupportPath must be an absolute non-empty path")
	}
	if c.CachePath == "" || !filepath.IsAbs(c.CachePath) {
		return newEngineError(ErrInvalidArgument, "cachePath must be an absolute non-empty path")
	}

	if c.ListenHost == "" {
		c.ListenHost = "127.0.0.1"
	}
	if c.ListenHost != "127.0.0.1" && c.ListenHost != "::1" {
		return newEngineError(ErrInvalidArgument, "listenHost must be 127.0.0.1 or ::1")
	}

	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return newEngineError(ErrInvalidArgument, "listenPort must be 0-65535")
	}

	const minCache = 32 * 1024 * 1024
	const maxCache = 1 * 1024 * 1024 * 1024
	if c.CacheSizeBytes < minCache {
		c.CacheSizeBytes = minCache
	}
	if c.CacheSizeBytes > maxCache {
		c.CacheSizeBytes = maxCache
	}

	if c.ConnectionsLimit <= 0 {
		c.ConnectionsLimit = 25
	}

	if c.TorrentDisconnectTimeoutSeconds <= 0 {
		c.TorrentDisconnectTimeoutSeconds = 120
	}

	if err := os.MkdirAll(c.ApplicationSupportPath, 0755); err != nil {
		return newEngineError(ErrInvalidArgument, fmt.Sprintf("cannot create applicationSupportPath: %v", err))
	}
	if err := os.MkdirAll(c.CachePath, 0755); err != nil {
		return newEngineError(ErrInvalidArgument, fmt.Sprintf("cannot create cachePath: %v", err))
	}

	return nil
}
