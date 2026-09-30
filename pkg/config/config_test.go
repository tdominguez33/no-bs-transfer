package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
		{
			name:     "query params stripped",
			input:    "https://transfer.midominio.com?foo=bar",
			expected: "transfer.midominio.com",
		},
		{
			name:     "hash fragment stripped",
			input:    "https://transfer.midominio.com#section",
			expected: "transfer.midominio.com",
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

func TestGetAppDataConfigPath(t *testing.T) {
	path := GetAppDataConfigPath()
	if path == "" {
		t.Fatal("expected non-empty config path")
	}
	if filepath.Base(path) != "config.json" {
		t.Errorf("expected filename config.json, got %q", filepath.Base(path))
	}
}

func TestPortableMarker(t *testing.T) {
	tmpDir := t.TempDir()
	SetExeDirOverride(tmpDir)
	defer SetExeDirOverride("")

	// Initially no portable.txt
	if HasPortableMarker() {
		t.Fatal("expected HasPortableMarker to be false initially")
	}

	// Create portable.txt
	markerPath := filepath.Join(tmpDir, "portable.txt")
	if err := os.WriteFile(markerPath, []byte("portable"), 0644); err != nil {
		t.Fatalf("failed to create portable.txt: %v", err)
	}

	if !HasPortableMarker() {
		t.Fatal("expected HasPortableMarker to be true after creating portable.txt")
	}
}

func TestPortableLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	SetExeDirOverride(tmpDir)
	defer SetExeDirOverride("")

	// Create portable.txt
	markerPath := filepath.Join(tmpDir, "portable.txt")
	if err := os.WriteFile(markerPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create portable.txt: %v", err)
	}

	// Load should default to portable mode
	cfg := Load()
	if !cfg.SavePortable {
		t.Errorf("expected SavePortable to be true with portable.txt")
	}

	// Save should write config.json in tmpDir
	cfg.CustomDomain = "portable.test.com"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	localConfig := filepath.Join(tmpDir, "config.json")
	if _, err := os.Stat(localConfig); err != nil {
		t.Fatalf("expected config.json to exist at %q: %v", localConfig, err)
	}

	// Reload and verify
	reloaded := Load()
	if reloaded.CustomDomain != "portable.test.com" {
		t.Errorf("expected custom domain %q, got %q", "portable.test.com", reloaded.CustomDomain)
	}
	if !reloaded.SavePortable {
		t.Errorf("expected SavePortable to remain true")
	}
}

func TestDownloadDir(t *testing.T) {
	def := DefaultDownloadDir()
	if def == "" {
		t.Fatal("expected non-empty DefaultDownloadDir")
	}

	cfg := &AppConfig{
		DownloadDir: "",
	}
	eff := GetEffectiveDownloadDir(cfg)
	if eff != def {
		t.Errorf("GetEffectiveDownloadDir() with empty = %q; want %q", eff, def)
	}

	tmpDir := t.TempDir()
	cfg.DownloadDir = tmpDir
	eff = GetEffectiveDownloadDir(cfg)
	if eff != tmpDir {
		t.Errorf("GetEffectiveDownloadDir() with tmpDir = %q; want %q", eff, tmpDir)
	}
}
