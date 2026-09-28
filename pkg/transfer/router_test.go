package transfer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleRouter(t *testing.T) {
	tmpDir := t.TempDir()
	dummyFile := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(dummyFile, []byte("hello transfer"), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	files := []*SharedFile{
		{
			ID:       "123456",
			Name:     "sample.txt",
			Path:     dummyFile,
			Size:     "14 B",
			RawBytes: 14,
		},
	}
	bundleCode := "654321"

	srv, err := NewServer(files, bundleCode, 0, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Stop()

	// 1. Root / returns 404
	reqRoot := httptest.NewRequest("GET", "/", nil)
	rrRoot := httptest.NewRecorder()
	srv.handleRouter(rrRoot, reqRoot)
	if rrRoot.Code != http.StatusNotFound {
		t.Errorf("expected / to return 404, got %d", rrRoot.Code)
	}

	// 2. /{bundleCode} directly returns 200 and landing page
	reqBundle := httptest.NewRequest("GET", "/"+bundleCode, nil)
	rrBundle := httptest.NewRecorder()
	srv.handleRouter(rrBundle, reqBundle)
	if rrBundle.Code != http.StatusOK {
		t.Errorf("expected /%s to return 200, got %d", bundleCode, rrBundle.Code)
	}
	if !strings.Contains(rrBundle.Body.String(), "Archivos disponibles") {
		t.Errorf("expected bundle landing page to contain 'Archivos disponibles'")
	}
	if !strings.Contains(rrBundle.Body.String(), "sample.txt") {
		t.Errorf("expected bundle landing page to contain 'sample.txt'")
	}

	// 3. /b/{bundleCode} also returns 200 for backward compatibility
	reqOldBundle := httptest.NewRequest("GET", "/b/"+bundleCode, nil)
	rrOldBundle := httptest.NewRecorder()
	srv.handleRouter(rrOldBundle, reqOldBundle)
	if rrOldBundle.Code != http.StatusOK {
		t.Errorf("expected /b/%s to return 200, got %d", bundleCode, rrOldBundle.Code)
	}

	// 4. /{fileID} serves the file content
	reqFile := httptest.NewRequest("GET", "/123456", nil)
	rrFile := httptest.NewRecorder()
	srv.handleRouter(rrFile, reqFile)
	if rrFile.Code != http.StatusOK {
		t.Errorf("expected /123456 to return 200, got %d", rrFile.Code)
	}
	if rrFile.Body.String() != "hello transfer" {
		t.Errorf("expected file body 'hello transfer', got %q", rrFile.Body.String())
	}

	// 5. Non-existent path returns 404
	req404 := httptest.NewRequest("GET", "/999999", nil)
	rr404 := httptest.NewRecorder()
	srv.handleRouter(rr404, req404)
	if rr404.Code != http.StatusNotFound {
		t.Errorf("expected non-existent code to return 404, got %d", rr404.Code)
	}
}
