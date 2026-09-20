// Package config loads devmesh daemon configuration from a JSON file and
// environment variables, applying compiled defaults. Precedence is
// env > file > defaults (CLI flags are merged by the command layer).
//
//glazedclilint:file-ignore DEVMESH_* environment overrides are a documented domain config source for the daemon, not CLI flags
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DockerConfig controls the Docker adapter.
type DockerConfig struct {
	Enabled                        bool `json:"enabled"`
	AllowNonLoopbackPublishedPorts bool `json:"allow_non_loopback_published_ports"`
}

// HTTPConfig controls the future HTTP/TLS proxy phase.
type HTTPConfig struct {
	Enabled    bool   `json:"enabled"`
	HTTPAddr   string `json:"http_addr"`
	HTTPSAddr  string `json:"https_addr"`
	BaseDomain string `json:"base_domain,omitempty"`
	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
}

// Config is the merged daemon configuration.
type Config struct {
	Socket          string        `json:"socket"`
	TCPFrontendHost string        `json:"tcp_frontend_host"`
	TCPFrontendMin  int           `json:"tcp_frontend_min"`
	TCPFrontendMax  int           `json:"tcp_frontend_max"`
	RuntimeIdleTTL  time.Duration `json:"runtime_idle_ttl"`
	LeaseTTL        time.Duration `json:"lease_ttl"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	StatePath       string        `json:"state_path"`
	Docker          DockerConfig  `json:"docker"`
	HTTP            HTTPConfig    `json:"http"`
}

// Default returns the compiled defaults.
func Default() Config {
	return Config{
		TCPFrontendHost: "127.0.0.1",
		TCPFrontendMin:  15000,
		TCPFrontendMax:  19999,
		RuntimeIdleTTL:  10 * time.Minute,
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

type fileConfig struct {
	Socket          string `json:"socket"`
	TCPFrontendHost string `json:"tcp_frontend_host"`
	TCPFrontendMin  *int   `json:"tcp_frontend_min"`
	TCPFrontendMax  *int   `json:"tcp_frontend_max"`
	RuntimeIdleTTL  string `json:"runtime_idle_ttl"`
	LeaseTTL        string `json:"lease_ttl"`
	ShutdownTimeout string `json:"shutdown_timeout"`
	StatePath       string `json:"state_path"`
	Docker          *struct {
		Enabled                        *bool `json:"enabled"`
		AllowNonLoopbackPublishedPorts *bool `json:"allow_non_loopback_published_ports"`
	} `json:"docker"`
	HTTP *struct {
		Enabled    *bool  `json:"enabled"`
		HTTPAddr   string `json:"http_addr"`
		HTTPSAddr  string `json:"https_addr"`
		BaseDomain string `json:"base_domain"`
		CertFile   string `json:"cert_file"`
		KeyFile    string `json:"key_file"`
	} `json:"http"`
}

// Load reads path (if non-empty), merges it over Default, then applies
// environment overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return err
	}
	if fc.Socket != "" {
		cfg.Socket = fc.Socket
	}
	if fc.TCPFrontendHost != "" {
		cfg.TCPFrontendHost = fc.TCPFrontendHost
	}
	if fc.TCPFrontendMin != nil {
		cfg.TCPFrontendMin = *fc.TCPFrontendMin
	}
	if fc.TCPFrontendMax != nil {
		cfg.TCPFrontendMax = *fc.TCPFrontendMax
	}
	if fc.StatePath != "" {
		cfg.StatePath = fc.StatePath
	}
	if d, err := parseDuration(fc.RuntimeIdleTTL); err != nil {
		return err
	} else if d != 0 {
		cfg.RuntimeIdleTTL = d
	}
	if d, err := parseDuration(fc.LeaseTTL); err != nil {
		return err
	} else if d != 0 {
		cfg.LeaseTTL = d
	}
	if d, err := parseDuration(fc.ShutdownTimeout); err != nil {
		return err
	} else if d != 0 {
		cfg.ShutdownTimeout = d
	}
	if fc.Docker != nil {
		if fc.Docker.Enabled != nil {
			cfg.Docker.Enabled = *fc.Docker.Enabled
		}
		if fc.Docker.AllowNonLoopbackPublishedPorts != nil {
			cfg.Docker.AllowNonLoopbackPublishedPorts = *fc.Docker.AllowNonLoopbackPublishedPorts
		}
	}
	if fc.HTTP != nil {
		if fc.HTTP.Enabled != nil {
			cfg.HTTP.Enabled = *fc.HTTP.Enabled
		}
		if fc.HTTP.HTTPAddr != "" {
			cfg.HTTP.HTTPAddr = fc.HTTP.HTTPAddr
		}
		if fc.HTTP.HTTPSAddr != "" {
			cfg.HTTP.HTTPSAddr = fc.HTTP.HTTPSAddr
		}
		if fc.HTTP.BaseDomain != "" {
			cfg.HTTP.BaseDomain = fc.HTTP.BaseDomain
		}
		if fc.HTTP.CertFile != "" {
			cfg.HTTP.CertFile = fc.HTTP.CertFile
		}
		if fc.HTTP.KeyFile != "" {
			cfg.HTTP.KeyFile = fc.HTTP.KeyFile
		}
	}
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("DEVMESH_SOCKET"); v != "" {
		cfg.Socket = v
	}
	if v := os.Getenv("DEVMESH_TCP_FRONTEND_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TCPFrontendMin = n
		}
	}
	if v := os.Getenv("DEVMESH_TCP_FRONTEND_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TCPFrontendMax = n
		}
	}
	if v := os.Getenv("DEVMESH_DOCKER_ENABLED"); v != "" {
		cfg.Docker.Enabled = v == "1" || v == "true" || v == "yes"
	}
	if v := os.Getenv("DEVMESH_STATE_PATH"); v != "" {
		cfg.StatePath = v
	}
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
