package config

import "testing"

func TestFileMapperFlatKeys(t *testing.T) {
	raw := map[string]any{
		"socket":            "/tmp/x.sock",
		"tcp_frontend_min":  16000,
		"tcp_frontend_max":  16999,
		"lease_ttl":         "20s",
		"shutdown_timeout":  "2s",
		"state_path":        "/tmp/state.json",
		"tcp_frontend_host": "127.0.0.1",
	}
	got, err := FileMapper(raw)
	if err != nil {
		t.Fatal(err)
	}
	sec := got[SectionSlug]
	want := map[string]any{
		"socket":            "/tmp/x.sock",
		"tcp-frontend-min":  16000,
		"tcp-frontend-max":  16999,
		"lease-ttl":         "20s",
		"shutdown-timeout":  "2s",
		"state":             "/tmp/state.json",
		"tcp-frontend-host": "127.0.0.1",
	}
	for k, v := range want {
		if sec[k] != v {
			t.Errorf("field %s = %v, want %v", k, sec[k], v)
		}
	}
}

func TestFileMapperRejectsRemovedIdleTTL(t *testing.T) {
	// runtime_idle_ttl was removed when automatic runtime reaping was
	// dropped; supplying it must produce a migration error, not silence.
	_, err := FileMapper(map[string]any{"runtime_idle_ttl": "10m"})
	if err == nil {
		t.Fatal("removed runtime_idle_ttl key accepted silently")
	}
}

func TestFileMapperNestedDockerAndHTTP(t *testing.T) {
	raw := map[string]any{
		"docker": map[string]any{
			"enabled":                            true,
			"allow_non_loopback_published_ports": false,
		},
		"http": map[string]any{
			"enabled":     true,
			"http_addr":   "127.0.0.1:8088",
			"https_addr":  "127.0.0.1:8443",
			"base_domain": "dev.example.com",
			"cert_file":   "/certs/cert.pem",
			"key_file":    "/certs/key.pem",
		},
	}
	got, err := FileMapper(raw)
	if err != nil {
		t.Fatal(err)
	}
	sec := got[SectionSlug]
	if sec["docker-enabled"] != true {
		t.Errorf("docker-enabled = %v", sec["docker-enabled"])
	}
	if sec["docker-allow-non-loopback-published-ports"] != false {
		t.Errorf("docker allow non loopback = %v", sec["docker-allow-non-loopback-published-ports"])
	}
	if sec["http-enabled"] != true || sec["http-addr"] != "127.0.0.1:8088" {
		t.Errorf("http mapping wrong: %v", sec)
	}
	if sec["http-base-domain"] != "dev.example.com" || sec["http-cert-file"] != "/certs/cert.pem" || sec["http-key-file"] != "/certs/key.pem" {
		t.Errorf("http tls mapping wrong: %v", sec)
	}
}

func TestFileMapperNestedDockerWithoutEnabled(t *testing.T) {
	// A partial docker object must not fabricate a docker-enabled value.
	got, err := FileMapper(map[string]any{"docker": map[string]any{"allow_non_loopback_published_ports": true}})
	if err != nil {
		t.Fatal(err)
	}
	sec := got[SectionSlug]
	if _, ok := sec["docker-enabled"]; ok {
		t.Errorf("docker-enabled should be omitted when absent, got %v", sec["docker-enabled"])
	}
	if sec["docker-allow-non-loopback-published-ports"] != true {
		t.Errorf("allow_non_loopback not mapped: %v", sec)
	}
}

func TestFileMapperRejectsNonMapping(t *testing.T) {
	if _, err := FileMapper([]any{1, 2}); err == nil {
		t.Fatal("expected error for non-mapping config root")
	}
	if _, err := FileMapper(map[string]any{"docker": "not-a-map"}); err == nil {
		t.Fatal("expected error for non-mapping docker section")
	}
}
