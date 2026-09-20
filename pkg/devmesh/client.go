package devmesh

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/wesen/devmesh/internal/api"
	"github.com/wesen/devmesh/internal/transport"
)

// Handle is a live registration. Endpoint returns the stable frontend.
type Handle interface {
	Endpoint() string
	Close() error
}

type registrationHandle struct {
	client  *transport.Client
	baseReq api.RegisterRequest

	mu       sync.Mutex
	name     string
	regID    string
	token    string
	endpoint string
	ttl      time.Duration
	closed   bool

	cancel context.CancelFunc
	done   chan struct{}
}

// Register registers an already-bound backend. Use ListenTCP for the common
// case where devmesh should also create the application listener.
func Register(ctx context.Context, opts RegistrationOptions) (Handle, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("devmesh: Name is required")
	}
	if opts.Backend == "" {
		return nil, fmt.Errorf("devmesh: Backend is required")
	}
	host, portStr, err := net.SplitHostPort(opts.Backend)
	if err != nil {
		return nil, fmt.Errorf("devmesh: invalid Backend %q: %w", opts.Backend, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("devmesh: invalid Backend port: %w", err)
	}
	socket := opts.Socket
	if socket == "" {
		socket = transport.DefaultSocketPath()
	}

	kind := opts.Kind
	if kind == "" {
		kind = KindTCP
	}
	hbCtx, cancel := context.WithCancel(context.Background())
	h := &registrationHandle{
		client: transport.NewClient(socket, 5*time.Second),
		name:   opts.Name,
		done:   make(chan struct{}),
		cancel: cancel,
		baseReq: api.RegisterRequest{
			Name:          opts.Name,
			Kind:          string(kind),
			AppProtocol:   opts.AppProtocol,
			Source:        "process",
			Backend:       api.BackendDTO{Host: host, Port: port},
			PreferredPort: opts.PreferredPort,
			TTLSeconds:    opts.TTLSeconds,
		},
	}
	if err := h.register(ctx); err != nil {
		cancel()
		return nil, err
	}
	go h.heartbeatLoop(hbCtx)
	return h, nil
}

func (h *registrationHandle) register(ctx context.Context) error {
	var resp api.RegisterResponse
	if err := h.client.Do(ctx, "POST", "/v1/registrations", h.baseReq, &resp); err != nil {
		return err
	}
	h.mu.Lock()
	h.regID = resp.RegistrationID
	h.token = resp.LeaseToken
	h.endpoint = net.JoinHostPort(resp.Frontend.Host, strconv.Itoa(resp.Frontend.Port))
	ttl := 15 * time.Second
	if h.baseReq.TTLSeconds > 0 {
		ttl = time.Duration(h.baseReq.TTLSeconds) * time.Second
	}
	h.ttl = ttl
	h.mu.Unlock()
	return nil
}

func (h *registrationHandle) heartbeatLoop(ctx context.Context) {
	defer close(h.done)
	h.mu.Lock()
	interval := h.ttl / 3
	h.mu.Unlock()
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	backoff := 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		h.mu.Lock()
		id, token := h.regID, h.token
		h.mu.Unlock()

		var hb api.HeartbeatResponse
		err := h.client.DoAuth(ctx, "POST", "/v1/registrations/"+id+"/heartbeat", token, nil, &hb)
		if err == nil {
			backoff = 100 * time.Millisecond
			continue
		}
		var te *transport.Error
		if asTransportError(err, &te) && te.Status == 404 {
			if rerr := h.register(ctx); rerr == nil {
				backoff = 100 * time.Millisecond
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
}

// Endpoint returns the stable frontend host:port.
func (h *registrationHandle) Endpoint() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endpoint
}

// Close stops heartbeating and best-effort deletes the registration.
func (h *registrationHandle) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	id, token := h.regID, h.token
	h.mu.Unlock()

	h.cancel()
	select {
	case <-h.done:
	case <-time.After(2 * time.Second):
	}

	if id == "" || token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = h.client.DoAuth(ctx, "DELETE", "/v1/registrations/"+id, token, nil, nil)
	return nil
}

func asTransportError(err error, target **transport.Error) bool {
	if err == nil {
		return false
	}
	te, ok := err.(*transport.Error)
	if ok {
		*target = te
	}
	return ok
}
