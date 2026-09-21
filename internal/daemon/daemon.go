// Package daemon orchestrates the devmesh core: it combines the registry,
// runtime manager, lease manager, and persistent state behind a small domain
// API. The HTTP layer (internal/api) is a thin adapter over this package.
package daemon

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wesen/devmesh/internal/config"
	"github.com/wesen/devmesh/internal/dockerwatch"
	"github.com/wesen/devmesh/internal/lease"
	"github.com/wesen/devmesh/internal/proxy"
	"github.com/wesen/devmesh/internal/registry"
	"github.com/wesen/devmesh/internal/runtime"
	"github.com/wesen/devmesh/internal/state"
)

// Version is the daemon/CLI version string.
const Version = "0.1.0"

// Daemon owns all in-process state.
type Daemon struct {
	cfg    config.Config
	logger *slog.Logger

	Registry *registry.Registry
	Runtime  *runtime.Manager
	Lease    *lease.Manager
	State    *state.Store
	HTTP     *proxy.Router

	mutate    sync.Mutex
	startedAt time.Time

	dockerStatus atomic.Value // string
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	tlsCert      *tls.Certificate

	// Proxy listeners are bound during New so successful daemon startup means
	// every configured public endpoint is actually owned.
	httpListener  net.Listener
	httpsListener net.Listener
}

// New builds a daemon from validated config. It loads persistent state.
func New(cfg config.Config, logger *slog.Logger) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.StatePath == "" {
		cfg.StatePath = config.DefaultStatePath()
	}
	st, err := state.Load(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	d := &Daemon{
		cfg:       cfg,
		logger:    logger,
		Registry:  registry.New(),
		State:     st,
		Lease:     lease.NewManager(cfg.LeaseTTL),
		startedAt: time.Now(),
	}
	alloc := runtime.NewAllocator(cfg.TCPFrontendHost, cfg.TCPFrontendMin, cfg.TCPFrontendMax, st, logger)
	d.Runtime = runtime.NewManager(alloc, func(name string) *registry.Backend {
		rec, ok := d.Registry.Resolve(name)
		if !ok || rec.Status != registry.StatusReady || rec.Backend == nil {
			return nil
		}
		return rec.Backend
	}, logger, 3*time.Second)
	d.dockerStatus.Store("disabled")
	if cfg.Docker.Enabled {
		d.dockerStatus.Store("degraded")
	}
	if err := d.loadTLS(); err != nil {
		return nil, err
	}
	scheme, addr := "http", cfg.HTTP.HTTPAddr
	if d.tlsCert != nil {
		scheme, addr = "https", cfg.HTTP.HTTPSAddr
	}
	port, err := listenerPort(addr)
	if err != nil {
		return nil, err
	}
	d.HTTP = proxy.NewRouter(scheme, port, logger)
	if err := d.bindProxyListeners(); err != nil {
		return nil, err
	}
	return d, nil
}

func listenerPort(addr string) (int, error) {
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("parse listener address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0, fmt.Errorf("parse listener port %q: %w", rawPort, err)
	}
	return port, nil
}

func (d *Daemon) bindProxyListeners() error {
	if d.cfg.HTTP.Enabled {
		ln, err := net.Listen("tcp", d.cfg.HTTP.HTTPAddr)
		if err != nil {
			return fmt.Errorf("bind HTTP proxy listener %s: %w", d.cfg.HTTP.HTTPAddr, err)
		}
		d.httpListener = ln
	}
	if d.tlsCert != nil {
		ln, err := net.Listen("tcp", d.cfg.HTTP.HTTPSAddr)
		if err != nil {
			if d.httpListener != nil {
				_ = d.httpListener.Close()
				d.httpListener = nil
			}
			return fmt.Errorf("bind HTTPS proxy listener %s: %w", d.cfg.HTTP.HTTPSAddr, err)
		}
		d.httpsListener = ln
	}
	return nil
}

// loadTLS validates an existing PEM certificate/key pair at startup. Both
// files must be present together; automatic ACME issuance is deliberately out
// of scope.
func (d *Daemon) loadTLS() error {
	cfg := d.cfg.HTTP
	if cfg.CertFile == "" && cfg.KeyFile == "" {
		return nil
	}
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return fmt.Errorf("both http.cert_file and http.key_file are required for TLS")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("load TLS certificate/key: %w", err)
	}
	d.tlsCert = &cert
	return nil
}

