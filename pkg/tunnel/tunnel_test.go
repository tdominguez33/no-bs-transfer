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
