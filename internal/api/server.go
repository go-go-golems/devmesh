// Package api implements the devmesh administrative HTTP API over a Unix
// socket. Handlers are thin adapters over internal/daemon.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-go-golems/devmesh/internal/daemon"
)

// Server owns the API routes.
type Server struct {
	d      *daemon.Daemon
	logger *slog.Logger
	mux    *http.ServeMux
}

// NewServer builds the API server.
func NewServer(d *daemon.Daemon, logger *slog.Logger) *Server {
	s := &Server{d: d, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the underlying HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/services", s.handleList)
	s.mux.HandleFunc("GET /v1/services/{name}", s.handleResolve)
	s.mux.HandleFunc("GET /v1/services/{name}/inspect", s.handleInspect)
	s.mux.HandleFunc("POST /v1/registrations", s.handleRegister)
	s.mux.HandleFunc("POST /v1/registrations/{id}/heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("DELETE /v1/registrations/{id}", s.handleDelete)
}
