package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"sync"
	"time"

	"no-bs-transfer/pkg/config"
	"no-bs-transfer/pkg/transfer"
	"no-bs-transfer/pkg/tunnel"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FileShareItem represents an individual file with its direct download link
type FileShareItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      string `json:"size"`
	DirectURL string `json:"directUrl"`
}

// MultiShareResult contains both the bundle URL and individual direct links
type MultiShareResult struct {
	PublicBaseURL string          `json:"publicBaseUrl"`
	BundleURL     string          `json:"bundleUrl"`
	Files         []FileShareItem `json:"files"`
	LocalURLs     []string        `json:"localUrls"`
	TotalSize     string          `json:"totalSize"`
}

// App struct
type App struct {
	ctx           context.Context
	tunnelManager *tunnel.Manager
	server        *transfer.Server
	mu            sync.Mutex
	activeFiles   []*transfer.SharedFile
	bundleCode    string
	usedCodes     map[string]bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		tunnelManager: tunnel.NewManager(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.SaveWindowDimensions()
	return false
}

func (a *App) shutdown(ctx context.Context) {
	a.StopShare()
}

// SaveWindowDimensions saves current window width and height to config
func (a *App) SaveWindowDimensions() {
	if a.ctx == nil {
		return
	}
	if wailsRuntime.WindowIsMaximised(a.ctx) || wailsRuntime.WindowIsMinimised(a.ctx) {
		return
	}
	w, h := wailsRuntime.WindowGetSize(a.ctx)
	if w >= 450 && h >= 300 {
		cfg := config.Load()
		if cfg.WindowWidth != w || cfg.WindowHeight != h {
			cfg.WindowWidth = w
			cfg.WindowHeight = h
			_ = config.Save(cfg)
		}
	}
}

// GetConfig returns the application configuration loaded from disk
func (a *App) GetConfig() *config.AppConfig {
	return config.Load()
}

// SaveConfig persists the application configuration to disk
func (a *App) SaveConfig(cfg *config.AppConfig) error {
	existing := config.Load()
	if cfg.WindowWidth <= 0 && existing.WindowWidth > 0 {
		cfg.WindowWidth = existing.WindowWidth
	}
	if cfg.WindowHeight <= 0 && existing.WindowHeight > 0 {
		cfg.WindowHeight = existing.WindowHeight
	}
	return config.Save(cfg)
}

// SelectFiles opens native Windows file dialog allowing multiple selection
func (a *App) SelectFiles() ([]*transfer.SharedFile, error) {
	filePaths, err := wailsRuntime.OpenMultipleFilesDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Seleccionar uno o más archivos",
	})
	if err != nil {
		return nil, fmt.Errorf("error al abrir selector de archivos: %w", err)
	}

	return a.LoadFilesFromPaths(filePaths)
}

func generateStagingFileID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("f_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("f_%x", b)
}

// LoadFilesFromPaths loads metadata for multiple file paths (e.g. from drag & drop)
func (a *App) LoadFilesFromPaths(paths []string) ([]*transfer.SharedFile, error) {
	var result []*transfer.SharedFile

	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue // Skip unreadable paths
		}

		if fi.IsDir() {
			continue // Currently skip directories
		}

		sf := &transfer.SharedFile{
			ID:       generateStagingFileID(),
			Path:     p,
			Name:     fi.Name(),
			Size:     formatBytes(fi.Size()),
			RawBytes: fi.Size(),
		}
		result = append(result, sf)
	}

	return result, nil
}

func generate6DigitCode(existing map[string]bool) string {
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			continue
		}
		code := fmt.Sprintf("%06d", n.Int64())
		if !existing[code] {
			existing[code] = true
			return code
		}
	}
}

// StartShare initiates the local server and Cloudflare tunnel for multiple files
func (a *App) StartShare(files []*transfer.SharedFile) (*MultiShareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(files) == 0 {
		return nil, fmt.Errorf("no hay archivos para compartir")
	}

	// Stop any existing share
	if a.server != nil {
		a.server.Stop()
		a.server = nil
	}
	a.tunnelManager.Stop()

	// Assign unique 6-digit random codes to each file and bundle
	a.usedCodes = make(map[string]bool)
	a.bundleCode = generate6DigitCode(a.usedCodes)
	for _, f := range files {
		f.ID = generate6DigitCode(a.usedCodes)
	}
	a.activeFiles = files

	// 1. Check config for custom domain & fixed port
	cfg := config.Load()
	port := 0
	if cfg.UseCustomDomain && cfg.FixedPort > 0 {
		port = cfg.FixedPort
	}

	// 2. Start HTTP Server with bundleCode
	srv, err := transfer.NewServer(files, a.bundleCode, port, func(stats transfer.TransferStats) {
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "transfer_update", stats)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("fallo al iniciar servidor de transferencia: %w", err)
	}
	a.server = srv

	// 3. Start Cloudflare Tunnel (Named Tunnel or Quick Tunnel)
	var publicURL string
	if cfg.UseCustomDomain && cfg.TunnelToken != "" && cfg.CustomDomain != "" {
		publicURL, err = a.tunnelManager.StartWithToken(cfg.TunnelToken, cfg.CustomDomain)
		if err != nil {
			srv.Stop()
			a.server = nil
			return nil, fmt.Errorf("fallo al iniciar túnel con dominio personalizado: %w", err)
		}
	} else {
		publicURL, err = a.tunnelManager.Start(srv.Port())
		if err != nil {
			srv.Stop()
			a.server = nil
			return nil, fmt.Errorf("fallo al generar túnel de Cloudflare: %w", err)
		}
	}

	// Build direct links per file (directly /{code})
	var fileItems []FileShareItem
	for _, f := range files {
		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: fmt.Sprintf("%s/%s", publicURL, f.ID),
		})
	}

	localIPs := getLocalIPs(srv.Port())

	var totalBytes int64
	for _, f := range files {
		totalBytes += f.RawBytes
	}

	return &MultiShareResult{
		PublicBaseURL: publicURL,
		BundleURL:     fmt.Sprintf("%s/%s", publicURL, a.bundleCode),
		Files:         fileItems,
		LocalURLs:     localIPs,
		TotalSize:     formatBytes(totalBytes),
	}, nil
}

