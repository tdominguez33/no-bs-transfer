package tunnel

import "testing"

func TestFormatPublicURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"transfer.midominio.com", "https://transfer.midominio.com"},
		{"https://transfer.midominio.com", "https://transfer.midominio.com"},
		{"http://transfer.midominio.com", "https://transfer.midominio.com"},
		{"transfer.midominio.com/", "https://transfer.midominio.com"},
		{"https://transfer.midominio.com/", "https://transfer.midominio.com"},
		{"https://transfer.midominio.com/path", "https://transfer.midominio.com"},
		{"  transfer.midominio.com  ", "https://transfer.midominio.com"},
		{"", ""},
		{"   ", ""},
	}

	for _, tc := range tests {
		got := FormatPublicURL(tc.input)
		if got != tc.expected {
			t.Errorf("FormatPublicURL(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestHasDevFlag(t *testing.T) {
	if hasDevFlag("non-existent-flag", "NON_EXISTENT_ENV_12345") {
		t.Error("expected false when not set")
	}

	t.Setenv("TEST_ENV_VAR_FLAG", "1")
	if !hasDevFlag("test-flag", "TEST_ENV_VAR_FLAG") {
		t.Error("expected true when env var is 1")
	}

	t.Setenv("TEST_ENV_VAR_FLAG", "true")
	if !hasDevFlag("test-flag", "TEST_ENV_VAR_FLAG") {
		t.Error("expected true when env var is true")
	}
}

func TestIgnoreLocalInstallDevFlag(t *testing.T) {
	t.Setenv("NO_BS_IGNORE_LOCAL_INSTALL", "1")
	path, err := FindCloudflared()
	if err == nil {
		if path != DefaultBinaryPath() {
			t.Errorf("expected path to be DefaultBinaryPath %q, got %q", DefaultBinaryPath(), path)
		}
	}
}

func TestIsValidTunnelURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://funny-cat-123.trycloudflare.com", true},
		{"https://abc-def.trycloudflare.com", true},
		{"https://api.trycloudflare.com", false},
		{"http://api.trycloudflare.com", false},
		{"", false},
	}

	for _, tc := range tests {
		got := isValidTunnelURL(tc.url)
		if got != tc.expected {
			t.Errorf("isValidTunnelURL(%q) = %v; want %v", tc.url, got, tc.expected)
		}
	}
}

func TestShouldForceHTTP2(t *testing.T) {
	if ShouldForceHTTP2() {
		t.Error("expected false when not set")
	}

	t.Setenv("NO_BS_FORCE_HTTP2", "1")
	if !ShouldForceHTTP2() {
		t.Error("expected true when NO_BS_FORCE_HTTP2 is 1")
	}

	t.Setenv("NO_BS_FORCE_HTTP2", "")
	t.Setenv("NO_BS_HTTP2", "true")
	if !ShouldForceHTTP2() {
		t.Error("expected true when NO_BS_HTTP2 is true")
	}
}


