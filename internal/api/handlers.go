package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wesen/devmesh/internal/daemon"
	"github.com/wesen/devmesh/internal/registry"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Version: daemon.Version,
		Docker:  s.d.DockerStatus(),
	})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	infos := s.d.List()
	out := ListResponse{Services: make([]ServiceDTO, 0, len(infos))}
	for _, info := range infos {
		out.Services = append(out.Services, serviceDTO(info))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, err := s.d.Resolve(name)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceDTO(info))
}

func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, err := s.d.Inspect(name)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, inspectDTO(info))
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, s.logger, &daemon.Error{Code: daemon.CodeInvalidRequest, Message: err.Error()})
		return
	}
	source := registry.Source(req.Source)
	if source == "" {
		source = registry.SourceProcess
	}
	if source != registry.SourceProcess && source != registry.SourceManual && source != registry.SourceDocker {
		writeError(w, s.logger, &daemon.Error{Code: daemon.CodeInvalidRequest, Message: fmt.Sprintf("unknown source %q", req.Source)})
		return
	}
	kind := registry.Kind(req.Kind)
	res, err := s.d.Register(daemon.RegisterParams{
		Name:              req.Name,
		Kind:              kind,
		AppProtocol:       req.AppProtocol,
		Backend:           registry.Backend{Host: req.Backend.Host, Port: req.Backend.Port},
		PreferredPort:     req.PreferredPort,
		TTLSeconds:        req.TTLSeconds,
		Source:            source,
		OwnerKey:          req.OwnerKey,
		RegistrationID:    req.RegistrationID,
		DockerContainerID: req.DockerContainerID,
	})
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	resp := RegisterResponse{
		RegistrationID: res.RegistrationID,
		LeaseToken:     res.LeaseToken,
		Name:           res.Name,
		Frontend:       frontendDTO(res.Frontend),
	}
	if res.ExpiresAt != nil {
		resp.ExpiresAt = res.ExpiresAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := bearerToken(r)
	exp, err := s.d.Heartbeat(id, token)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, HeartbeatResponse{ExpiresAt: exp.UTC().Format(time.RFC3339)})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := bearerToken(r)
	if err := s.d.DeleteRegistration(id, token); err != nil {
		writeError(w, s.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func decodeJSON(r *http.Request, dst any) error {
	defer func() { _, _ = io.Copy(io.Discard, r.Body) }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
