package transfer

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRangeStart(t *testing.T) {
	fileSize := int64(1000)

	tests := []struct {
		header   string
		expected int64
	}{
		{"", 0},
		{"bytes=0-", 0},
		{"bytes=100-", 100},
		{"bytes=500-999", 500},
		{"bytes=-200", 800},
		{"invalid-range", 0},
		{"bytes=abc-", 0},
	}

	for _, tc := range tests {
		got := parseRangeStart(tc.header, fileSize)
		if got != tc.expected {
			t.Errorf("parseRangeStart(%q, %d) = %d; want %d", tc.header, fileSize, got, tc.expected)
		}
	}
}

func TestTrackerRestartAndResume(t *testing.T) {
	tracker := NewTracker(nil)
	defer tracker.Stop()

	guestID := "guest1"
	fileID := "file1"
	fileName := "test.bin"
	fileSize := int64(1000)
	ip := "127.0.0.1"
	ua := "Mozilla/5.0"

	// 1. Initial download from scratch
	gt := tracker.StartTransfer(guestID, fileID, fileName, fileSize, ip, ua, 0)
	if gt.BytesSent != 0 || gt.Progress != 0 || gt.Status != "downloading" {
		t.Fatalf("expected initial transfer to be at 0, got bytes=%d progress=%f status=%s", gt.BytesSent, gt.Progress, gt.Status)
	}

	// 2. Transfer 500 bytes (50%)
	tracker.AddBytes(gt.ID, 500)
	if atomic.LoadInt64(&gt.BytesSent) != 500 || gt.Progress != 50 {
		t.Fatalf("expected 500 bytes (50%%), got bytes=%d progress=%f", gt.BytesSent, gt.Progress)
	}

	// 3. Client cancels or pauses -> connection closes
	tracker.OnConnectionClosed(gt.ID)
	if gt.Status != "paused" || gt.Speed != 0 {
		t.Fatalf("expected status=paused and speed=0, got status=%s speed=%f", gt.Status, gt.Speed)
	}

	// 4. Guest starts from scratch (offset 0) -> progress MUST reset
	gt = tracker.StartTransfer(guestID, fileID, fileName, fileSize, ip, ua, 0)
	if gt.BytesSent != 0 || gt.Progress != 0 || gt.Status != "downloading" {
		t.Fatalf("expected reset on fresh start, got bytes=%d progress=%f", gt.BytesSent, gt.Progress)
	}

	// Transfer 200 bytes
	tracker.AddBytes(gt.ID, 200)
	if atomic.LoadInt64(&gt.BytesSent) != 200 || gt.Progress != 20 {
		t.Fatalf("expected 200 bytes (20%%), got bytes=%d progress=%f", gt.BytesSent, gt.Progress)
	}

	// 5. Client pauses
	tracker.OnConnectionClosed(gt.ID)

	// 6. Client resumes from byte 200
	gt = tracker.StartTransfer(guestID, fileID, fileName, fileSize, ip, ua, 200)
	if gt.BytesSent != 200 || gt.Progress != 20 || gt.Status != "downloading" {
		t.Fatalf("expected resume from 200, got bytes=%d progress=%f", gt.BytesSent, gt.Progress)
	}

	// Complete transfer (add 800 bytes)
	tracker.AddBytes(gt.ID, 800)
	if atomic.LoadInt64(&gt.BytesSent) != 1000 || gt.Progress != 100 || gt.Status != "completed" {
		t.Fatalf("expected completed at 1000 bytes, got bytes=%d progress=%f status=%s", gt.BytesSent, gt.Progress, gt.Status)
	}

	// Connection closes after completion
	tracker.OnConnectionClosed(gt.ID)
	if gt.Status != "completed" {
		t.Fatalf("expected status to stay completed, got %s", gt.Status)
	}
}

func TestTrackerCancelledTimeout(t *testing.T) {
	tracker := NewTracker(nil)
	defer tracker.Stop()

	gt := tracker.StartTransfer("guest2", "file2", "photo.jpg", 500, "127.0.0.1", "Mozilla/5.0", 0)
	tracker.AddBytes(gt.ID, 100)
	tracker.OnConnectionClosed(gt.ID)

	if gt.Status != "paused" {
		t.Fatalf("expected paused, got %s", gt.Status)
	}

	// Artificially simulate 31 seconds elapsed
	tracker.mu.Lock()
	gt.LastActive = time.Now().Add(-35 * time.Second)
	tracker.mu.Unlock()

	// Wait 1.1s for speedLoop tick
	time.Sleep(1200 * time.Millisecond)

	tracker.mu.Lock()
	status := gt.Status
	tracker.mu.Unlock()

	if status != "cancelled" {
		t.Fatalf("expected status to transition to cancelled after timeout, got %s", status)
	}
}

func TestTrackerMultiConnectionRange(t *testing.T) {
	tracker := NewTracker(nil)
	defer tracker.Stop()

	// Connection 1 starts at byte 0
	gt1 := tracker.StartTransfer("guest_multi", "file_multi", "movie.mp4", 1000, "127.0.0.1", "aria2/1.36.0", 0)
	tracker.AddBytes(gt1.ID, 150)
	if atomic.LoadInt64(&gt1.BytesSent) != 150 {
		t.Fatalf("expected 150 bytes, got %d", atomic.LoadInt64(&gt1.BytesSent))
	}

	// Concurrent Connection 2 opens at offset 500 while Connection 1 is still active
	gt2 := tracker.StartTransfer("guest_multi", "file_multi", "movie.mp4", 1000, "127.0.0.1", "aria2/1.36.0", 500)
	if gt1 != gt2 {
		t.Fatal("expected gt1 and gt2 to reference same transfer instance")
	}

	// BytesSent must NOT be reset to 500
	if atomic.LoadInt64(&gt2.BytesSent) != 150 {
		t.Fatalf("expected BytesSent to remain 150, got %d", atomic.LoadInt64(&gt2.BytesSent))
	}

	// Connection 2 adds 200 bytes
	tracker.AddBytes(gt2.ID, 200)
	if atomic.LoadInt64(&gt2.BytesSent) != 350 {
		t.Fatalf("expected 350 bytes, got %d", atomic.LoadInt64(&gt2.BytesSent))
	}

	// Connection 1 closes
	tracker.OnConnectionClosed(gt1.ID)
	// Status should still be downloading because Connection 2 is active!
	if gt2.Status != "downloading" {
		t.Fatalf("expected status to remain downloading while connection 2 is active, got %s", gt2.Status)
	}

	// Connection 2 closes
	tracker.OnConnectionClosed(gt2.ID)
	if gt2.Status != "paused" {
		t.Fatalf("expected status paused after all connections closed, got %s", gt2.Status)
	}
}
