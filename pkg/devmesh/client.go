package devmesh

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-go-golems/devmesh/internal/api"
	"github.com/go-go-golems/devmesh/internal/transport"
)

// Handle is a live registration. Endpoint returns the stable frontend.
// Canceling the context passed to Register stops the heartbeat loop, and
// Close unregisters the service; either way the daemon expires the lease
// shortly after the producer disappears.
type Handle interface {
	Endpoint() string
	Close() error
}

// RegistrationInfo describes a live registration. It never contains the lease
// token.
type RegistrationInfo struct {
	RegistrationID string
	Name           string
	Endpoint       string
	ExpiresAt      time.Time
	TTL            time.Duration
}

// InfoHandle is a Handle that also exposes registration metadata. It is what
// Register and ListenTCP return.
type InfoHandle interface {
	Handle
	Info() RegistrationInfo
}

// defaultLeaseTTL is only used when the daemon response omits ttl_seconds.
const defaultLeaseTTL = 15 * time.Second

type registrationHandle struct {
	client  *transport.Client
	baseReq api.RegisterRequest

	mu       sync.Mutex
	name     string
	regID    string
	token    string
	endpoint string
	ttl      time.Duration
	expires  time.Time
	closed   bool

	cancel context.CancelFunc
	done   chan struct{}
}

// Register registers an already-bound backend. Use ListenTCP for the common
// case where devmesh should also create the application listener.
//
// The heartbeat loop runs until the passed context is canceled or Close is
// called, so a short-lived ctx bounds the whole registration, not just its
// creation.
func Register(ctx context.Context, opts RegistrationOptions) (InfoHandle, error) {
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
	source := "process"
	if opts.Manual {
		source = "manual"
	}
	hbCtx, cancel := context.WithCancel(ctx)
	h := &registrationHandle{
		client: transport.NewClient(socket, 5*time.Second),
		name:   opts.Name,
		done:   make(chan struct{}),
		cancel: cancel,
		baseReq: api.RegisterRequest{
			Name:        opts.Name,
			Kind:        string(kind),
			AppProtocol: opts.AppProtocol,
			Source:      source,
			Backend:     api.BackendDTO{Host: host, Port: port},
			HTTPHost:    opts.HTTPHost,
			TTLSeconds:  opts.TTLSeconds,
		},
	}
	if opts.PreferredPort != 0 {
		h.baseReq.PreferredPort = opts.PreferredPort
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
	ttl := defaultLeaseTTL
	if resp.TTLSeconds > 0 {
		ttl = time.Duration(resp.TTLSeconds) * time.Second
	}
	h.mu.Lock()
	h.regID = resp.RegistrationID
	h.token = resp.LeaseToken
	h.endpoint = frontendEndpoint(resp.Frontend)
	h.ttl = ttl
	h.expires = time.Time{}
	if resp.ExpiresAt != "" {
		if exp, err := time.Parse(time.RFC3339, resp.ExpiresAt); err == nil {
			h.expires = exp
		}
	}
	h.mu.Unlock()
	return nil
}

func frontendEndpoint(f api.FrontendDTO) string {
	if f.URL != "" {
		return f.URL
	}
	return net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
}

// heartbeatInterval is TTL/3 with a floor: the daemon rejects TTLs below three
// seconds, so the interval always leaves real margin.
func (h *registrationHandle) heartbeatInterval() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	interval := h.ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}

func (h *registrationHandle) heartbeatLoop(ctx context.Context) {
	defer close(h.done)
	timer := time.NewTimer(h.heartbeatInterval())
	defer timer.Stop()

	backoff := 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		h.mu.Lock()
		id, token := h.regID, h.token
		h.mu.Unlock()

		var hb api.HeartbeatResponse
		err := h.client.DoAuth(ctx, "POST", "/v1/registrations/"+id+"/heartbeat", token, nil, &hb)
		if err == nil {
			if exp, perr := time.Parse(time.RFC3339, hb.ExpiresAt); perr == nil {
				h.mu.Lock()
				h.expires = exp
				h.mu.Unlock()
			}
			backoff = 100 * time.Millisecond
			timer.Reset(h.heartbeatInterval())
			continue
		}
		var te *transport.Error
		if asTransportError(err, &te) && te.Status == 404 {
			// The daemon forgot the registration (typically after a restart):
			// re-register from scratch, reusing the remembered frontend.
			if rerr := h.register(ctx); rerr == nil {
				backoff = 100 * time.Millisecond
				timer.Reset(h.heartbeatInterval())
				continue
			}
		}
		// Transient failure: bounded, jittered retry. The daemon's lease
		// outlives a few missed heartbeats, so a short outage is survivable.
		timer.Reset(jitter(backoff))
		backoff *= 2
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
}

// jitter spreads retry attempts by +/-20%.
func jitter(d time.Duration) time.Duration {
	// #nosec G404 -- retry jitter does not require unpredictable bytes.
	f := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * f)
}

// Endpoint returns the stable frontend host:port (or URL for HTTP services).
func (h *registrationHandle) Endpoint() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endpoint
}

// Info returns the current registration metadata without the token.
func (h *registrationHandle) Info() RegistrationInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return RegistrationInfo{
		RegistrationID: h.regID,
		Name:           h.name,
		Endpoint:       h.endpoint,
		ExpiresAt:      h.expires,
		TTL:            h.ttl,
	}
}

// Close stops heartbeating and best-effort deletes the registration. It always
// deletes the latest registration identity, including one adopted by a
// re-registration after a daemon restart.
func (h *registrationHandle) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	h.cancel()
	select {
	case <-h.done:
	case <-time.After(2 * time.Second):
	}

	h.mu.Lock()
	id, token := h.regID, h.token
	h.mu.Unlock()
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
