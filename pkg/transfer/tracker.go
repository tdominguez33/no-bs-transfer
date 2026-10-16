package transfer

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GuestTransfer represents a single file download in progress by a client
type GuestTransfer struct {
	ID          string    `json:"id"`
	GuestID     string    `json:"guestId"`
	FileID      string    `json:"fileId"`
	FileName    string    `json:"fileName"`
	IP          string    `json:"ip"`
	Device      string    `json:"device"`
	UserAgent   string    `json:"userAgent"`
	BytesSent   int64     `json:"bytesSent"`
	TotalBytes  int64     `json:"totalBytes"`
	Speed       float64   `json:"speed"` // Bytes per second
	Progress    float64   `json:"progress"` // 0 to 100
	Status      string    `json:"status"` // "downloading", "completed", "paused", "cancelled"
	ConnectedAt time.Time `json:"connectedAt"`
	LastActive  time.Time `json:"lastActive"`

	// Internal
	lastBytes   int64
	activeConns int32
}

// TransferStats summarizes the global transfer state
type TransferStats struct {
	TotalSpeed     float64          `json:"totalSpeed"` // Bytes per second
	TotalBytesSent int64            `json:"totalBytesSent"`
	ActiveGuests   int              `json:"activeGuests"`
	Transfers      []*GuestTransfer `json:"transfers"`
}

// Tracker manages state across multiple files and guests
type Tracker struct {
	mu             sync.RWMutex
	notifyMu       sync.Mutex
	transfers      map[string]*GuestTransfer
	totalBytesSent int64
	onUpdate       func(stats TransferStats)
	stopChan       chan struct{}
	stopOnce       sync.Once
}

// NewTracker creates a new multi-file tracker
func NewTracker(onUpdate func(stats TransferStats)) *Tracker {
	t := &Tracker{
		transfers: make(map[string]*GuestTransfer),
		onUpdate:  onUpdate,
		stopChan:  make(chan struct{}),
	}

	go t.speedLoop()
	return t
}

// Stop shuts down the background calculation loop
func (t *Tracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopChan)
	})
}

// StartTransfer starts or resumes tracking a guest downloading a specific file.
// If startOffset is 0, it resets progress (new download or restarted from scratch).
// If startOffset > 0, it sets progress to that offset when resuming a paused download.
func (t *Tracker) StartTransfer(guestID, fileID, fileName string, fileSize int64, ip, userAgent string, startOffset int64) *GuestTransfer {
	key := guestID + ":" + fileID

	t.mu.Lock()
	defer t.mu.Unlock()

	gt, exists := t.transfers[key]
	if !exists {
		gt = &GuestTransfer{
			ID:          key,
			GuestID:     guestID,
			FileID:      fileID,
			FileName:    fileName,
			IP:          ip,
			Device:      parseDevice(userAgent),
			UserAgent:   userAgent,
			TotalBytes:  fileSize,
			Status:      "downloading",
			ConnectedAt: time.Now(),
			LastActive:  time.Now(),
		}
		t.transfers[key] = gt
	}

	gt.IP = ip
	gt.Device = parseDevice(userAgent)
	gt.UserAgent = userAgent
	gt.LastActive = time.Now()
	gt.Status = "downloading"
	activeBefore := atomic.AddInt32(&gt.activeConns, 1) - 1

	if activeBefore == 0 {
		curBytes := atomic.LoadInt64(&gt.BytesSent)
		if startOffset <= 0 {
			atomic.StoreInt64(&gt.BytesSent, 0)
			gt.lastBytes = 0
			gt.Progress = 0
			gt.Speed = 0
			gt.ConnectedAt = time.Now()
		} else if !exists || startOffset <= curBytes {
			atomic.StoreInt64(&gt.BytesSent, startOffset)
			gt.lastBytes = startOffset
			if gt.TotalBytes > 0 {
				pct := float64(startOffset) / float64(gt.TotalBytes) * 100
				if pct > 100 {
					pct = 100
				}
				gt.Progress = pct
			}
		}
	}

	return gt
}

// GetOrCreateTransfer is kept for backward compatibility
func (t *Tracker) GetOrCreateTransfer(guestID, fileID, fileName string, fileSize int64, ip, userAgent string) *GuestTransfer {
	return t.StartTransfer(guestID, fileID, fileName, fileSize, ip, userAgent, 0)
}

// OnConnectionClosed is called immediately when an HTTP connection terminates
func (t *Tracker) OnConnectionClosed(key string) {
	t.mu.Lock()
	gt, exists := t.transfers[key]
	if !exists {
		t.mu.Unlock()
		return
	}

	remaining := atomic.AddInt32(&gt.activeConns, -1)
	if remaining < 0 {
		atomic.StoreInt32(&gt.activeConns, 0)
		remaining = 0
	}

	if remaining == 0 {
		curBytes := atomic.LoadInt64(&gt.BytesSent)
		gt.Speed = 0
		gt.lastBytes = curBytes
		gt.LastActive = time.Now()

		if (gt.TotalBytes == 0 || curBytes >= gt.TotalBytes) && gt.TotalBytes >= 0 {
			gt.Status = "completed"
			gt.Progress = 100
		} else if gt.Status != "completed" {
			gt.Status = "paused"
		}
	}

	stats := t.collectStatsLocked()
	t.mu.Unlock()

	t.notifyUpdate(stats)
}

