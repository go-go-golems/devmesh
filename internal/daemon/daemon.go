// Package daemon orchestrates the devmesh core: it combines the registry,
// runtime manager, lease manager, and persistent state behind a small domain
// API. The HTTP layer (internal/api) is a thin adapter over this package.
package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wesen/devmesh/internal/config"
	"github.com/wesen/devmesh/internal/dockerwatch"
	"github.com/wesen/devmesh/internal/lease"
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

	mutate    sync.Mutex
	startedAt time.Time

	dockerStatus atomic.Value // string
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// New builds a daemon from config. It loads persistent state.
func New(cfg config.Config, logger *slog.Logger) (*Daemon, error) {
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
	d.Runtime = runtime.NewManager(alloc, logger, 3*time.Second)
	d.dockerStatus.Store("disabled")
	if cfg.Docker.Enabled {
		d.dockerStatus.Store("degraded")
	}
	return d, nil
}

// Config returns the daemon configuration.
func (d *Daemon) Config() config.Config { return d.cfg }

// Logger returns the daemon logger.
func (d *Daemon) Logger() *slog.Logger { return d.logger }

// StartedAt returns daemon start time.
func (d *Daemon) StartedAt() time.Time { return d.startedAt }

// Start launches background sweeper and reaper goroutines.
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

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.Runtime.Reap(d.cfg.RuntimeIdleTTL)
			}
		}
	}()

	d.logger.Info("daemon_started", "version", Version, "state", d.cfg.StatePath)

	d.StartDockerWatcher(ctx)
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
	cb := dockerwatch.Callbacks{
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
			})
			return rerr
		},
		OnForget: func(ownerKey, name string) { d.ForgetByOwner(ownerKey, name) },
		OnStatus: d.SetDockerStatus,
	}
	w := dockerwatch.NewWatcher(api, d.cfg.Docker.AllowNonLoopbackPublishedPorts, cb, d.logger)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := w.Run(ctx); err != nil {
			d.logger.Warn("docker_watcher_stopped", "error", err)
		}
	}()
}

func (d *Daemon) sweepLeases(now time.Time) {
	for _, e := range d.Lease.Expired(now) {
		d.Lease.Remove(e.RegistrationID)
		d.mutate.Lock()
		d.Registry.MarkUnavailable(e.OwnerKey)
		d.Runtime.ClearBackend(e.Name)
		d.mutate.Unlock()
		d.logger.Info("lease_expired", "service", e.Name, "registration_id", e.RegistrationID)
	}
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
}

// RegisterResult is returned to a producer after registration.
type RegisterResult struct {
	RegistrationID string
	LeaseToken     string
	Name           string
	Frontend       registry.Frontend
	ExpiresAt      *time.Time
}

// Register validates and applies a backend, allocating the frontend if needed.
func (d *Daemon) Register(p RegisterParams) (RegisterResult, error) {
	d.mutate.Lock()
	defer d.mutate.Unlock()

	if err := registry.ValidateName(p.Name); err != nil {
		return RegisterResult{}, errf(CodeInvalidName, "%s", err)
	}
	kind := p.Kind
	if kind == "" {
		kind = registry.KindTCP
	}
	if kind != registry.KindTCP {
		return RegisterResult{}, errf(CodeUnsupportedKind, "kind %q is not implemented in the TCP MVP", kind)
	}
	if err := registry.ValidateBackend(p.Backend); err != nil {
		return RegisterResult{}, errf(CodeInvalidBackend, "%s", err)
	}
	source := p.Source
	if source == "" {
		source = registry.SourceProcess
	}
	regID := p.RegistrationID
	if regID == "" {
		regID = newID()
	}
	ownerKey := p.OwnerKey
	if ownerKey == "" {
		ownerKey = string(source) + ":" + regID
	}
	if err := d.Registry.CheckOwnership(p.Name, ownerKey); err != nil {
		return RegisterResult{}, errf(CodeNameConflict, "%s", err)
	}

	rt, err := d.Runtime.EnsureTCPRuntime(p.Name, p.PreferredPort)
	if err != nil {
		var exhausted *runtime.ErrPortExhausted
		if errors.As(err, &exhausted) {
			return RegisterResult{}, errf(CodePortExhausted, "%s", err)
		}
		return RegisterResult{}, errf(CodeInternal, "allocate frontend: %s", err)
	}

	rec := registry.ServiceRecord{
		Name:              p.Name,
		Kind:              kind,
		AppProtocol:       p.AppProtocol,
		OwnerKey:          ownerKey,
		Source:            source,
		Backend:           &p.Backend,
		Frontend:          rt.Frontend,
		Status:            registry.StatusReady,
		DockerContainerID: p.DockerContainerID,
		UpdatedAt:         time.Now(),
	}
	if _, err := d.Registry.CreateOrReplaceOwned(rec); err != nil {
		return RegisterResult{}, errf(CodeNameConflict, "%s", err)
	}
	d.Runtime.SetBackend(p.Name, p.Backend)

	res := RegisterResult{
		RegistrationID: regID,
		Name:           p.Name,
		Frontend:       rt.Frontend,
	}

	// Docker registrations are driven by container lifecycle, not leases.
	if source == registry.SourceDocker {
		d.logger.Info("service_registered", "service", p.Name, "source", source, "backend", p.Backend.Addr(), "frontend", rt.Frontend.Addr())
		return res, nil
	}

	token, err := lease.NewToken()
	if err != nil {
		return RegisterResult{}, errf(CodeInternal, "generate lease token: %s", err)
	}
	ttl := d.cfg.LeaseTTL
	if p.TTLSeconds > 0 {
		ttl = time.Duration(p.TTLSeconds) * time.Second
	}
	entry := d.Lease.AddWithTTL(regID, p.Name, ownerKey, token, ttl)
	res.LeaseToken = token
	res.ExpiresAt = &entry.ExpiresAt

	d.logger.Info("service_registered", "service", p.Name, "source", source, "backend", p.Backend.Addr(), "frontend", rt.Frontend.Addr())
	return res, nil
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

// DeleteRegistration removes a leased registration, keeping the frontend
// reserved but marking the service unavailable.
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
	d.mutate.Lock()
	d.Registry.MarkUnavailable(e.OwnerKey)
	d.Runtime.ClearBackend(e.Name)
	d.mutate.Unlock()
	d.logger.Info("service_backend_removed", "service", e.Name, "registration_id", id)
	return nil
}

// ForgetByOwner marks a service unavailable (used by the Docker watcher on
// container stop/destroy).
func (d *Daemon) ForgetByOwner(ownerKey, name string) {
	d.mutate.Lock()
	d.Registry.MarkUnavailable(ownerKey)
	d.Runtime.ClearBackend(name)
	d.mutate.Unlock()
	d.logger.Info("service_backend_removed", "service", name, "owner_key", ownerKey, "source", registry.SourceDocker)
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
	DockerContainerID string
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
		DockerContainerID: rec.DockerContainerID,
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
