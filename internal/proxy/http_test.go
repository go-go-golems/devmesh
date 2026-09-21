package proxy

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-go-golems/devmesh/internal/registry"
)

func testHTTPLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestRouterSelectsBackendOnce pins P10. The old Director called the provider
// once for availability and again for target selection; a disappearing backend
// could leave a caller-supplied absolute URL intact. The router now snapshots
// one backend and SetURL always overwrites the outbound authority.
func TestRouterSelectsBackendOnce(t *testing.T) {
	safe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("SAFE"))
	}))
	defer safe.Close()
	unintended := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("UNINTENDED"))
	}))
	defer unintended.Close()

	backend := backendFromURL(t, safe.URL)
	var calls atomic.Int32
	r := NewRouter("http", 80, testHTTPLogger())
	r.Set("route.test", func() *registry.Backend {
		if calls.Add(1) == 1 {
			return &backend
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, unintended.URL+"/", nil)
	req.Host = "route.test"
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "SAFE" {
		t.Fatalf("status/body = %d/%q, want 200/SAFE", rr.Code, rr.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider called %d times, want 1", got)
	}
}

func TestFrontendURLIncludesNonDefaultPort(t *testing.T) {
	r := NewRouter("https", 8443, testHTTPLogger())
	if got, want := r.FrontendURL("Api.Test."), "https://api.test:8443"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if got, want := NewRouter("http", 80, testHTTPLogger()).FrontendURL("api.test"), "http://api.test"; got != want {
		t.Fatalf("default HTTP URL = %q, want %q", got, want)
	}
}

func backendFromURL(t *testing.T, raw string) registry.Backend {
	t.Helper()
	trimmed := strings.TrimPrefix(raw, "http://")
	host, rawPort, err := net.SplitHostPort(trimmed)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Backend{Host: host, Port: port}
}
