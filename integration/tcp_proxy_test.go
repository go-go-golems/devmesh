package integration

import (
	"context"
	"fmt"
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
	"github.com/wesen/devmesh/internal/registry"
	"github.com/wesen/devmesh/internal/transport"
)

type harness struct {
	d         *daemon.Daemon
	client    *transport.Client
	socket    string
	httpAddr  string
	httpsAddr string
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
	return startHarnessOpts(t, leaseTTL, false, false, "", "")
}

func startHarnessWithDocker(t *testing.T, leaseTTL time.Duration, dockerEnabled bool) *harness {
	return startHarnessOpts(t, leaseTTL, dockerEnabled, false, "", "")
}

func startHarnessHTTP(t *testing.T, leaseTTL time.Duration) *harness {
	return startHarnessOpts(t, leaseTTL, false, true, "", "")
}

func startHarnessTLS(t *testing.T, leaseTTL time.Duration, certFile, keyFile string) *harness {
	return startHarnessOpts(t, leaseTTL, false, true, certFile, keyFile)
}

func startHarnessOpts(t *testing.T, leaseTTL time.Duration, dockerEnabled, httpEnabled bool, certFile, keyFile string) *harness {
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
	cfg.Docker.Enabled = dockerEnabled
	httpAddr := ""
	httpsAddr := ""
	if httpEnabled {
		cfg.HTTP.Enabled = true
		cfg.HTTP.HTTPAddr = net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", freeBase(t)))
		cfg.HTTP.HTTPSAddr = net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", freeBase(t)))
		httpAddr = cfg.HTTP.HTTPAddr
		httpsAddr = cfg.HTTP.HTTPSAddr
	}
	cfg.HTTP.CertFile = certFile
	cfg.HTTP.KeyFile = keyFile

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

	return &harness{d: d, client: transport.NewClient(socket, 3*time.Second), socket: socket, httpAddr: httpAddr, httpsAddr: httpsAddr}
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

func TestShutdownClosesActiveTCPConnections(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	backend := prefixEcho(t, "A")
	var reg api.RegisterResponse
	if err := h.client.Do(context.Background(), "POST", "/v1/registrations", api.RegisterRequest{Name: "shutdown.svc", Kind: "tcp", Source: "manual", Backend: backendDTO(t, backend)}, &reg); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", net.JoinHostPort(reg.Frontend.Host, strconv.Itoa(reg.Frontend.Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.d.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, writeErr := conn.Write([]byte("b"))
	if writeErr == nil {
		_, writeErr = conn.Read(buf)
	}
	if writeErr == nil {
		t.Fatal("active TCP connection survived daemon shutdown")
	}
}

func TestBackendReplacementKeepsFrontend(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	ctx := context.Background()
	backendA := prefixEcho(t, "A")
	backendB := prefixEcho(t, "B")

	register := func(backend string) api.RegisterResponse {
		var reg api.RegisterResponse
		req := api.RegisterRequest{Name: "replace.svc", Kind: "tcp", Source: "manual", Backend: backendDTO(t, backend)}
		if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &reg); err != nil {
			t.Fatalf("register %s: %v", backend, err)
		}
		return reg
	}

	first := register(backendA)
	frontend := net.JoinHostPort(first.Frontend.Host, strconv.Itoa(first.Frontend.Port))
	if got := dialAndRead(t, frontend, "x"); got != "A:x" {
		t.Fatalf("first backend: got %q", got)
	}
	if err := h.client.DoAuth(ctx, "DELETE", "/v1/registrations/"+first.RegistrationID, first.LeaseToken, nil, nil); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	second := register(backendB)
	if second.Frontend.Port != first.Frontend.Port {
		t.Fatalf("frontend changed on replace: %d -> %d", first.Frontend.Port, second.Frontend.Port)
	}
	if got := dialAndRead(t, frontend, "y"); got != "B:y" {
		t.Fatalf("after replace: got %q, want B:y", got)
	}
}

// TestSameOwnerReplacementKeepsFrontendInternal exercises the trusted path
// used by Docker. The old lease is retired, so stale deletion cannot remove
// the newer publication (P01).
func TestSameOwnerReplacementKeepsFrontendInternal(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	backendA := prefixEcho(t, "A")
	backendB := prefixEcho(t, "B")
	toBackend := func(addr string) registry.Backend {
		return registry.Backend{Host: "127.0.0.1", Port: mustPort(t, addr)}
	}
	first, err := h.d.Register(daemon.RegisterParams{Name: "internal.svc", Kind: registry.KindTCP, Source: registry.SourceManual, OwnerKey: "manual:stable", Backend: toBackend(backendA)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.d.Register(daemon.RegisterParams{Name: "internal.svc", Kind: registry.KindTCP, Source: registry.SourceManual, OwnerKey: "manual:stable", Backend: toBackend(backendB)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Frontend.Port != second.Frontend.Port {
		t.Fatalf("frontend changed: %d -> %d", first.Frontend.Port, second.Frontend.Port)
	}
	if err := h.d.DeleteRegistration(first.RegistrationID, first.LeaseToken); err == nil {
		t.Fatal("retired old registration delete succeeded")
	}
	if got := dialAndRead(t, first.Frontend.Addr(), "x"); got != "B:x" {
		t.Fatalf("stale delete disabled replacement: got %q", got)
	}
}

func mustPort(t *testing.T, addr string) int {
	t.Helper()
	_, raw, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestUnrelatedOwnerConflicts(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	ctx := context.Background()
	backend := prefixEcho(t, "A")
	req := api.RegisterRequest{Name: "owned.svc", Kind: "tcp", Source: "manual", Backend: backendDTO(t, backend)}
	if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &api.RegisterResponse{}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := h.client.Do(ctx, "POST", "/v1/registrations", req, &api.RegisterResponse{}); err == nil {
		t.Fatal("second registration succeeded, want conflict")
	}
}

func TestPublicRegisterRejectsDockerSource(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	backend := prefixEcho(t, "A")
	req := api.RegisterRequest{Name: "hijack.svc", Kind: "tcp", Source: "docker", Backend: backendDTO(t, backend)}
	if err := h.client.Do(context.Background(), "POST", "/v1/registrations", req, &api.RegisterResponse{}); err == nil {
		t.Fatal("public docker source accepted")
	}
}

func TestTTLOutOfRangeRejected(t *testing.T) {
	h := startHarness(t, 5*time.Second)
	backend := prefixEcho(t, "A")
	for _, ttl := range []int{1, 2, 3601} {
		req := api.RegisterRequest{Name: "ttl.svc", Kind: "tcp", Source: "manual", Backend: backendDTO(t, backend), TTLSeconds: ttl}
		if err := h.client.Do(context.Background(), "POST", "/v1/registrations", req, &api.RegisterResponse{}); err == nil {
			t.Fatalf("ttl %d accepted", ttl)
		}
	}
	var resp api.RegisterResponse
	req := api.RegisterRequest{Name: "ttl.svc", Kind: "tcp", Source: "manual", Backend: backendDTO(t, backend), TTLSeconds: 3}
	if err := h.client.Do(context.Background(), "POST", "/v1/registrations", req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TTLSeconds != 3 {
		t.Fatalf("ttl_seconds=%d, want 3", resp.TTLSeconds)
	}
}
