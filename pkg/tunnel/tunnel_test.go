package tunnel

import (
	"fmt"
	"testing"
)

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

func TestCheckInternet(t *testing.T) {
	// Active internet environment should return true quickly
	ok := CheckInternet()
	if !ok {
		t.Log("Note: CheckInternet returned false (machine may be offline or endpoints blocked)")
	}
}

func TestFormatTunnelError(t *testing.T) {
	tests := []struct {
		err            error
		isCustomDomain bool
		expectedMsg    string
	}{
		{
			err:            fmt.Errorf("cloudflared se detuvo: Could not lookup srv records on _v2-origintunneld._tcp.argotunnel.com: lookup argotunnel.com: dnsquery: Se devolvió esta operación porque se agotó el tiempo de espera."),
			isCustomDomain: true,
			expectedMsg:    ErrNoInternet,
		},
		{
			err:            fmt.Errorf("dial tcp: lookup trycloudflare.com: no such host"),
			isCustomDomain: false,
			expectedMsg:    ErrNoInternet,
		},
		{
			err:            fmt.Errorf("cannot determine default configuration path: unauthorized token credentials"),
			isCustomDomain: true,
			expectedMsg:    ErrInvalidTunnelToken,
		},
		{
			err:            fmt.Errorf("rate limit exceeded on trycloudflare.com: 503 Service Unavailable"),
			isCustomDomain: false,
			expectedMsg:    ErrTunnelRateLimited,
		},
		{
			err:            fmt.Errorf("bind: address already in use"),
			isCustomDomain: false,
			expectedMsg:    ErrPortInUse,
		},
		{
			err:            fmt.Errorf("cloudflared no encontrado en el sistema"),
			isCustomDomain: false,
			expectedMsg:    ErrCloudflaredNotFound,
		},
		{
			err:            fmt.Errorf("error al ejecutar cloudflared (dominio propio): executable file not found"),
			isCustomDomain: true,
			expectedMsg:    ErrCloudflaredNotFound,
		},
	}

	for _, tc := range tests {
		formatted := FormatTunnelError(tc.err, tc.isCustomDomain)
		if formatted == nil || formatted.Error() != tc.expectedMsg {
			t.Errorf("FormatTunnelError(%v, %v) = %v; want %q", tc.err, tc.isCustomDomain, formatted, tc.expectedMsg)
		}
	}
}


