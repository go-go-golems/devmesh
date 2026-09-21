package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-go-golems/devmesh/internal/api"
	devmesh "github.com/go-go-golems/devmesh/pkg/devmesh"
)

// TestGoClientRegistersAndHeartbeats verifies the public Go client binds a
// backend, keeps its lease alive, and cleans up on Close.
func TestGoClientRegistersAndHeartbeats(t *testing.T) {
	h := startHarness(t, 3*time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				n, _ := c.Read(buf)
				_, _ = c.Write(append([]byte("C:"), buf[:n]...))
			}(c)
		}
	}()

	handle, err := devmesh.Register(context.Background(), devmesh.RegistrationOptions{
		Name:       "client.svc",
		Kind:       devmesh.KindTCP,
		Backend:    ln.Addr().String(),
		Socket:     h.socket,
		TTLSeconds: 3,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got := dialAndRead(t, handle.Endpoint(), "hi"); got != "C:hi" {
		t.Fatalf("proxy via client registration: got %q", got)
	}

	// Wait longer than the TTL so that only heartbeats keep it alive.
	time.Sleep(4500 * time.Millisecond)
	var svc api.ServiceDTO
	if err := h.client.Do(context.Background(), "GET", "/v1/services/client.svc", nil, &svc); err != nil {
		t.Fatalf("resolve after TTL: %v", err)
	}
	if svc.Status != "ready" {
		t.Fatalf("status after TTL = %q, want ready (heartbeat failed)", svc.Status)
	}

	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := h.client.Do(context.Background(), "GET", "/v1/services/client.svc", nil, &svc); err != nil {
		t.Fatalf("resolve after close: %v", err)
	}
	if svc.Status != "unavailable" {
		t.Fatalf("status after close = %q, want unavailable", svc.Status)
	}
}
