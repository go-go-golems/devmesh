// Package lease manages time-bounded process/manual registrations so that a
// crashed producer does not leave a stale backend forever.
package lease

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ErrNotFound indicates the daemon no longer knows the registration.
var ErrNotFound = errors.New("registration not found")

// ErrUnauthorized indicates a bad or missing lease token.
var ErrUnauthorized = errors.New("unauthorized")

// Entry is one active leased registration.
type Entry struct {
	RegistrationID string
	Name           string
	OwnerKey       string
	Token          string
	// TTL is the effective duration for this entry. It is set from the
	// registration request (or the daemon default) and is used both for the
	// initial expiry and for every renewal, so a requested TTL stays in force
	// across heartbeats.
	TTL       time.Duration
	ExpiresAt time.Time
}

// Manager tracks leased registrations in memory.
type Manager struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*Entry
}

// NewManager builds a lease manager with the given TTL.
func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	return &Manager{ttl: ttl, entries: map[string]*Entry{}}
}

// TTL returns the configured lease duration.
func (m *Manager) TTL() time.Duration { return m.ttl }

// NewToken returns a cryptographically random lease token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Add records a new lease using the manager TTL.
func (m *Manager) Add(id, name, ownerKey, token string) Entry {
	return m.AddWithTTL(id, name, ownerKey, token, m.ttl)
}

// AddWithTTL records a new lease with an explicit TTL.
func (m *Manager) AddWithTTL(id, name, ownerKey, token string, ttl time.Duration) Entry {
	if ttl <= 0 {
		ttl = m.ttl
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := &Entry{
		RegistrationID: id,
		Name:           name,
		OwnerKey:       ownerKey,
		Token:          token,
		TTL:            ttl,
		ExpiresAt:      time.Now().Add(ttl),
	}
	m.entries[id] = e
	return *e
}

// Renew extends a lease after verifying the token. Renewal uses the entry's
// effective TTL and fails once the lease has reached its expiry, so an expired
// registration cannot be resurrected; the client must register anew.
func (m *Manager) Renew(id, token string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return time.Time{}, ErrNotFound
	}
	now := time.Now()
	if !now.Before(e.ExpiresAt) {
		// Expired but not yet swept: treat exactly like a missing registration.
		delete(m.entries, id)
		return time.Time{}, ErrNotFound
	}
	if !tokenEqual(e.Token, token) {
		return time.Time{}, ErrUnauthorized
	}
	e.ExpiresAt = now.Add(e.TTL)
	return e.ExpiresAt, nil
}

// Delete removes a lease after verifying the token.
func (m *Manager) Delete(id, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return ErrNotFound
	}
	if !tokenEqual(e.Token, token) {
		return ErrUnauthorized
	}
	delete(m.entries, id)
	return nil
}

// Remove drops a lease unconditionally (used by the expiry sweeper).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, id)
}

// Get returns a copy of an entry.
func (m *Manager) Get(id string) (Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Expired returns copies of entries whose lease has elapsed without removing
// them. Prefer TakeExpired for expiry handling: it selects and removes under
// one lock so a lease cannot be renewed between selection and removal.
func (m *Manager) Expired(now time.Time) []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Entry
	for _, e := range m.entries {
		if !now.Before(e.ExpiresAt) {
			out = append(out, *e)
		}
	}
	return out
}

// TakeExpired removes and returns the entries whose leases have elapsed. The
// select-and-delete happens under one lock, so an expired entry can never be
// renewed after (or concurrently with) being taken by the sweeper.
func (m *Manager) TakeExpired(now time.Time) []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Entry
	for id, e := range m.entries {
		if !now.Before(e.ExpiresAt) {
			out = append(out, *e)
			delete(m.entries, id)
		}
	}
	return out
}

// Len reports the number of active leases.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

func tokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
