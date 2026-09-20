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

// Callbacks connect the watcher to the daemon.
type Callbacks struct {
	OnRegister func(ctx context.Context, reg Registration) error
	OnForget   func(ownerKey, name string)
	OnStatus   func(status string)
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

// Run reconciles then consumes events until ctx is canceled.
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

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := w.eventLoop(ctx)
		if ctx.Err() != nil {
			return nil
		}
		w.logger.Warn("docker_disconnected", "error", err)
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
// forgets previously tracked containers that are gone.
func (w *Watcher) Reconcile(ctx context.Context) error {
	containers, err := w.api.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	seen := map[string]bool{}
	for _, c := range containers {
		if labels, perr := ParseLabels(c.Labels); perr != nil || !labels.Enabled {
			continue
		}
		reg, err := w.registerContainer(ctx, c.ID)
		if err != nil {
			w.logger.Warn("docker_registration_failed", "container_id", c.ID, "error", err)
			continue
		}
		if reg != nil {
			seen[reg.ContainerID] = true
		}
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

func (w *Watcher) eventLoop(ctx context.Context) error {
	msgs, errs := w.api.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(filters.Arg("type", "container")),
	})
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			if err == nil {
				return errors.New("docker event stream closed")
			}
			return err
		case m := <-msgs:
			w.handleEvent(ctx, m)
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
		inspect, err := w.api.ContainerInspect(ctx, id)
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
		w.cb.OnForget(reg.OwnerKey, reg.Name)
	}
	w.logger.Info("service_backend_removed", "service", reg.Name, "owner_key", reg.OwnerKey, "source", "docker")
}
