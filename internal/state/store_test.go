package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPort("checkout.postgres", 15432); err != nil {
		t.Fatal(err)
	}
	if s.Port("checkout.postgres") != 15432 {
		t.Fatal("port not remembered")
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Port("checkout.postgres") != 15432 {
		t.Fatal("port not persisted")
	}
}

func TestCorruptRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("corrupt state should not fail load: %v", err)
	}
	if len(s.Snapshot()) != 0 {
		t.Fatal("expected empty state after corruption")
	}
	matches, _ := filepath.Glob(path + ".corrupt.*")
	if len(matches) == 0 {
		t.Fatal("expected a corrupt backup file")
	}
}

func TestFutureVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"tcp_ports":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected future version to be refused")
	}
}