// AddFilesToShare dynamically adds new files to an already active sharing tunnel
func (a *App) AddFilesToShare(newFiles []*transfer.SharedFile) (*MultiShareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil || !a.tunnelManager.IsRunning() {
		return nil, fmt.Errorf("no hay sesión activa")
	}

	if a.usedCodes == nil {
		a.usedCodes = make(map[string]bool)
	}

	existingPaths := make(map[string]bool)
	for _, f := range a.activeFiles {
		existingPaths[f.Path] = true
	}

	var added []*transfer.SharedFile
	for _, f := range newFiles {
		if existingPaths[f.Path] {
			continue
		}
		existingPaths[f.Path] = true
		f.ID = generate6DigitCode(a.usedCodes)
		a.activeFiles = append(a.activeFiles, f)
		added = append(added, f)
	}

	if len(added) > 0 {
		a.server.AddFiles(added)
	}

	publicURL := a.tunnelManager.GetURL()
	var fileItems []FileShareItem
	var totalBytes int64
	for _, f := range a.activeFiles {
		totalBytes += f.RawBytes
		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: fmt.Sprintf("%s/%s", publicURL, f.ID),
		})
	}

	return &MultiShareResult{
		PublicBaseURL: publicURL,
		BundleURL:     fmt.Sprintf("%s/%s", publicURL, a.bundleCode),
		Files:         fileItems,
		LocalURLs:     getLocalIPs(a.server.Port()),
		TotalSize:     formatBytes(totalBytes),
	}, nil
}

// RemoveFileFromShare removes an individual file from an active sharing session and cancels active downloads for it
func (a *App) RemoveFileFromShare(fileID string) (*MultiShareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil || !a.tunnelManager.IsRunning() {
		return nil, fmt.Errorf("no hay sesión activa")
	}

	// 1. Remove from server (cancels active downloads and removes tracker stats)
	a.server.RemoveFile(fileID)

	// 2. Filter a.activeFiles
	var remaining []*transfer.SharedFile
	for _, f := range a.activeFiles {
		if f.ID != fileID {
			remaining = append(remaining, f)
		}
	}
	a.activeFiles = remaining

	// 3. If no files left, terminate the session
	if len(remaining) == 0 {
		if a.server != nil {
			a.server.Stop()
			a.server = nil
		}
		a.tunnelManager.Stop()
		a.activeFiles = nil
		a.bundleCode = ""
		a.usedCodes = nil

		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "share_stopped", true)
		}

		return &MultiShareResult{
			Files: []FileShareItem{},
		}, nil
	}

	// 4. Return updated MultiShareResult
	publicURL := a.tunnelManager.GetURL()
	var fileItems []FileShareItem
	var totalBytes int64
	for _, f := range a.activeFiles {
		totalBytes += f.RawBytes
		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: fmt.Sprintf("%s/%s", publicURL, f.ID),
		})
	}

	return &MultiShareResult{
		PublicBaseURL: publicURL,
		BundleURL:     fmt.Sprintf("%s/%s", publicURL, a.bundleCode),
		Files:         fileItems,
		LocalURLs:     getLocalIPs(a.server.Port()),
		TotalSize:     formatBytes(totalBytes),
	}, nil
}

// StopShare terminates the active sharing session
func (a *App) StopShare() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server != nil {
		a.server.Stop()
		a.server = nil
	}

	a.tunnelManager.Stop()

	a.activeFiles = nil
	a.bundleCode = ""
	a.usedCodes = nil

	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "share_stopped", true)
	}

	return nil
}

// CopyToClipboard copies text to the system clipboard
func (a *App) CopyToClipboard(text string) error {
	return wailsRuntime.ClipboardSetText(a.ctx, text)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getLocalIPs(port int) []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ips = append(ips, fmt.Sprintf("http://%s:%d", ip.String(), port))
			}
		}
	}
	return ips
}