// TLSCertificate returns the loaded certificate, or nil when TLS is disabled.
func (d *Daemon) TLSCertificate() *tls.Certificate { return d.tlsCert }

// Config returns the daemon configuration.
func (d *Daemon) Config() config.Config { return d.cfg }

// Logger returns the daemon logger.
func (d *Daemon) Logger() *slog.Logger { return d.logger }

// StartedAt returns daemon start time.
func (d *Daemon) StartedAt() time.Time { return d.startedAt }

// Start launches background workers and serves the proxy listeners bound by New.
func (d *Daemon) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				d.sweepLeases(now)
			}
		}
	}()

	if d.httpListener != nil {
		d.wg.Add(1)
		go d.serveProxy(ctx, "http", d.httpListener, &http.Server{Handler: d.HTTP})
	}
	if d.httpsListener != nil {
		d.wg.Add(1)
		go d.serveProxy(ctx, "https", d.httpsListener, &http.Server{
			Handler: d.HTTP,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{*d.tlsCert},
				MinVersion:   tls.VersionTLS12,
			},
		})
	}

	d.logger.Info("daemon_started", "version", Version, "state", d.cfg.StatePath)
	d.StartDockerWatcher(ctx)
}

func (d *Daemon) serveProxy(ctx context.Context, scheme string, ln net.Listener, srv *http.Server) {
	defer d.wg.Done()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), d.cfg.ShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	var err error
	if scheme == "https" {
		err = srv.ServeTLS(ln, "", "")
	} else {
		err = srv.Serve(ln)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		d.logger.Warn(scheme+"_proxy_serve_failed", "addr", ln.Addr().String(), "error", err)
	}
}

// DockerCallbacks returns the callback wiring that connects a Docker watcher
// to this daemon. It is extracted so tests can drive a fake watcher against a
// real daemon without a Docker daemon.
func (d *Daemon) DockerCallbacks() dockerwatch.Callbacks {
	return dockerwatch.Callbacks{
		OnRegister: func(ctx context.Context, reg dockerwatch.Registration) error {
			_, rerr := d.Register(RegisterParams{
				Name:              reg.Name,
				Kind:              registry.Kind(reg.Kind),
				AppProtocol:       reg.AppProtocol,
				Backend:           registry.Backend{Host: reg.BackendHost, Port: reg.BackendPort},
				PreferredPort:     reg.PreferredPort,
				Source:            registry.SourceDocker,
				OwnerKey:          reg.OwnerKey,
				DockerContainerID: reg.ContainerID,
				HTTPHost:          reg.HTTPHost,
			})
			return rerr
		},
		OnForget: func(reg dockerwatch.Registration) {
			d.ForgetDockerPublication(reg.Name, reg.OwnerKey, reg.ContainerID)
		},
		OnStatus: d.SetDockerStatus,
	}
}

// StartDockerWatcher launches the Docker adapter when enabled. Docker being
// unavailable only degrades the Docker status; the rest of devmesh keeps
// running.
func (d *Daemon) StartDockerWatcher(ctx context.Context) {
	if !d.cfg.Docker.Enabled {
		d.SetDockerStatus("disabled")
		return
	}
	api, err := dockerwatch.NewClient()
	if err != nil {
		d.logger.Warn("docker_connect_failed", "error", err)
		d.SetDockerStatus("degraded")
		return
	}
	w := dockerwatch.NewWatcher(api, d.cfg.Docker.AllowNonLoopbackPublishedPorts, d.DockerCallbacks(), d.logger)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := w.Run(ctx); err != nil {
			d.logger.Warn("docker_watcher_stopped", "error", err)
		}
	}()
}

func (d *Daemon) sweepLeases(now time.Time) {
	for _, e := range d.Lease.TakeExpired(now) {
		if d.endPublication(e.Name, e.RegistrationID) {
			d.logger.Info("lease_expired", "service", e.Name, "registration_id", e.RegistrationID)
		} else {
			d.logger.Info("stale_lease_expired", "service", e.Name, "registration_id", e.RegistrationID)
		}
	}
}

// endPublication clears the current backend only when producerID still
// identifies the installed publication. Stale removals (an old lease or a
// container that has been replaced) are ignored, which is the core protection
// for backend replacement. The frontend stays reserved.
func (d *Daemon) endPublication(name, producerID string) bool {
	d.mutate.Lock()
	defer d.mutate.Unlock()
	return d.Registry.ClearBackendIf(name, producerID)
}

