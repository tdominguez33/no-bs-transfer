package config

import "testing"

func TestCleanDomainHost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean domain stays unchanged",
			input:    "transfer.midominio.com",
			expected: "transfer.midominio.com",
		},
		{
			name:     "https prefix stripped",
			input:    "https://transfer.midominio.com",
			expected: "transfer.midominio.com",
		},
		{
			name:     "http prefix stripped",
			input:    "http://transfer.midominio.com",
			expected: "transfer.midominio.com",
		},
		{
			name:     "uppercase HTTPS prefix stripped",
			input:    "HTTPS://Transfer.MiDominio.com",
			expected: "Transfer.MiDominio.com",
		},
		{
			name:     "trailing slash stripped",
			input:    "transfer.midominio.com/",
			expected: "transfer.midominio.com",
		},
		{
			name:     "https prefix and trailing slash stripped",
			input:    "https://transfer.midominio.com/",
			expected: "transfer.midominio.com",
		},
		{
			name:     "https prefix and subpath stripped",
			input:    "https://transfer.midominio.com/download/file",
			expected: "transfer.midominio.com",
		},
		{
			name:     "clean domain with subpath stripped",
			input:    "transfer.midominio.com/path",
			expected: "transfer.midominio.com",
		},
		{
			name:     "whitespace trimmed",
			input:    "   https://transfer.midominio.com/   ",
			expected: "transfer.midominio.com",
		},
		{
			name:     "single slash https prefix stripped",
			input:    "https:/transfer.midominio.com",
			expected: "transfer.midominio.com",
		},
		{
			name:     "colon only https prefix stripped",
			input:    "https:transfer.midominio.com",
			expected: "transfer.midominio.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanDomainHost(tc.input)
			if got != tc.expected {
				t.Errorf("CleanDomainHost(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}
