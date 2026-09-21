package registry

import (
	"sort"
	"sync"
	"time"
)

// Registry is an in-memory, mutex-protected map of logical services. It never
// performs network or filesystem I/O while holding its lock.
type Registry struct {
	mu       sync.RWMutex
	services map[string]ServiceRecord
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{services: map[string]ServiceRecord{}}
}

// CheckOwnership reports whether ownerKey may claim name. It returns a
// *NameConflictError when another owner already holds a ready service. A name
// whose previous owner is gone (status unavailable) may be taken over.
func (r *Registry) CheckOwnership(name, ownerKey string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	existing, ok := r.services[name]
	if ok && existing.OwnerKey != ownerKey && existing.Status != StatusUnavailable {
		return &NameConflictError{Name: name, OwnerKey: ownerKey}
	}
	return nil
}

// CreateOrReplaceOwned inserts or updates a record. A record with the same
// owner key may replace an existing one; a different owner key conflicts only
// while the existing service is not dormant.
func (r *Registry) CreateOrReplaceOwned(rec ServiceRecord) (ServiceRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.services[rec.Name]
	if ok && existing.OwnerKey != rec.OwnerKey && existing.Status != StatusUnavailable {
		return ServiceRecord{}, &NameConflictError{Name: rec.Name, OwnerKey: rec.OwnerKey}
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now()
	}
	r.services[rec.Name] = rec
	return rec.Clone(), nil
}

// SetBackend updates the backend and marks the service ready. It only applies
// when ownerKey owns the named service.
func (r *Registry) SetBackend(name, ownerKey string, b Backend) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.services[name]
	if !ok || rec.OwnerKey != ownerKey {
		return false
	}
	rec.Backend = &b
	rec.Status = StatusReady
	rec.UpdatedAt = time.Now()
	r.services[name] = rec
	return true
}

// ClearBackendIf marks the named service unavailable when producerID still
// identifies the current publication. A removal for a replaced producer is a
// no-op and returns false, so stale events and old leases cannot disable a
// newer backend. The frontend assignment is retained.
func (r *Registry) ClearBackendIf(name, producerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.services[name]
	if !ok || rec.ProducerID != producerID {
		return false
	}
	rec.Backend = nil
	rec.Status = StatusUnavailable
	rec.UpdatedAt = time.Now()
	r.services[name] = rec
	return true
}

// FindByHostname returns the service currently claiming the canonical hostname.
// It is used to keep one HTTP route owner per hostname.
func (r *Registry) FindByHostname(hostname string) (ServiceRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rec := range r.services {
		if rec.Hostname == hostname {
			return rec.Clone(), true
		}
	}
	return ServiceRecord{}, false
}

// Remove deletes a record owned by ownerKey.
func (r *Registry) Remove(ownerKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, rec := range r.services {
		if rec.OwnerKey == ownerKey {
			delete(r.services, name)
			return true
		}
	}
	return false
}

// Resolve returns a copy of the named record.
func (r *Registry) Resolve(name string) (ServiceRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.services[name]
	if !ok {
		return ServiceRecord{}, false
	}
	return rec.Clone(), true
}

// List returns copies of all records sorted by name.
func (r *Registry) List() []ServiceRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServiceRecord, 0, len(r.services))
	for _, rec := range r.services {
		out = append(out, rec.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len reports the number of records.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.services)
}
