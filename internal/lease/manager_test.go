package lease

import (
	"testing"
	"time"
)

func TestRenewAndDelete(t *testing.T) {
	m := NewManager(50 * time.Millisecond)
	e := m.Add("reg-1", "svc", "process:1", "secret")
	if e.ExpiresAt.IsZero() {
		t.Fatal("no expiry set")
	}

	newExp, err := m.Renew("reg-1", "secret")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !newExp.After(e.ExpiresAt) && newExp.Equal(e.ExpiresAt) {
		t.Fatal("renew did not extend lease")
	}

	if _, err := m.Renew("reg-1", "wrong"); err != ErrUnauthorized {
		t.Fatalf("wrong token: got %v, want ErrUnauthorized", err)
	}
	if err := m.Delete("reg-1", "wrong"); err != ErrUnauthorized {
		t.Fatalf("delete wrong token: got %v", err)
	}
	if err := m.Delete("reg-1", "secret"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := m.Renew("reg-1", "secret"); err != ErrNotFound {
		t.Fatalf("renew after delete: got %v, want ErrNotFound", err)
	}
}

// TestRequestedTTLSurvivesRenewal pins the P06 regression: a registration that
// requested a 60s lease must renew to 60s, not to the manager default.
func TestRequestedTTLSurvivesRenewal(t *testing.T) {
	m := NewManager(15 * time.Second)
	m.AddWithTTL("reg-ttl", "svc", "process:1", "tok", 60*time.Second)
	_, err := m.Renew("reg-ttl", "tok")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	entry, ok := m.Get("reg-ttl")
	if !ok {
		t.Fatal("renewed entry missing")
	}
	if remaining := time.Until(entry.ExpiresAt); remaining < 50*time.Second || remaining > 60*time.Second {
		t.Fatalf("remaining TTL = %v, want ~60s", remaining)
	}
}

// TestRenewFailsAtExpiry pins the expiry boundary: a lease that has reached
// its deadline cannot be resurrected by renewal.
func TestRenewFailsAtExpiry(t *testing.T) {
	m := NewManager(20 * time.Millisecond)
	m.Add("reg-exp", "svc", "process:1", "tok")
	time.Sleep(30 * time.Millisecond)
	if _, err := m.Renew("reg-exp", "tok"); err != ErrNotFound {
		t.Fatalf("renew at expiry: got %v, want ErrNotFound", err)
	}
	if m.Len() != 0 {
		t.Fatalf("expired entry retained: %d", m.Len())
	}
}

func TestExpiry(t *testing.T) {
	m := NewManager(20 * time.Millisecond)
	m.Add("reg-2", "svc", "process:2", "tok")
	if got := m.Expired(time.Now()); len(got) != 0 {
		t.Fatalf("expired before TTL: %v", got)
	}
	time.Sleep(30 * time.Millisecond)
	got := m.Expired(time.Now())
	if len(got) != 1 || got[0].RegistrationID != "reg-2" {
		t.Fatalf("expected reg-2 expired, got %v", got)
	}
	m.Remove("reg-2")
	if m.Len() != 0 {
		t.Fatalf("Len = %d, want 0", m.Len())
	}
}

// TestTakeExpiredRemovesAtomically pins the P07 regression: a lease selected
// as expired is removed under the same lock, so a renewal cannot race the
// sweeper and still be deleted.
func TestTakeExpiredRemovesAtomically(t *testing.T) {
	m := NewManager(time.Minute)
	m.AddWithTTL("reg-3", "svc", "process:3", "tok", 60*time.Second)
	taken := m.TakeExpired(time.Now().Add(2 * time.Minute))
	if len(taken) != 1 || taken[0].RegistrationID != "reg-3" {
		t.Fatalf("take = %+v", taken)
	}
	if _, err := m.Renew("reg-3", "tok"); err != ErrNotFound {
		t.Fatalf("renew after take: got %v, want ErrNotFound", err)
	}
	// A not-yet-expired entry is left alone.
	m.AddWithTTL("reg-4", "svc", "process:4", "tok", 60*time.Second)
	if got := m.TakeExpired(time.Now()); len(got) != 0 {
		t.Fatalf("unexpired entry taken: %v", got)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1", m.Len())
	}
}

func TestTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("duplicate token generated")
		}
		seen[tok] = true
	}
}
