package runtime

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-go-golems/devmesh/internal/state"
)

func testAllocator(t *testing.T, minPort, maxPort int) *Allocator {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return NewAllocator("127.0.0.1", minPort, maxPort, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAllocatePreferredFree(t *testing.T) {
	// Find a free port by binding :0, then release it and ask the allocator for it.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	a := testAllocator(t, port, port)
	alloc, err := a.Allocate("svc.a", port)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	defer func() { _ = alloc.Listener.Close() }()
	if alloc.Port != port {
		t.Fatalf("got port %d, want %d", alloc.Port, port)
	}
}

func TestAllocateFallbackWhenPreferredOccupied(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()
	used := occupied.Addr().(*net.TCPAddr).Port

	a := testAllocator(t, used, used+2)
	alloc, err := a.Allocate("svc.b", used)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	defer func() { _ = alloc.Listener.Close() }()
	if alloc.Port == used {
		t.Fatalf("allocator returned occupied preferred port %d", used)
	}
}

func TestAllocateReusesRemembered(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	st, err := state.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetPort("svc.c", port); err != nil {
		t.Fatal(err)
	}
	a := NewAllocator("127.0.0.1", port, port, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	alloc, err := a.Allocate("svc.c", 0)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	defer func() { _ = alloc.Listener.Close() }()
	if alloc.Port != port {
		t.Fatalf("got %d, want remembered %d", alloc.Port, port)
	}
}

func TestAllocateExhausted(t *testing.T) {
	// A single-port range that is already occupied must be exhausted.
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l1.Close() }()
	p1 := l1.Addr().(*net.TCPAddr).Port

	a := testAllocator(t, p1, p1)
	_, err = a.Allocate("svc.d", 0)
	if err == nil {
		t.Fatal("allocate succeeded on exhausted range")
	}
	if _, ok := err.(*ErrPortExhausted); !ok {
		t.Fatalf("got %T, want *ErrPortExhausted", err)
	}
}

// TestAllocateFailsWhenStateCannotPersist ensures a bound listener is closed
// and allocation fails rather than advertising a non-durable stable frontend.
func TestAllocateFailsWhenStateCannotPersist(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	dir := t.TempDir()
	statePath := filepath.Join(dir, "parent", "state.json")
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parent"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewAllocator("127.0.0.1", port, port, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := a.Allocate("durability.svc", 0); err == nil {
		t.Fatal("allocation succeeded despite state persistence failure")
	}
	// The allocator closed its temporary listener, so the port is available.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err != nil {
		t.Fatalf("failed allocation leaked listener on %d: %v", port, err)
	}
	_ = ln.Close()
	// The store is dirty, so retrying the same value does not falsely return
	// nil without writing it.
	if err := st.SetPort("durability.svc", port); err == nil {
		t.Fatal("dirty state retry falsely succeeded")
	}
}

func TestAllocationsAreDistinct(t *testing.T) {
	a := testAllocator(t, 30000, 30050)
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		alloc, err := a.Allocate(name, 0)
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		defer func() { _ = alloc.Listener.Close() }()
		if seen[alloc.Port] {
			t.Fatalf("duplicate allocation port %d", alloc.Port)
		}
		seen[alloc.Port] = true
	}
}
