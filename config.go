package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
)

// Config controls which listeners the daemon opens.
//
// HTTP and HTTPS are independently optional (requirements v2):
//   - HTTP only:  no mkcert, no CA, nothing TLS-related is required.
//   - HTTPS only: mkcert certificates are required.
//   - Both:       HTTP exists only to redirect to HTTPS.
//   - Neither:    rejected at startup.
type Config struct {
	HTTPEnabled  bool   `json:"http_enabled"`
	HTTPSEnabled bool   `json:"https_enabled"`
	HTTPPort     int    `json:"http_port"`
	HTTPSPort    int    `json:"https_port"`
	CertFile     string `json:"cert_file"`
	KeyFile      string `json:"key_file"`
	DataFile     string `json:"data_file"`
}

func defaultConfig() Config {
	return Config{
		HTTPEnabled:  true,
		HTTPSEnabled: false,
		HTTPPort:     80,
		HTTPSPort:    443,
		DataFile:     "links.json",
	}
}

// loadConfig reads the JSON config file at path. A missing file is not an
// error: the defaults (HTTP only on port 80) apply, so a fresh checkout runs
// with zero setup.
func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if !c.HTTPEnabled && !c.HTTPSEnabled {
		return fmt.Errorf("invalid config: http_enabled and https_enabled are both false; enable at least one listener")
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("invalid config: http_port %d out of range", c.HTTPPort)
	}
	if c.HTTPSPort < 1 || c.HTTPSPort > 65535 {
		return fmt.Errorf("invalid config: https_port %d out of range", c.HTTPSPort)
	}
	if c.HTTPEnabled && c.HTTPSEnabled && c.HTTPPort == c.HTTPSPort {
		return fmt.Errorf("invalid config: http_port and https_port must differ when both are enabled")
	}
	if c.DataFile == "" {
		return fmt.Errorf("invalid config: data_file must not be empty")
	}
	if c.HTTPSEnabled {
		if c.CertFile == "" || c.KeyFile == "" {
			return fmt.Errorf("invalid config: https_enabled is true but cert_file/key_file are not set; run mkcert first (see README) or disable HTTPS")
		}
		if _, err := os.Stat(c.CertFile); err != nil {
			return fmt.Errorf("invalid config: cert_file %s: %w", c.CertFile, err)
		}
		if _, err := os.Stat(c.KeyFile); err != nil {
			return fmt.Errorf("invalid config: key_file %s: %w", c.KeyFile, err)
		}
		// Fail loudly at startup if the pair does not load, instead of
		// silently degrading to HTTP-only.
		if _, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile); err != nil {
			return fmt.Errorf("invalid config: load cert/key pair: %w", err)
		}
	}
	return nil
}

// tlsConfig loads the configured key pair once for all TLS listeners.
func (c Config) tlsConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
