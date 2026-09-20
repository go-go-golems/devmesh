package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wesen/devmesh/internal/api"
)

func httpGet(t *testing.T, addr, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET host=%s: %v", host, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func registerHTTP(t *testing.T, h *harness, name, host, backendAddr, owner string) {
	t.Helper()
	bd := backendDTO(t, backendAddr)
	req := api.RegisterRequest{
		Name:     name,
		Kind:     "http",
		Source:   "manual",
		OwnerKey: owner,
		Backend:  bd,
		HTTPHost: host,
	}
	if err := h.client.Do(context.Background(), "POST", "/v1/registrations", req, &api.RegisterResponse{}); err != nil {
		t.Fatalf("register http %s: %v", name, err)
	}
}

func TestHTTPProxyRoutesByHost(t *testing.T) {
	h := startHarnessHTTP(t, 5*time.Second)

	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "A")
	}))
	defer backendA.Close()
	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "B")
	}))
	defer backendB.Close()

	registerHTTP(t, h, "checkout.api", "api-checkout.test", strings.TrimPrefix(backendA.URL, "http://"), "manual:api")
	registerHTTP(t, h, "checkout.web", "web-checkout.test", strings.TrimPrefix(backendB.URL, "http://"), "manual:web")

	// The HTTP proxy listener starts asynchronously; retry until it accepts.
	deadline := time.Now().Add(5 * time.Second)
	var code int
	var body string
	for time.Now().Before(deadline) {
		code, body = httpGet(t, h.httpAddr, "api-checkout.test")
		if code == 200 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code != 200 || body != "A" {
		t.Fatalf("api host: code=%d body=%q, want 200/A", code, body)
	}

	if code, body := httpGet(t, h.httpAddr, "web-checkout.test"); code != 200 || body != "B" {
		t.Fatalf("web host: code=%d body=%q, want 200/B", code, body)
	}

	if code, _ := httpGet(t, h.httpAddr, "unknown.test"); code != http.StatusNotFound {
		t.Fatalf("unknown host: code=%d, want 404", code)
	}

	// Mark the API service unavailable and expect 503, not a stale backend.
	infos := h.d.List()
	var ownerKey string
	for _, info := range infos {
		if info.Name == "checkout.api" {
			ownerKey = info.OwnerKey
		}
	}
	h.d.ForgetByOwner(ownerKey, "checkout.api")
	if code, _ := httpGet(t, h.httpAddr, "api-checkout.test"); code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable host: code=%d, want 503", code)
	}
}
