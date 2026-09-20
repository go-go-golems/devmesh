package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"

	"github.com/wesen/devmesh/internal/registry"
)

// BackendProvider returns the current backend for a route, or nil when the
// service is unavailable.
type BackendProvider func() *registry.Backend

// Router demultiplexes HTTP requests by Host header onto one shared listener.
// It is safe for concurrent use.
type Router struct {
	mu     sync.RWMutex
	routes map[string]*httpProxy
	logger *slog.Logger
	scheme string
}

type httpProxy struct {
	hostname string
	provider BackendProvider
	proxy    *httputil.ReverseProxy
}

// NewRouter builds an HTTP router. scheme is "http" or "https" and is used only
// for the frontend URL.
func NewRouter(scheme string, logger *slog.Logger) *Router {
	if scheme == "" {
		scheme = "http"
	}
	return &Router{routes: map[string]*httpProxy{}, logger: logger, scheme: scheme}
}

// Set installs or replaces the route for hostname.
func (r *Router) Set(hostname string, provider BackendProvider) {
	key := normalizeHost(hostname)
	hp := &httpProxy{hostname: key, provider: provider}
	hp.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			if b := provider(); b != nil {
				req.URL.Scheme = "http"
				req.URL.Host = b.Addr()
			}
			req.Host = hp.hostname
			if req.Header.Get("X-Forwarded-Host") == "" {
				req.Header.Set("X-Forwarded-Host", hp.hostname)
			}
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

// FrontendURL returns the stable URL for a hostname.
func (r *Router) FrontendURL(hostname string) string {
	return r.scheme + "://" + normalizeHost(hostname)
}

// ServeHTTP routes by Host header.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	r.mu.RLock()
	hp, ok := r.routes[host]
	r.mu.RUnlock()
	if !ok {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	if hp.provider() == nil {
		http.Error(w, "503 Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	hp.proxy.ServeHTTP(w, req)
}

func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return h
	}
	if strings.Contains(h, ":") {
		if hostPart, _, err := net.SplitHostPort(h); err == nil {
			return hostPart
		}
	}
	return strings.TrimSuffix(h, ".")
}
