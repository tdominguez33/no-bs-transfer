package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"no-bs-transfer/pkg/config"
)

func TestGetCloudflaredDownloadURL(t *testing.T) {
	url, err := GetCloudflaredDownloadURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty download URL")
	}
}

func TestDefaultPaths(t *testing.T) {
	dir := DefaultDownloadDir()
	if dir == "" {
		t.Fatal("expected non-empty DefaultDownloadDir")
	}

	bin := DefaultBinaryPath()
	if bin == "" {
		t.Fatal("expected non-empty DefaultBinaryPath")
	}
	if filepath.Dir(bin) != dir {
		t.Errorf("expected binary dir to match download dir: got %q, want %q", filepath.Dir(bin), dir)
	}
}

func TestDefaultPathsPortable(t *testing.T) {
	tmpDir := t.TempDir()
	config.SetExeDirOverride(tmpDir)
	defer config.SetExeDirOverride("")

	// Create portable marker
	markerPath := filepath.Join(tmpDir, "portable.txt")
	if err := os.WriteFile(markerPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create portable.txt: %v", err)
	}

	dir := DefaultDownloadDir()
	expectedDir := filepath.Join(tmpDir, "bin")
	if dir != expectedDir {
		t.Errorf("expected DefaultDownloadDir in portable mode to be %q, got %q", expectedDir, dir)
	}

	bin := DefaultBinaryPath()
	expectedBin := filepath.Join(expectedDir, "cloudflared.exe")
	if bin != expectedBin {
		t.Errorf("expected DefaultBinaryPath in portable mode to be %q, got %q", expectedBin, bin)
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-existing
	if fileExists(filepath.Join(tmpDir, "does-not-exist.exe")) {
		t.Error("expected false for non-existing file")
	}

	// Empty file (size 0)
	emptyPath := filepath.Join(tmpDir, "empty.exe")
	if err := os.WriteFile(emptyPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}
	if fileExists(emptyPath) {
		t.Error("expected false for 0-byte file")
	}

	// Directory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if fileExists(subDir) {
		t.Error("expected false for directory")
	}

	// Valid file with content
	validPath := filepath.Join(tmpDir, "valid.exe")
	if err := os.WriteFile(validPath, []byte("fake-binary-content"), 0644); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}
	if !fileExists(validPath) {
		t.Error("expected true for valid file with content")
	}
}

func TestDownloadCloudflaredContextCancel(t *testing.T) {
	// Setup slow test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(1 * time.Second)
		_, _ = w.Write([]byte("some data"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := DownloadCloudflared(ctx, nil)
	if err == nil {
		t.Fatal("expected error due to context cancellation or bad URL, got nil")
	}
}
