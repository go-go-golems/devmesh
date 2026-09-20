package registry

import "testing"

func TestValidateName(t *testing.T) {
	valid := []string{"checkout.postgres", "checkout-api", "billing.api.v2", "a", "a1.b2-c3"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Checkout.Api", "checkout_postgres", ".checkout", "checkout.", "under_score", "white space"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestValidateBackend(t *testing.T) {
	if err := ValidateBackend(Backend{Host: "127.0.0.1", Port: 8080}); err != nil {
		t.Errorf("loopback backend rejected: %v", err)
	}
	if err := ValidateBackend(Backend{Host: "::1", Port: 8080}); err != nil {
		t.Errorf("ipv6 loopback backend rejected: %v", err)
	}
	if err := ValidateBackend(Backend{Host: "10.0.0.5", Port: 8080}); err == nil {
		t.Error("remote backend accepted, want rejection")
	}
	if err := ValidateBackend(Backend{Host: "127.0.0.1", Port: 0}); err == nil {
		t.Error("zero port accepted, want rejection")
	}
}

func TestOwnershipRules(t *testing.T) {
	r := New()
	rec := ServiceRecord{Name: "checkout.postgres", OwnerKey: "docker:checkout:db:5432", Kind: KindTCP, Status: StatusReady}
	if _, err := r.CreateOrReplaceOwned(rec); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Same owner may replace the backend.
	rec2 := rec
	rec2.Backend = &Backend{Host: "127.0.0.1", Port: 49173}
	if _, err := r.CreateOrReplaceOwned(rec2); err != nil {
		t.Fatalf("same owner replace failed: %v", err)
	}
	got, _ := r.Resolve("checkout.postgres")
	if got.Backend == nil || got.Backend.Port != 49173 {
		t.Fatalf("backend not replaced: %+v", got.Backend)
	}

	// A different owner conflicts.
	other := ServiceRecord{Name: "checkout.postgres", OwnerKey: "process:xyz", Kind: KindTCP}
	if _, err := r.CreateOrReplaceOwned(other); err == nil {
		t.Fatal("different owner registration succeeded, want conflict")
	}
	if err := r.CheckOwnership("checkout.postgres", "process:xyz"); err == nil {
		t.Fatal("CheckOwnership allowed foreign owner")
	}
	if err := r.CheckOwnership("checkout.postgres", "docker:checkout:db:5432"); err != nil {
		t.Fatalf("CheckOwnership rejected true owner: %v", err)
	}

	// MarkUnavailable keeps the frontend but clears the backend.
	if !r.MarkUnavailable("docker:checkout:db:5432") {
		t.Fatal("MarkUnavailable returned false")
	}
	got, _ = r.Resolve("checkout.postgres")
	if got.Status != StatusUnavailable || got.Backend != nil {
		t.Fatalf("MarkUnavailable state wrong: %+v", got)
	}
}

func TestResolveUnknown(t *testing.T) {
	r := New()
	if _, ok := r.Resolve("nope"); ok {
		t.Fatal("Resolve returned unknown service")
	}
}
