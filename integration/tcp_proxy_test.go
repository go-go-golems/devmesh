package integration

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/wesen/devmesh/internal/api"
	"github.com/wesen/devmesh/internal/config"
	"github.com/wesen/devmesh/internal/daemon"
	"github.com/wesen/devmesh/internal/transport"
)

type harness struct {
	d      *daemon.Daemon
	client *transport.Client
	socket string
}

func freeBase(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startHarness(t *testing.T, leaseTTL time.Duration) *harness {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "devmesh.sock")

	base := freeBase(t)
	cfg := config.Default()
	cfg.Socket = socket
	cfg.StatePath = filepath.Join(dir, "state.json")
	cfg.TCPFrontendMin = base + 1
	cfg.TCPFrontendMax = base + 40
	cfg.LeaseTTL = leaseTTL
	cfg.Docker.Enabled = false

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := daemon.New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)
	httpSrv := &http.Server{Handler: api.NewServer(d, logger).Handler()}
	go func() { _ = httpSrv.Serve(ln) }()

	t.Cleanup(func() {
		cancel()
		_ = httpSrv.Close()
		_ = ln.Close()
		_ = d.Shutdown(context.Background())
		transport.RemoveSocket(socket)
	})

	return &harness{d: d, client: transport.NewClient(socket, 3*time.Second), socket: socket}
}

// prefixEcho starts a TCP server that replies "<prefix>:<payload>".
func prefixEcho(t *testing.T, prefix string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				_, _ = c.Write(append([]byte(prefix+":"), buf[:n]...))
			}(c)
		}
	}()
	return ln.Addr().String()
}

func backendDTO(t *testing.T, addr string) api.BackendDTO {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return api.BackendDTO{Host: host, Port: port}
}

func dialAndRead(t *testing.T, addr, payload string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf[:n])
}

func TestTCPProxyRoundTripAndLeaseLifecycle(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	ctx := context.Background()
	backend := prefixEcho(t, "A")

	var reg api.RegisterResponse
	req := api.RegisterRequest{
		Name:    "test.echo",
		Kind:    "tcp",
		Source:  "process",
		Backend: backendDTO(t, backend),
	}
	if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.Frontend.Port == 0 || reg.LeaseToken == "" {
		t.Fatalf("bad register response: %+v", reg)
	}
	frontend := net.JoinHostPort(reg.Frontend.Host, strconv.Itoa(reg.Frontend.Port))

	if got := dialAndRead(t, frontend, "hello"); got != "A:hello" {
		t.Fatalf("proxy returned %q, want %q", got, "A:hello")
	}

	// Heartbeat with the token succeeds; with a wrong token fails.
	var hb api.HeartbeatResponse
	if err := h.client.DoAuth(ctx, "POST", "/v1/registrations/"+reg.RegistrationID+"/heartbeat", reg.LeaseToken, nil, &hb); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if hb.ExpiresAt == "" {
		t.Fatal("heartbeat returned empty expiry")
	}
	if err := h.client.DoAuth(ctx, "POST", "/v1/registrations/"+reg.RegistrationID+"/heartbeat", "wrong", nil, &hb); err == nil {
		t.Fatal("heartbeat with wrong token succeeded")
	}

	// Delete makes the service unavailable but keeps the frontend.
	if err := h.client.DoAuth(ctx, "DELETE", "/v1/registrations/"+reg.RegistrationID, reg.LeaseToken, nil, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var svc api.ServiceDTO
	if err := h.client.Do(ctx, "GET", "/v1/services/test.echo", nil, &svc); err != nil {
		t.Fatalf("resolve after delete: %v", err)
	}
	if svc.Status != "unavailable" {
		t.Fatalf("status after delete = %q, want unavailable", svc.Status)
	}
	if svc.Frontend.Port != reg.Frontend.Port {
		t.Fatalf("frontend changed after delete: %d -> %d", reg.Frontend.Port, svc.Frontend.Port)
	}
}

func TestBackendReplacementKeepsFrontend(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	ctx := context.Background()
	backendA := prefixEcho(t, "A")
	backendB := prefixEcho(t, "B")

	register := func(owner, backend string) api.RegisterResponse {
		var reg api.RegisterResponse
		req := api.RegisterRequest{
			Name:     "replace.svc",
			Kind:     "tcp",
			Source:   "manual",
			OwnerKey: owner,
			Backend:  backendDTO(t, backend),
		}
		if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &reg); err != nil {
			t.Fatalf("register %s: %v", backend, err)
		}
		return reg
	}

	first := register("manual:stable-owner", backendA)
	frontend := net.JoinHostPort(first.Frontend.Host, strconv.Itoa(first.Frontend.Port))
	if got := dialAndRead(t, frontend, "x"); got != "A:x" {
		t.Fatalf("first backend: got %q", got)
	}

	// Same owner replaces the backend; frontend must not change.
	second := register("manual:stable-owner", backendB)
	if second.Frontend.Port != first.Frontend.Port {
		t.Fatalf("frontend changed on replace: %d -> %d", first.Frontend.Port, second.Frontend.Port)
	}
	if got := dialAndRead(t, frontend, "y"); got != "B:y" {
		t.Fatalf("after replace: got %q, want B:y", got)
	}
}

func TestUnrelatedOwnerConflicts(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	ctx := context.Background()
	backend := prefixEcho(t, "A")

	req := api.RegisterRequest{Name: "owned.svc", Kind: "tcp", Source: "manual", OwnerKey: "manual:one", Backend: backendDTO(t, backend)}
	if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &api.RegisterResponse{}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	req.OwnerKey = "manual:two"
	err := h.client.Do(ctx, "POST", "/v1/registrations", req, &api.RegisterResponse{})
	if err == nil {
		t.Fatal("second owner registration succeeded, want conflict")
	}
}