// Shutdown stops background work, closes runtimes, and flushes state.
func (d *Daemon) Shutdown(ctx context.Context) error {
	if d.cancel != nil {
		d.cancel()
	}
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	d.Runtime.CloseAll()
	if err := d.State.Save(); err != nil {
		d.logger.Error("state_save_failed", "error", err)
		return err
	}
	d.logger.Info("daemon_stopped")
	return nil
}

// RegisterParams describes a registration request from any producer.
type RegisterParams struct {
	Name              string
	Kind              registry.Kind
	AppProtocol       string
	Backend           registry.Backend
	PreferredPort     int
	TTLSeconds        int
	Source            registry.Source
	OwnerKey          string
	RegistrationID    string
	DockerContainerID string
	HTTPHost          string
}

// RegisterResult is returned to a producer after registration.
type RegisterResult struct {
	RegistrationID string
	LeaseToken     string
	Name           string
	Frontend       registry.Frontend
	ExpiresAt      *time.Time
	// TTLSeconds is the effective lease duration for process/manual sources.
	// Clients use it to schedule renewal instead of assuming a default.
	TTLSeconds int
}

// Register validates and applies a backend, allocating a TCP frontend or an
// HTTP hostname route as appropriate.
func (d *Daemon) Register(p RegisterParams) (RegisterResult, error) {
	d.mutate.Lock()
	defer d.mutate.Unlock()
	return d.registerLocked(p)
}

