package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiveServerUpload(t *testing.T) {
	tmpDir := t.TempDir()

	var doneFiles []ReceivedFile
	var progressEvents []ReceiveProgress

	callbacks := ReceiveCallbacks{
		OnProgress: func(p ReceiveProgress) {
			progressEvents = append(progressEvents, p)
		},
		OnFileDone: func(f ReceivedFile) {
			doneFiles = append(doneFiles, f)
		},
	}

	srv, err := NewReceiveServer(tmpDir, 0, callbacks)
	if err != nil {
		t.Fatalf("NewReceiveServer failed: %v", err)
	}
	defer srv.Stop()

	if srv.Port() <= 0 {
		t.Fatalf("expected valid port, got %d", srv.Port())
	}

	uploadURL := fmt.Sprintf("http://127.0.0.1:%d/upload", srv.Port())

	// 1. Upload first file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test_photo.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	content := []byte("fake image content 12345")
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write content failed: %v", err)
	}
	_ = writer.Close()

	req, err := http.NewRequest(http.MethodPost, uploadURL, body)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var res struct {
		Success bool           `json:"success"`
		Files   []ReceivedFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("Decode response failed: %v", err)
	}

	if !res.Success || len(res.Files) != 1 {
		t.Fatalf("unexpected response: %+v", res)
	}

	savedFile := filepath.Join(tmpDir, "test_photo.jpg")
	readBack, err := os.ReadFile(savedFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(readBack) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(readBack), string(content))
	}

	// 2. Upload duplicate file to verify unique renaming
	body2 := &bytes.Buffer{}
	writer2 := multipart.NewWriter(body2)
	part2, err := writer2.CreateFormFile("file", "test_photo.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	content2 := []byte("different content 67890")
	if _, err := part2.Write(content2); err != nil {
		t.Fatalf("write content failed: %v", err)
	}
	_ = writer2.Close()

	req2, _ := http.NewRequest(http.MethodPost, uploadURL, body2)
	req2.Header.Set("Content-Type", writer2.FormDataContentType())

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second POST /upload failed: %v", err)
	}
	defer resp2.Body.Close()

	savedDuplicate := filepath.Join(tmpDir, "test_photo (1).jpg")
	readBack2, err := os.ReadFile(savedDuplicate)
	if err != nil {
		t.Fatalf("ReadFile of duplicate failed: %v", err)
	}
	if string(readBack2) != string(content2) {
		t.Errorf("content mismatch on duplicate: got %q, want %q", string(readBack2), string(content2))
	}

	if len(doneFiles) != 2 {
		t.Errorf("expected 2 doneFiles callback invocations, got %d", len(doneFiles))
	}
}

func TestReceiveServerUploadPage(t *testing.T) {
	tmpDir := t.TempDir()
	srv, err := NewReceiveServer(tmpDir, 0, ReceiveCallbacks{})
	if err != nil {
		t.Fatalf("NewReceiveServer failed: %v", err)
	}
	defer srv.Stop()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", srv.Port()))
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("Enviar archivos a tu PC")) {
		t.Errorf("expected upload page title in response body")
	}
}
