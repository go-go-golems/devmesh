package dockerwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
)

// dockerCallTimeout bounds each individual Docker API call so a stuck daemon
// cannot stall reconciliation or event handling indefinitely.
const dockerCallTimeout = 10 * time.Second

// reconcileInterval is the periodic full-inventory repair pass. Events give
// prompt updates; the periodic pass repairs anything a missed event (or a
// snapshot/subscription gap) left behind. A short discovery delay after an
// event gap is an accepted trade-off instead of replay machinery.
const reconcileInterval = 5 * time.Second

// Callbacks connect the watcher to the daemon.
type Callbacks struct {
	OnRegister func(ctx context.Context, reg Registration) error
	// OnForget receives the full registration so the daemon can compare the
	// concrete container identity and ignore stale stop events for a
	// container that has already been replaced.
	OnForget func(reg Registration)
	OnStatus func(status string)
}

// Watcher reconciles running containers at startup and reacts to lifecycle
// events, reconnecting with backoff and re-reconciling after any gap.
type Watcher struct {
	api              DockerAPI
	allowNonLoopback bool
	logger           *slog.Logger
	cb               Callbacks

	mu      sync.Mutex
	tracked map[string]Registration // container ID -> registration
}

// NewWatcher builds a watcher.
func NewWatcher(api DockerAPI, allowNonLoopback bool, cb Callbacks, logger *slog.Logger) *Watcher {
	return &Watcher{
		api:              api,
		allowNonLoopback: allowNonLoopback,
		logger:           logger,
		cb:               cb,
		tracked:          map[string]Registration{},
	}
}

// Run reconciles then consumes events until ctx is canceled. A periodic
// reconciliation pass repairs missed events; the event stream only provides
// responsiveness.
func (w *Watcher) Run(ctx context.Context) error {
	defer func() { _ = w.api.Close() }()

	if err := w.reconcileWithRetry(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		w.setStatus("degraded")
		w.logger.Warn("docker_reconcile_failed", "error", err)
	} else {
		w.setStatus("connected")
	}

	reconcileTicker := time.NewTicker(reconcileInterval)
	defer reconcileTicker.Stop()

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		// eventLoop returns on stream failure, a closed channel, or ctx
		// cancellation; a periodic tick reconciles in place instead.
		streamCtx, cancelStream := context.WithCancel(ctx)
		streamFailed := w.eventLoop(streamCtx, reconcileTicker.C)
		cancelStream()
		if ctx.Err() != nil {
			return nil
		}
		if !streamFailed {
			// eventLoop exited because ctx was canceled.
			return nil
		}
		w.logger.Warn("docker_disconnected")
		w.setStatus("degraded")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		if rerr := w.Reconcile(ctx); rerr != nil {
			continue
		}
		w.setStatus("connected")
		backoff = time.Second
	}
}

func (w *Watcher) setStatus(status string) {
	if w.cb.OnStatus != nil {
		w.cb.OnStatus(status)
	}
}

// listContainers bounds the Docker list call with its own deadline.
func (w *Watcher) listContainers(ctx context.Context) ([]container.Summary, error) {
	cctx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()
	return w.api.ContainerList(cctx, container.ListOptions{})
}

// inspectContainer bounds the Docker inspect call with its own deadline.
func (w *Watcher) inspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	cctx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()
	return w.api.ContainerInspect(cctx, id)
}