func (d *Daemon) registerLocked(p RegisterParams) (RegisterResult, error) {
	if err := registry.ValidateName(p.Name); err != nil {
		return RegisterResult{}, errf(CodeInvalidName, "%s", err)
	}
	kind := p.Kind
	if kind == "" {
		kind = registry.KindTCP
	}
	if kind != registry.KindTCP && kind != registry.KindHTTP {
		return RegisterResult{}, errf(CodeUnsupportedKind, "kind %q is not supported", kind)
	}
	if err := registry.ValidateBackend(p.Backend); err != nil {
		return RegisterResult{}, errf(CodeInvalidBackend, "%s", err)
	}
	source := p.Source
	if source == "" {
		source = registry.SourceProcess
	}
	// Validate the effective lease before allocating a listener or mutating the
	// registry. A rejected TTL must leave no dormant record behind.
	ttl := d.cfg.LeaseTTL
	if source != registry.SourceDocker && p.TTLSeconds > 0 {
		ttl = time.Duration(p.TTLSeconds) * time.Second
	}
	if source != registry.SourceDocker && (ttl < config.MinLeaseTTL || ttl > config.MaxLeaseTTL) {
		return RegisterResult{}, errf(CodeInvalidRequest, "ttl_seconds must be between %d and %d", int(config.MinLeaseTTL.Seconds()), int(config.MaxLeaseTTL.Seconds()))
	}
	hostname := ""
	if kind == registry.KindHTTP {
		var err error
		hostname, err = canonicalHTTPHost(p.HTTPHost)
		if err != nil {
			return RegisterResult{}, errf(CodeInvalidRequest, "%s", err)
		}
	}
	if existing, ok := d.Registry.Resolve(p.Name); ok {
		if existing.Kind != kind {
			return RegisterResult{}, errf(CodeNameConflict, "service %s already exists with kind %s; kind is immutable until daemon restart", p.Name, existing.Kind)
		}
		if kind == registry.KindHTTP && existing.Hostname != hostname {
			return RegisterResult{}, errf(CodeNameConflict, "service %s already owns hostname %s; hostname is immutable until daemon restart", p.Name, existing.Hostname)
		}
	}
	if kind == registry.KindHTTP {
		if existing, ok := d.Registry.FindByHostname(hostname); ok && existing.Name != p.Name {
			return RegisterResult{}, errf(CodeNameConflict, "hostname %s is already owned by service %s", hostname, existing.Name)
		}
	}
	regID := p.RegistrationID
	if regID == "" {
		regID = newID()
	}
	ownerKey := p.OwnerKey
	if ownerKey == "" {
		ownerKey = string(source) + ":" + regID
	}
	// The producer ID identifies the concrete publication: the registration ID
	// for process/manual sources, the container ID for Docker. It is what
	// removals must match to take effect.
	producerID := regID
	if source == registry.SourceDocker {
		if p.DockerContainerID == "" {
			return RegisterResult{}, errf(CodeInvalidRequest, "docker registrations require a container id")
		}
		producerID = p.DockerContainerID
	}
	if err := d.Registry.CheckOwnership(p.Name, ownerKey); err != nil {
		return RegisterResult{}, errf(CodeNameConflict, "%s", err)
	}
	// A same-owner replacement retires the previous publication's lease so a
	// stale registration cannot be renewed or deleted against the new backend.
	if existing, ok := d.Registry.Resolve(p.Name); ok &&
		existing.OwnerKey == ownerKey && existing.ProducerID != producerID &&
		(existing.Source == registry.SourceProcess || existing.Source == registry.SourceManual) {
		d.Lease.Remove(existing.ProducerID)
	}

	res := RegisterResult{RegistrationID: regID, Name: p.Name}

	rec := registry.ServiceRecord{
		Name:              p.Name,
		Kind:              kind,
		AppProtocol:       p.AppProtocol,
		OwnerKey:          ownerKey,
		ProducerID:        producerID,
		Source:            source,
		Backend:           &p.Backend,
		Status:            registry.StatusReady,
		DockerContainerID: p.DockerContainerID,
		UpdatedAt:         time.Now(),
	}

	if kind == registry.KindHTTP {
		rec.Hostname = hostname
		rec.Frontend = registry.Frontend{URL: d.HTTP.FrontendURL(hostname)}
	} else {
		rt, err := d.Runtime.EnsureTCPRuntime(p.Name, p.PreferredPort)
		if err != nil {
			var exhausted *runtime.ErrPortExhausted
			if errors.As(err, &exhausted) {
				return RegisterResult{}, errf(CodePortExhausted, "%s", err)
			}
			return RegisterResult{}, errf(CodeInternal, "allocate frontend: %s", err)
		}
		rec.Frontend = rt.Frontend
	}

	if _, err := d.Registry.CreateOrReplaceOwned(rec); err != nil {
		return RegisterResult{}, errf(CodeNameConflict, "%s", err)
	}

	if kind == registry.KindHTTP {
		name := p.Name
		d.HTTP.Set(rec.Hostname, func() *registry.Backend {
			r, ok := d.Registry.Resolve(name)
			if !ok || r.Status != registry.StatusReady {
				return nil
			}
			return r.Backend
		})
	}
	res.Frontend = rec.Frontend

	// Docker registrations are driven by container lifecycle, not leases.
	if source == registry.SourceDocker {
		d.logger.Info("service_registered", "service", p.Name, "source", source, "backend", p.Backend.Addr(), "frontend", frontendLabel(rec.Frontend))
		return res, nil
	}

	token, err := lease.NewToken()
	if err != nil {
		return RegisterResult{}, errf(CodeInternal, "generate lease token: %s", err)
	}
	entry := d.Lease.AddWithTTL(regID, p.Name, ownerKey, token, ttl)
	res.LeaseToken = token
	res.ExpiresAt = &entry.ExpiresAt
	res.TTLSeconds = int(ttl.Seconds())

	d.logger.Info("service_registered", "service", p.Name, "source", source, "backend", p.Backend.Addr(), "frontend", frontendLabel(rec.Frontend))
	return res, nil
}

func canonicalHTTPHost(raw string) (string, error) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if host == "" {
		return "", fmt.Errorf("http_host is required for kind=http")
	}
	if strings.ContainsAny(host, ":/@") {
		return "", fmt.Errorf("http_host %q must be a hostname without scheme, path, or port", raw)
	}
	return host, nil
}

func frontendLabel(f registry.Frontend) string {
	if f.URL != "" {
		return f.URL
	}
	return f.Addr()
}

// Heartbeat renews a leased registration, returning the new expiry.
func (d *Daemon) Heartbeat(id, token string) (time.Time, error) {
	exp, err := d.Lease.Renew(id, token)
	if err != nil {
		if errors.Is(err, lease.ErrNotFound) {
			return time.Time{}, errf(CodeRegistrationNotFound, "registration %s is unknown", id)
		}
		return time.Time{}, errf(CodeUnauthorized, "invalid lease token")
	}
	return exp, nil
}

