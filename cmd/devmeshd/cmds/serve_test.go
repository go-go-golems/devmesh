package cmds

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestResolveConfigPathFromEnv(t *testing.T) {
	t.Setenv("DEVMESH_CONFIG", "/tmp/from-env.json")
	desc := NewServeCommand().Description()
	cmd := &cobra.Command{Use: "serve"}
	cmd.Flags().String("config", "", "")

	got, err := resolveConfigPath(desc, cmd)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != "/tmp/from-env.json" {
		t.Fatalf("got %q, want /tmp/from-env.json", got)
	}
}

func TestResolveConfigPathFlagWinsOverEnv(t *testing.T) {
	t.Setenv("DEVMESH_CONFIG", "/tmp/from-env.json")
	desc := NewServeCommand().Description()
	cmd := &cobra.Command{Use: "serve"}
	cmd.Flags().String("config", "", "")
	if err := cmd.Flags().Set("config", "/tmp/from-flag.json"); err != nil {
		t.Fatal(err)
	}

	got, err := resolveConfigPath(desc, cmd)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != "/tmp/from-flag.json" {
		t.Fatalf("got %q, want /tmp/from-flag.json", got)
	}
}

func TestResolveConfigPathAbsent(t *testing.T) {
	t.Setenv("DEVMESH_CONFIG", "")
	desc := NewServeCommand().Description()
	cmd := &cobra.Command{Use: "serve"}
	cmd.Flags().String("config", "", "")

	got, err := resolveConfigPath(desc, cmd)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

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
