package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/wesen/devmesh/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.StatePath = filepath.Join(t.TempDir(), "state.json")
	cfg.Docker.Enabled = false
	return cfg
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestTLSRequiresBothFiles(t *testing.T) {
	cfg := testConfig(t)
	cfg.HTTP.CertFile = "/nonexistent/cert.pem"
	if _, err := New(cfg, discardLogger()); err == nil {
		t.Fatal("expected error when cert_file is set without key_file")
	}

	cfg = testConfig(t)
	cfg.HTTP.KeyFile = "/nonexistent/key.pem"
	if _, err := New(cfg, discardLogger()); err == nil {
		t.Fatal("expected error when key_file is set without cert_file")
	}
}

func TestTLSInvalidPairRejected(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t)
	cfg.HTTP.CertFile = certFile
	cfg.HTTP.KeyFile = keyFile
	if _, err := New(cfg, discardLogger()); err == nil {
		t.Fatal("expected error for invalid certificate/key pair")
	}
}
