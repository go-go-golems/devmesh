// Package config defines the devmesh daemon configuration model and the
// mapping from the devmesh JSON config-file shape onto Glazed command fields.
//
// Configuration precedence is owned by Glazed's middleware chain
// (defaults < config file < env < args < flags). This package no longer reads
// environment variables or files itself; it exposes the in-process Config type,
// its compiled defaults, and FileMapper for the config-file middleware.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SectionSlug is the Glazed section that `devmeshd serve` configuration fields
// live in. It must equal glazed's schema.DefaultSlug.
const SectionSlug = "default"

// Lease TTL bounds shared by config validation and the daemon. Requests or
// configured defaults outside this range are rejected instead of engineering
// for impractically short deadlines; the client heartbeat interval (TTL/3)
// needs real margin against scheduling delay.
const (
	MinLeaseTTL = 3 * time.Second
	MaxLeaseTTL = time.Hour
)

// DockerConfig controls the Docker adapter.
type DockerConfig struct {
	Enabled                        bool `json:"enabled"`
	AllowNonLoopbackPublishedPorts bool `json:"allow_non_loopback_published_ports"`
}

// HTTPConfig controls the HTTP/TLS proxy phase.
type HTTPConfig struct {
	Enabled    bool   `json:"enabled"`
	HTTPAddr   string `json:"http_addr"`
	HTTPSAddr  string `json:"https_addr"`
	BaseDomain string `json:"base_domain,omitempty"`
	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
}

// Config is the in-process daemon configuration.
type Config struct {
	Socket          string        `json:"socket"`
	TCPFrontendHost string        `json:"tcp_frontend_host"`
	TCPFrontendMin  int           `json:"tcp_frontend_min"`
	TCPFrontendMax  int           `json:"tcp_frontend_max"`
	LeaseTTL        time.Duration `json:"lease_ttl"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	StatePath       string        `json:"state_path"`
	Docker          DockerConfig  `json:"docker"`
	HTTP            HTTPConfig    `json:"http"`
}

// Default returns the compiled defaults. These mirror the Glazed field defaults
// declared by `devmeshd serve`; keep the two in sync.
func Default() Config {
	return Config{
		TCPFrontendHost: "127.0.0.1",
		TCPFrontendMin:  15000,
		TCPFrontendMax:  19999,
		LeaseTTL:        15 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		Docker:          DockerConfig{Enabled: true},
		HTTP: HTTPConfig{
			Enabled:   false,
			HTTPAddr:  "127.0.0.1:8088",
			HTTPSAddr: "127.0.0.1:8443",
		},
	}
}

// Validate checks effective configuration after Glazed decoding. It is the
// single place that rejects malformed values instead of silently substituting
// defaults.
func (c Config) Validate() error {
	if c.TCPFrontendMin < 1 || c.TCPFrontendMax > 65535 || c.TCPFrontendMin > c.TCPFrontendMax {
		return fmt.Errorf("invalid tcp frontend port range %d-%d", c.TCPFrontendMin, c.TCPFrontendMax)
	}
	if c.TCPFrontendHost == "" {
		return fmt.Errorf("tcp frontend host is empty")
	}
	if c.LeaseTTL < MinLeaseTTL || c.LeaseTTL > MaxLeaseTTL {
		return fmt.Errorf("lease_ttl must be between %s and %s", MinLeaseTTL, MaxLeaseTTL)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be positive")
	}
	if c.HTTP.Enabled {
		if _, _, err := net.SplitHostPort(c.HTTP.HTTPAddr); err != nil {
			return fmt.Errorf("invalid http_addr %q: %w", c.HTTP.HTTPAddr, err)
		}
		if c.HTTP.CertFile != "" && c.HTTP.KeyFile != "" {
			if _, _, err := net.SplitHostPort(c.HTTP.HTTPSAddr); err != nil {
				return fmt.Errorf("invalid https_addr %q: %w", c.HTTP.HTTPSAddr, err)
			}
		}
	}
	return nil
}

// DefaultStatePath returns the OS-appropriate state file path.
func DefaultStatePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "devmesh-state.json"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "devmesh", "state.json")
}

// FileMapper transforms the devmesh JSON config-file shape into the standard
// Glazed section map. The on-disk format stays stable: flat snake_case keys
// plus nested docker/http objects, for example:
//
//	{
//	  "tcp_frontend_min": 15000,
//	  "lease_ttl": "15s",
//	  "docker": {"enabled": true, "allow_non_loopback_published_ports": false},
//	  "http":   {"enabled": false, "http_addr": "127.0.0.1:8088"}
//	}
//
// It maps onto the default section fields (kebab-case) declared by
// `devmeshd serve`.
func FileMapper(raw any) (map[string]map[string]any, error) {
	root, err := asStringMap(raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	put := func(field string, v any) {
		if v != nil {
			out[field] = v
		}
	}

	for key, v := range root {
		switch key {
		case "runtime_idle_ttl":
			return nil, fmt.Errorf("runtime_idle_ttl is no longer supported: frontend listeners are kept until the daemon stops")
		case "socket":
			put("socket", v)
		case "state_path":
			put("state", v)
		case "tcp_frontend_host":
			put("tcp-frontend-host", v)
		case "tcp_frontend_min":
			put("tcp-frontend-min", v)
		case "tcp_frontend_max":
			put("tcp-frontend-max", v)
		case "lease_ttl":
			put("lease-ttl", v)
		case "shutdown_timeout":
			put("shutdown-timeout", v)
		case "docker":
			m, err := asStringMap(v)
			if err != nil {
				return nil, fmt.Errorf("docker: %w", err)
			}
			put("docker-enabled", m["enabled"])
			put("docker-allow-non-loopback-published-ports", m["allow_non_loopback_published_ports"])
		case "http":
			m, err := asStringMap(v)
			if err != nil {
				return nil, fmt.Errorf("http: %w", err)
			}
			put("http-enabled", m["enabled"])
			put("http-addr", m["http_addr"])
			put("https-addr", m["https_addr"])
			put("http-base-domain", m["base_domain"])
			put("http-cert-file", m["cert_file"])
			put("http-key-file", m["key_file"])
		}
	}
	return map[string]map[string]any{SectionSlug: out}, nil
}

// asStringMap normalizes the map types produced by YAML and JSON unmarshalling.
func asStringMap(v any) (map[string]any, error) {
	switch m := v.(type) {
	case map[string]any:
		return m, nil
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("config map key %v is not a string", k)
			}
			out[ks] = val
		}
		return out, nil
	case nil:
		return map[string]any{}, nil
	default:
		if raw, ok := v.(json.RawMessage); ok {
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err == nil {
				return decoded, nil
			}
		}
		return nil, fmt.Errorf("expected a mapping, got %T", v)
	}
}
