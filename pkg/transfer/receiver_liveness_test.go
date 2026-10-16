package transfer

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type progressLog struct {
	mu     sync.Mutex
	events []ReceiveProgress
}

func (l *progressLog) add(p ReceiveProgress) {
	l.mu.Lock()
	l.events = append(l.events, p)
	l.mu.Unlock()
}

// waitFor blocks until an event with the given status shows up after index `from`,
// returning the index right after it.
func (l *progressLog) waitFor(t *testing.T, status string, from int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for i := from; i < len(l.events); i++ {
			if l.events[i].Status == status {
				l.mu.Unlock()
				return i + 1
			}
		}
		l.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var got []string
	for _, e := range l.events {
		got = append(got, e.Status)
	}
	t.Fatalf("timed out waiting for status %q after event %d; got %v", status, from, got)
	return 0
}

func chunkURL(srv *ReceiveServer, uploadID string, idx, total int, size int64) string {
	return fmt.Sprintf("http://127.0.0.1:%d/upload?uploadId=%s&chunkIndex=%d&totalChunks=%d&totalSize=%d&fileName=big.bin",
		srv.Port(), uploadID, idx, total, size)
}

func postBytes(t *testing.T, url string, body []byte) int {
	t.Helper()
	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func newLivenessServer(t *testing.T) (*ReceiveServer, *progressLog, string) {
	t.Helper()
	dir := t.TempDir()
	log := &progressLog{}
	srv, err := NewReceiveServer(dir, 0, ReceiveCallbacks{OnProgress: log.add})
	if err != nil {
		t.Fatalf("NewReceiveServer failed: %v", err)
	}
	t.Cleanup(srv.Stop)
	return srv, log, dir
}

func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.part"))
	if len(matches) != 0 {
		t.Fatalf("expected .part files to be cleaned up, found %v", matches)
	}
}

func TestChunkedUploadCancelEndpoint(t *testing.T) {
	srv, log, dir := newLivenessServer(t)

	if code := postBytes(t, chunkURL(srv, "up_c", 0, 2, 20), []byte("0123456789")); code != http.StatusOK {
		t.Fatalf("chunk 0 expected 200, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "big.bin.part")); err != nil {
		t.Fatalf("expected .part file after first chunk: %v", err)
	}

	cancelURL := fmt.Sprintf("http://127.0.0.1:%d/upload/cancel?uploadId=up_c", srv.Port())
	if code := postBytes(t, cancelURL, nil); code != http.StatusNoContent {
		t.Fatalf("cancel expected 204, got %d", code)
	}

	log.waitFor(t, "cancelled", 0, 2*time.Second)
	assertNoPartFiles(t, dir)

	if code := postBytes(t, chunkURL(srv, "up_c", 1, 2, 20), []byte("abcdefghij")); code != http.StatusBadRequest {
		t.Fatalf("chunk after cancel expected 400, got %d", code)
	}
}

func TestChunkedUploadCancelInterruptsInFlightChunk(t *testing.T) {
	srv, log, dir := newLivenessServer(t)

	if code := postBytes(t, chunkURL(srv, "up_f", 0, 2, 20), []byte("0123456789")); code != http.StatusOK {
		t.Fatalf("chunk 0 expected 200, got %d", code)
	}

	// Chunk 1 starts streaming, then the sender goes silent with the request still open.
	pr, pw := io.Pipe()
	defer pw.Close()
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		resp, err := http.Post(chunkURL(srv, "up_f", 1, 2, 20), "application/octet-stream", pr)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	if _, err := pw.Write([]byte("abc")); err != nil {
		t.Fatalf("pipe write failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cancelURL := fmt.Sprintf("http://127.0.0.1:%d/upload/cancel?uploadId=up_f", srv.Port())
	postBytes(t, cancelURL, nil)

	log.waitFor(t, "cancelled", 0, 2*time.Second)
	assertNoPartFiles(t, dir)

	select {
	case <-reqDone:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight chunk request was not interrupted by cancel")
	}
}

func TestChunkedUploadInactivityWatchdog(t *testing.T) {
	prevStall, prevAbort, prevInterval := uploadStallAfter, uploadAbortAfter, uploadWatchInterval
	uploadStallAfter, uploadAbortAfter, uploadWatchInterval = 200*time.Millisecond, 800*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() {
		uploadStallAfter, uploadAbortAfter, uploadWatchInterval = prevStall, prevAbort, prevInterval
	})

	srv, log, dir := newLivenessServer(t)

	if code := postBytes(t, chunkURL(srv, "up_w", 0, 2, 20), []byte("0123456789")); code != http.StatusOK {
		t.Fatalf("chunk 0 expected 200, got %d", code)
	}

	// Silent sender: stalled first, speed reported as 0.
	next := log.waitFor(t, "stalled", 0, 2*time.Second)
	log.mu.Lock()
	stalled := log.events[next-1]
	log.mu.Unlock()
	if stalled.Speed != 0 || stalled.BytesRecv != 10 {
		t.Fatalf("stalled event should report speed 0 and 10 bytes, got speed=%f bytes=%d", stalled.Speed, stalled.BytesRecv)
	}

	// A heartbeat brings it back to uploading.
	pingURL := fmt.Sprintf("http://127.0.0.1:%d/upload/ping?uploadId=up_w", srv.Port())
	if code := postBytes(t, pingURL, nil); code != http.StatusNoContent {
		t.Fatalf("ping expected 204, got %d", code)
	}
	next = log.waitFor(t, "uploading", next, time.Second)

	// No more heartbeats: stalled again, then cancelled and cleaned up.
	next = log.waitFor(t, "stalled", next, 2*time.Second)
	log.waitFor(t, "cancelled", next, 3*time.Second)
	assertNoPartFiles(t, dir)

	if code := postBytes(t, pingURL, nil); code != http.StatusNotFound {
		t.Fatalf("ping after abort expected 404, got %d", code)
	}
}
