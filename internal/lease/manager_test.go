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
