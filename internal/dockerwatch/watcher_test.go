package dockerwatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func summaryFor(id string, labels map[string]string) container.Summary {
	return container.Summary{ID: id, Names: []string{"/checkout-db-1"}, Labels: labels}
}

func TestReconcileRegistersEnabledContainers(t *testing.T) {
	api := newFakeAPI()
	api.containers = []container.Summary{
		summaryFor("abc123", enabledLabels()),
		{ID: "ignore", Labels: map[string]string{}},
	}
	api.byID["abc123"] = inspectWith(enabledLabels(), "127.0.0.1", "49173")
	api.byID["ignore"] = inspectWith(map[string]string{}, "127.0.0.1", "1")

	var registered []Registration
	cb := Callbacks{
		OnRegister: func(_ context.Context, reg Registration) error {
			registered = append(registered, reg)
			return nil
		},
	}
	w := NewWatcher(api, false, cb, testLogger())
	if err := w.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(registered) != 1 {
		t.Fatalf("registered %d containers, want 1", len(registered))
	}
	if registered[0].Name != "checkout.postgres" || registered[0].BackendPort != 49173 {
		t.Fatalf("wrong registration: %+v", registered[0])
	}
}

func TestReconcileForgetsDisappearedContainers(t *testing.T) {
	api := newFakeAPI()
	api.containers = []container.Summary{summaryFor("abc123", enabledLabels())}
	api.byID["abc123"] = inspectWith(enabledLabels(), "127.0.0.1", "49173")

	var forgotten []Registration
	cb := Callbacks{
		OnRegister: func(context.Context, Registration) error { return nil },
		OnForget:   func(reg Registration) { forgotten = append(forgotten, reg) },
	}
	w := NewWatcher(api, false, cb, testLogger())
	if err := w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.tracked) != 1 {
		t.Fatalf("tracked %d, want 1", len(w.tracked))
	}

	// Container disappears; a second reconcile must forget it.
	api.containers = nil
	if err := w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(forgotten) != 1 || forgotten[0].Name != "checkout.postgres" || forgotten[0].ContainerID != "abc123" {
		t.Fatalf("forgotten = %+v, want the full registration for checkout.postgres", forgotten)
	}
	if len(w.tracked) != 0 {
		t.Fatalf("still tracking %d containers", len(w.tracked))
	}
}