func (w *Watcher) reconcileWithRetry(ctx context.Context) error {
	var err error
	backoff := 250 * time.Millisecond
	for attempt := 0; attempt < 5; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = w.Reconcile(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return err
}

// Reconcile lists running containers, registers devmesh-enabled ones, and
// forgets previously tracked containers that are gone. A container that is
// present in the list but fails inspection is not treated as absent: a
// transient failure must not fabricate disappearance.
func (w *Watcher) Reconcile(ctx context.Context) error {
	containers, err := w.listContainers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	// seen contains containers that are still present and devmesh-managed,
	// regardless of whether registration succeeded this pass.
	seen := map[string]bool{}
	for _, c := range containers {
		labels, perr := ParseLabels(c.Labels)
		if perr != nil {
			w.logger.Warn("docker_registration_failed", "container_id", c.ID, "error", perr)
			continue
		}
		if !labels.Enabled {
			continue
		}
		seen[c.ID] = true
		reg, rerr := w.registerContainer(ctx, c.ID)
		if rerr != nil {
			w.logger.Warn("docker_registration_failed", "container_id", c.ID, "error", rerr)
			continue
		}
		_ = reg // registration already tracked in registerContainer
	}

	// Forget tracked containers that are no longer present.
	w.mu.Lock()
	var gone []Registration
	for id, reg := range w.tracked {
		if !seen[id] {
			gone = append(gone, reg)
			delete(w.tracked, id)
		}
	}
	w.mu.Unlock()
	for _, reg := range gone {
		w.forget(reg)
	}
	return nil
}

// eventLoop consumes the event stream until it fails, closes, or ctx is
// canceled. It returns true when the stream failed and a reconnect is needed.
// The periodic reconcile ticker is serviced in the same select so missed
// events are repaired even while the stream is otherwise idle.
func (w *Watcher) eventLoop(ctx context.Context, ticks <-chan time.Time) bool {
	msgs, errs := w.api.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(filters.Arg("type", "container")),
	})
	for {
		select {
		case <-ctx.Done():
			return false
		case err := <-errs:
			if err == nil {
				return true
			}
			return true
		case m, ok := <-msgs:
			if !ok {
				return true
			}
			w.handleEvent(ctx, m)
		case <-ticks:
			if err := w.Reconcile(ctx); err != nil {
				w.logger.Warn("docker_reconcile_failed", "error", err)
			}
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, m events.Message) {
	action := string(m.Action)
	id := m.Actor.ID
	switch action {
	case "start", "restart":
		if _, err := w.registerContainer(ctx, id); err != nil {
			w.logger.Warn("docker_registration_failed", "container_id", id, "error", err)
		}
	case "die", "stop", "destroy":
		w.mu.Lock()
		reg, ok := w.tracked[id]
		if ok {
			delete(w.tracked, id)
		}
		w.mu.Unlock()
		if ok {
			w.forget(reg)
		}
	}
}

// registerContainer inspects a container with bounded retry for the start-event
// race, registers it, and tracks it. It returns nil when the container is not
// (or no longer) eligible.
func (w *Watcher) registerContainer(ctx context.Context, id string) (*Registration, error) {
	delays := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1500 * time.Millisecond,
		2 * time.Second,
	}
	for attempt := 0; ; attempt++ {
		inspect, err := w.inspectContainer(ctx, id)
		if err != nil {
			return nil, err
		}
		if inspect.State == nil || !inspect.State.Running {
			return nil, nil
		}
		reg, err := RegistrationFromInspect(inspect, w.allowNonLoopback)
		if err == nil {
			if rerr := w.cb.OnRegister(ctx, reg); rerr != nil {
				return nil, rerr
			}
			w.mu.Lock()
			w.tracked[reg.ContainerID] = reg
			w.mu.Unlock()
			w.logger.Info("docker_container_discovered", "service", reg.Name, "backend", fmt.Sprintf("%s:%d", reg.BackendHost, reg.BackendPort), "owner_key", reg.OwnerKey)
			return &reg, nil
		}
		var np *ErrNotPublished
		if errors.As(err, &np) && attempt < len(delays) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delays[attempt]):
				continue
			}
		}
		return nil, err
	}
}

func (w *Watcher) forget(reg Registration) {
	if w.cb.OnForget != nil {
		w.cb.OnForget(reg)
	}
	w.logger.Info("docker_container_forgotten", "service", reg.Name, "owner_key", reg.OwnerKey, "container_id", reg.ContainerID)
}
