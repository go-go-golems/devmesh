package dockerwatch

import (
	"testing"
)

func TestParseLabels(t *testing.T) {
	if l, err := ParseLabels(map[string]string{}); err != nil || l.Enabled {
		t.Fatalf("empty labels: got %+v, %v; want not enabled", l, err)
	}
	if l, err := ParseLabels(map[string]string{LabelEnable: "false", LabelName: "x"}); err != nil || l.Enabled {
		t.Fatalf("enable=false: got %+v, %v; want not enabled", l, err)
	}
	l, err := ParseLabels(map[string]string{
		LabelEnable:        "true",
		LabelName:          "checkout.postgres",
		LabelContainerPort: "5432",
		LabelKind:          "tcp",
		LabelAppProtocol:   "postgres",
		LabelPreferredPort: "5432",
	})
	if err != nil {
		t.Fatalf("valid labels: %v", err)
	}
	if !l.Enabled || l.Name != "checkout.postgres" || l.ContainerPort != 5432 || l.PreferredPort != 5432 || l.AppProtocol != "postgres" {
		t.Fatalf("parsed wrong: %+v", l)
	}

	bad := []map[string]string{
		{LabelEnable: "true"},
		{LabelEnable: "true", LabelName: "x"},
		{LabelEnable: "true", LabelName: "x", LabelContainerPort: "notaport"},
		{LabelEnable: "true", LabelName: "x", LabelContainerPort: "5432", LabelKind: "udp"},
	}
	for _, m := range bad {
		if _, err := ParseLabels(m); err == nil {
			t.Errorf("ParseLabels(%v) = nil error, want error", m)
		}
	}
}

func TestOwnerKeyUsesComposeThenName(t *testing.T) {
	compose := map[string]string{
		ComposeProjectLabel: "checkout",
		ComposeServiceLabel: "db",
	}
	if got := OwnerKey("/checkout-db-1", compose, 5432); got != "docker:checkout:db:5432" {
		t.Fatalf("compose owner key = %q", got)
	}
	if got := OwnerKey("/checkout-db-1", map[string]string{}, 5432); got != "docker:checkout-db-1:5432" {
		t.Fatalf("fallback owner key = %q", got)
	}
}