// AddBytes increments bytes transferred for a specific guest/file
func (t *Tracker) AddBytes(key string, n int64) {
	t.mu.Lock()
	gt, exists := t.transfers[key]
	if exists {
		sent := atomic.AddInt64(&gt.BytesSent, n)
		atomic.AddInt64(&t.totalBytesSent, n)
		gt.LastActive = time.Now()

		if gt.TotalBytes > 0 {
			pct := float64(sent) / float64(gt.TotalBytes) * 100
			if pct > 100 {
				pct = 100
			}
			gt.Progress = pct
			if sent >= gt.TotalBytes {
				gt.Status = "completed"
			} else {
				gt.Status = "downloading"
			}
		}
	}
	t.mu.Unlock()
}

// RemoveFileTransfers cancels and removes all active transfers for a given file ID
func (t *Tracker) RemoveFileTransfers(fileID string) {
	t.mu.Lock()
	for key, gt := range t.transfers {
		if gt.FileID == fileID {
			delete(t.transfers, key)
		}
	}

	stats := t.collectStatsLocked()
	t.mu.Unlock()

	t.notifyUpdate(stats)
}

func (t *Tracker) collectStatsLocked() TransferStats {
	var totalSpeed float64
	activeCount := 0
	list := make([]*GuestTransfer, 0, len(t.transfers))

	for _, gt := range t.transfers {
		if gt.Speed > 0 {
			totalSpeed += gt.Speed
		}
		if atomic.LoadInt32(&gt.activeConns) > 0 || gt.Speed > 0 {
			activeCount++
		}
		clone := *gt
		clone.BytesSent = atomic.LoadInt64(&gt.BytesSent)
		list = append(list, &clone)
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].ConnectedAt.Equal(list[j].ConnectedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].ConnectedAt.Before(list[j].ConnectedAt)
	})

	return TransferStats{
		TotalSpeed:     totalSpeed,
		TotalBytesSent: atomic.LoadInt64(&t.totalBytesSent),
		ActiveGuests:   activeCount,
		Transfers:      list,
	}
}

func (t *Tracker) notifyUpdate(stats TransferStats) {
	if t.onUpdate != nil {
		t.notifyMu.Lock()
		defer t.notifyMu.Unlock()
		t.onUpdate(stats)
	}
}

// speedLoop calculates speeds every second and notifies the UI
func (t *Tracker) speedLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.mu.Lock()
			now := time.Now()

			for _, gt := range t.transfers {
				curBytes := atomic.LoadInt64(&gt.BytesSent)
				active := atomic.LoadInt32(&gt.activeConns)

				if active > 0 {
					diff := curBytes - gt.lastBytes
					if diff < 0 {
						diff = 0
					}
					gt.Speed = float64(diff)
					gt.lastBytes = curBytes

					if gt.Speed > 0 {
						gt.Status = "downloading"
						gt.LastActive = now
					}
				} else {
					gt.Speed = 0
					gt.lastBytes = curBytes

					if gt.Status == "downloading" {
						if gt.TotalBytes > 0 && curBytes >= gt.TotalBytes {
							gt.Status = "completed"
							gt.Progress = 100
						} else {
							gt.Status = "paused"
							gt.LastActive = now
						}
					} else if gt.Status == "paused" && now.Sub(gt.LastActive) > 30*time.Second {
						gt.Status = "cancelled"
					}
				}
			}

			stats := t.collectStatsLocked()
			t.mu.Unlock()

			t.notifyUpdate(stats)
		}
	}
}

func parseDevice(ua string) string {
	low := strings.ToLower(ua)
	switch {
	case strings.Contains(low, "iphone"):
		return "iPhone"
	case strings.Contains(low, "ipad"):
		return "iPad"
	case strings.Contains(low, "android"):
		return "Android"
	case strings.Contains(low, "macintosh") || strings.Contains(low, "mac os"):
		return "macOS"
	case strings.Contains(low, "windows"):
		return "Windows"
	case strings.Contains(low, "linux"):
		return "Linux"
	case strings.Contains(low, "aria2"):
		return "aria2"
	case strings.Contains(low, "curl"):
		return "curl"
	case strings.Contains(low, "wget"):
		return "wget"
	case strings.Contains(low, "python"):
		return "Python"
	case strings.Contains(low, "powershell"):
		return "PowerShell"
	default:
		return "Browser"
	}
}
