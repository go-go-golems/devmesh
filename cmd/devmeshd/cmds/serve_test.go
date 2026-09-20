package cmds

import (
	"testing"
	"time"
)

func TestConfigFromSettings(t *testing.T) {
	s := &ServeSettings{
		Socket:                               "/tmp/x.sock",
		TCPFrontendHost:                      "127.0.0.1",
		TCPFrontendMin:                       16000,
		TCPFrontendMax:                       16099,
		RuntimeIdleTTL:                       "2m",
		LeaseTTL:                             "9s",
		ShutdownTimeout:                      "3s",
		StatePath:                            "/tmp/state.json",
		DockerEnabled:                        true,
		DockerAllowNonLoopbackPublishedPorts: true,
		HTTPEnabled:                          true,
		HTTPAddr:                             "127.0.0.1:8088",
		HTTPSAddr:                            "127.0.0.1:8443",
		HTTPBaseDomain:                       "dev.example.com",
		HTTPCertFile:                         "/certs/cert.pem",
		HTTPKeyFile:                          "/certs/key.pem",
	}
	cfg := configFromSettings(s)
	if cfg.Socket != "/tmp/x.sock" || cfg.StatePath != "/tmp/state.json" {
		t.Fatalf("paths not mapped: %+v", cfg)
	}
	if cfg.TCPFrontendMin != 16000 || cfg.TCPFrontendMax != 16099 {
		t.Fatalf("range not mapped: %+v", cfg)
	}
	if cfg.RuntimeIdleTTL != 2*time.Minute || cfg.LeaseTTL != 9*time.Second || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("durations not mapped: %+v", cfg)
	}
	if !cfg.Docker.Enabled || !cfg.Docker.AllowNonLoopbackPublishedPorts {
		t.Fatalf("docker not mapped: %+v", cfg.Docker)
	}
	if !cfg.HTTP.Enabled || cfg.HTTP.HTTPAddr != "127.0.0.1:8088" || cfg.HTTP.CertFile != "/certs/cert.pem" {
		t.Fatalf("http not mapped: %+v", cfg.HTTP)
	}
}

func TestConfigFromSettingsDefaultsSocketAndState(t *testing.T) {
	cfg := configFromSettings(&ServeSettings{TCPFrontendHost: "127.0.0.1", TCPFrontendMin: 1, TCPFrontendMax: 2})
	if cfg.Socket == "" {
		t.Fatal("socket default not applied")
	}
	if cfg.StatePath == "" {
		t.Fatal("state path default not applied")
	}
}

func TestParseDurationFallback(t *testing.T) {
	if got := parseDuration("", 5*time.Second); got != 5*time.Second {
		t.Fatalf("empty = %v", got)
	}
	if got := parseDuration("bogus", 5*time.Second); got != 5*time.Second {
		t.Fatalf("bogus = %v", got)
	}
	if got := parseDuration("2s", time.Second); got != 2*time.Second {
		t.Fatalf("2s = %v", got)
	}
}
