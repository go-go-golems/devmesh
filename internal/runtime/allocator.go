package runtime

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"

	"github.com/wesen/devmesh/internal/state"
)

// ErrPortExhausted indicates no bindable port remained in the configured range.
type ErrPortExhausted struct {
	Min, Max int
}

func (e *ErrPortExhausted) Error() string {
	return fmt.Sprintf("no free frontend port in range %d-%d", e.Min, e.Max)
}

// Allocation is a bound frontend port plus the listener that owns it. Returning
// the listener (not just the port) removes the allocation-to-bind race.
type Allocation struct {
	Port     int
	Listener net.Listener
}

// Allocator binds stable frontend ports by actually listening, preferring a
// remembered port, then a requested preferred port, then a deterministic hash
// offset across the configured range.
type Allocator struct {
	host   string
	min    int
	max    int
	state  *state.Store
	logger *slog.Logger
}

// NewAllocator builds an allocator. min/max must be a valid inclusive range.
func NewAllocator(host string, min, max int, st *state.Store, logger *slog.Logger) *Allocator {
	return &Allocator{host: host, min: min, max: max, state: st, logger: logger}
}

// Allocate binds and returns a frontend listener for name.
func (a *Allocator) Allocate(name string, preferred int) (Allocation, error) {
	remembered := 0
	if a.state != nil {
		remembered = a.state.Port(name)
	}
	if remembered != 0 {
		if alloc, ok, err := a.try(name, remembered); err != nil {
			return Allocation{}, err
		} else if ok {
			a.logger.Info("frontend_reused", "service", name, "frontend", allocAddr(a.host, remembered))
			return alloc, nil
		}
		a.logger.Warn("frontend_remembered_unavailable", "service", name, "port", remembered)
	}
	if preferred != 0 {
		if alloc, ok, err := a.try(name, preferred); err != nil {
			return Allocation{}, err
		} else if ok {
			a.logger.Info("frontend_allocated", "service", name, "frontend", allocAddr(a.host, preferred), "preferred", true)
			return alloc, nil
		}
	}
	size := a.max - a.min + 1
	if size <= 0 {
		return Allocation{}, &ErrPortExhausted{Min: a.min, Max: a.max}
	}
	start := startOffset(name, size)
	for i := 0; i < size; i++ {
		port := a.min + (start+i)%size
		if port == remembered || port == preferred {
			continue
		}
		if alloc, ok, err := a.try(name, port); err != nil {
			return Allocation{}, err
		} else if ok {
			a.logger.Info("frontend_allocated", "service", name, "frontend", allocAddr(a.host, port))
			return alloc, nil
		}
	}
	return Allocation{}, &ErrPortExhausted{Min: a.min, Max: a.max}
}

func (a *Allocator) try(name string, port int) (Allocation, bool, error) {
	ln, err := net.Listen("tcp", allocAddr(a.host, port))
	if err != nil {
		return Allocation{}, false, nil
	}
	if a.state != nil {
		if err := a.state.SetPort(name, port); err != nil {
			// A newly allocated stable frontend is not successful until its
			// remembered assignment is durable. Close this listener rather than
			// claiming a persistence guarantee we could not make.
			_ = ln.Close()
			a.logger.Error("state_save_failed", "service", name, "error", err)
			return Allocation{}, false, fmt.Errorf("persist frontend assignment for %s: %w", name, err)
		}
	}
	return Allocation{Port: port, Listener: ln}, true, nil
}

func allocAddr(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func startOffset(name string, size int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(size))
}
