package proxy

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/go-go-golems/devmesh/internal/registry"
)

// BackendProvider returns the current backend for a route, or nil when the
// service is unavailable.
type BackendProvider func() *registry.Backend

// Router demultiplexes HTTP requests by Host header onto one shared listener.
// It is safe for concurrent use. The registry remains authoritative for each
// route's backend; the router stores only hostname-to-provider wiring.
type Router struct {
	mu     sync.RWMutex
	routes map[string]*httpProxy
	logger *slog.Logger
	scheme string
	port   int
}

type httpProxy struct {
	hostname string
	provider BackendProvider
	proxy    *httputil.ReverseProxy
}

type routeContextKey struct{}

type routeSnapshot struct {
	backend    registry.Backend
	publicHost string
}

// NewRouter builds an HTTP router. scheme and port describe the public
// frontend, not the loopback backend. Non-default ports are included in URLs
// returned by FrontendURL.
func NewRouter(scheme string, port int, logger *slog.Logger) *Router {
	if scheme == "" {
		scheme = "http"
	}
	return &Router{routes: map[string]*httpProxy{}, logger: logger, scheme: scheme, port: port}
}

// Set installs or replaces the route for hostname. The backend provider is
// evaluated exactly once per request in ServeHTTP; Rewrite reads the resulting
// immutable request snapshot rather than calling it again.
func (r *Router) Set(hostname string, provider BackendProvider) {
	key := normalizeHost(hostname)
	hp := &httpProxy{hostname: key, provider: provider}
	hp.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			snapshot, ok := pr.In.Context().Value(routeContextKey{}).(routeSnapshot)
			if !ok {
				// ServeHTTP always installs this snapshot. If a future caller
				// bypasses it, force a guaranteed failing loopback target rather
				// than leaving a caller-supplied absolute URL in place.
				pr.SetURL(&url.URL{Scheme: "http", Host: "127.0.0.1:0"})
				return
			}
			pr.SetURL(&url.URL{Scheme: "http", Host: snapshot.backend.Addr()})
			// Preserve the canonical public host for applications that route on
			// Host, while SetURL selects only the verified loopback backend.
			pr.Out.Host = snapshot.publicHost
			// Inbound forwarded headers are untrusted. Set clean values from
			// the current request instead of appending caller-controlled ones.
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			r.logger.Warn("http_proxy_error", "host", hp.hostname, "error", err)
			http.Error(w, "502 Bad Gateway", http.StatusBadGateway)
		},
	}
	r.mu.Lock()
	r.routes[key] = hp
	r.mu.Unlock()
}

// Delete removes a route.
func (r *Router) Delete(hostname string) {
	r.mu.Lock()
	delete(r.routes, normalizeHost(hostname))
	r.mu.Unlock()
}

// Lookup returns the provider for hostname.
func (r *Router) Lookup(hostname string) (BackendProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hp, ok := r.routes[normalizeHost(hostname)]
	if !ok {
		return nil, false
	}
	return hp.provider, true
}

// FrontendURL returns the stable public URL for a hostname. A configured
// non-default listener port is part of the consumer contract.
func (r *Router) FrontendURL(hostname string) string {
	host := normalizeHost(hostname)
	if r.port > 0 && (r.scheme != "http" || r.port != 80) && (r.scheme != "https" || r.port != 443) {
		host = net.JoinHostPort(host, strconv.Itoa(r.port))
	}
	return (&url.URL{Scheme: r.scheme, Host: host}).String()
}

// ServeHTTP routes by Host header. It resolves the current backend once and
// carries that snapshot through Rewrite, preventing a backend change between
// availability check and outbound target selection.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	r.mu.RLock()
	hp, ok := r.routes[host]
	r.mu.RUnlock()
	if !ok {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	backend := hp.provider()
	if backend == nil {
		http.Error(w, "503 Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	snapshot := routeSnapshot{backend: *backend, publicHost: host}
	req = req.WithContext(context.WithValue(req.Context(), routeContextKey{}, snapshot))
	hp.proxy.ServeHTTP(w, req)
}

func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return h
	}
	if hostPart, _, err := net.SplitHostPort(h); err == nil {
		h = hostPart
	}
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return strings.TrimSuffix(h, ".")
}
