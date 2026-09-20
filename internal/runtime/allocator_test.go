package runtime

import (
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/wesen/devmesh/internal/state"
)

func testAllocator(t *testing.T, min, max int) *Allocator {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return NewAllocator("127.0.0.1", min, max, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	defer alloc.Listener.Close()
	if alloc.Port != port {
		t.Fatalf("got port %d, want %d", alloc.Port, port)
	}
}

func TestAllocateFallbackWhenPreferredOccupied(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	used := occupied.Addr().(*net.TCPAddr).Port

	a := testAllocator(t, used, used+2)
	alloc, err := a.Allocate("svc.b", used)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	defer alloc.Listener.Close()
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
	defer alloc.Listener.Close()
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
	defer l1.Close()
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

func TestAllocationsAreDistinct(t *testing.T) {
	a := testAllocator(t, 30000, 30050)
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		alloc, err := a.Allocate(name, 0)
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		defer alloc.Listener.Close()
		if seen[alloc.Port] {
			t.Fatalf("duplicate allocation port %d", alloc.Port)
		}
		seen[alloc.Port] = true
	}
}