// DeleteRegistration removes a leased registration. The service backend is
// cleared only when this registration still identifies the current
// publication; deleting a replaced registration leaves the replacement ready.
func (d *Daemon) DeleteRegistration(id, token string) error {
	e, ok := d.Lease.Get(id)
	if !ok {
		return errf(CodeRegistrationNotFound, "registration %s is unknown", id)
	}
	if err := d.Lease.Delete(id, token); err != nil {
		if errors.Is(err, lease.ErrUnauthorized) {
			return errf(CodeUnauthorized, "invalid lease token")
		}
		return errf(CodeRegistrationNotFound, "registration %s is unknown", id)
	}
	if d.endPublication(e.Name, e.RegistrationID) {
		d.logger.Info("service_backend_removed", "service", e.Name, "registration_id", id)
	} else {
		d.logger.Info("stale_registration_deleted", "service", e.Name, "registration_id", id)
	}
	return nil
}

// ForgetDockerPublication clears a Docker-registered service when the named
// container still owns the current publication. A stop event for a container
// that has been recreated is a no-op.
func (d *Daemon) ForgetDockerPublication(name, ownerKey, containerID string) {
	if d.endPublication(name, containerID) {
		d.logger.Info("service_backend_removed", "service", name, "owner_key", ownerKey, "source", registry.SourceDocker, "container_id", containerID)
	} else {
		d.logger.Info("stale_docker_forget_ignored", "service", name, "owner_key", ownerKey, "container_id", containerID)
	}
}

// ServiceInfo is the API-facing projection of a service.
type ServiceInfo struct {
	Name              string
	Kind              registry.Kind
	AppProtocol       string
	Status            registry.Status
	Frontend          registry.Frontend
	Backend           *registry.Backend
	Source            registry.Source
	OwnerKey          string
	ProducerID        string
	DockerContainerID string
	Hostname          string
}

func infoFromRecord(rec registry.ServiceRecord, includeBackend bool) ServiceInfo {
	info := ServiceInfo{
		Name:              rec.Name,
		Kind:              rec.Kind,
		AppProtocol:       rec.AppProtocol,
		Status:            rec.Status,
		Frontend:          rec.Frontend,
		Source:            rec.Source,
		OwnerKey:          rec.OwnerKey,
		ProducerID:        rec.ProducerID,
		DockerContainerID: rec.DockerContainerID,
		Hostname:          rec.Hostname,
	}
	if includeBackend && rec.Backend != nil {
		b := *rec.Backend
		info.Backend = &b
	}
	return info
}

// List returns all services without backend internals.
func (d *Daemon) List() []ServiceInfo {
	recs := d.Registry.List()
	out := make([]ServiceInfo, 0, len(recs))
	for _, rec := range recs {
		out = append(out, infoFromRecord(rec, false))
	}
	return out
}

// Resolve returns one service without backend internals.
func (d *Daemon) Resolve(name string) (ServiceInfo, error) {
	rec, ok := d.Registry.Resolve(name)
	if !ok {
		return ServiceInfo{}, errf(CodeServiceNotFound, "service %s is unknown", name)
	}
	return infoFromRecord(rec, false), nil
}

// Inspect returns one service including backend and owner details.
func (d *Daemon) Inspect(name string) (ServiceInfo, error) {
	rec, ok := d.Registry.Resolve(name)
	if !ok {
		return ServiceInfo{}, errf(CodeServiceNotFound, "service %s is unknown", name)
	}
	return infoFromRecord(rec, true), nil
}

// SetDockerStatus records Docker integration health.
func (d *Daemon) SetDockerStatus(status string) { d.dockerStatus.Store(status) }

// DockerStatus returns Docker integration health.
func (d *Daemon) DockerStatus() string {
	v, _ := d.dockerStatus.Load().(string)
	return v
}

// CheckFrontendRange reports whether at least one port in the configured range
// is bindable. It binds and immediately releases a probe listener.
func (d *Daemon) CheckFrontendRange() error {
	for port := d.cfg.TCPFrontendMin; port <= d.cfg.TCPFrontendMax; port++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(d.cfg.TCPFrontendHost, strconv.Itoa(port)))
		if err == nil {
			_ = ln.Close()
			return nil
		}
	}
	return errf(CodePortExhausted, "no bindable frontend port in range %d-%d", d.cfg.TCPFrontendMin, d.cfg.TCPFrontendMax)
}
