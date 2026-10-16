package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"no-bs-transfer/pkg/config"
	"no-bs-transfer/pkg/transfer"
	"no-bs-transfer/pkg/tunnel"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	ErrDownloadCancelled         = "ERR_DOWNLOAD_CANCELLED"
	ErrCloudflaredDownloadFailed = "ERR_CLOUDFLARED_DOWNLOAD_FAILED"
	ErrNoFilesToShare            = "ERR_NO_FILES_TO_SHARE"
	ErrServerStartFailed         = "ERR_SERVER_START_FAILED"
	ErrReceiveServerStartFailed  = "ERR_RECEIVE_SERVER_START_FAILED"
	ErrNoActiveSession           = "ERR_NO_ACTIVE_SESSION"
	ErrInvalidNetworkMode        = "ERR_INVALID_NETWORK_MODE"
	ErrDownloadDirAccess         = "ERR_DOWNLOAD_DIR_ACCESS"
	ErrFileNotFound              = "ERR_FILE_NOT_FOUND"
)

// FileShareItem represents an individual file with its direct download link
type FileShareItem struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      string `json:"size"`
	DirectURL string `json:"directUrl"`
	LocalURL  string `json:"localUrl"`
}

// MultiShareResult contains both the bundle URL and individual direct links
type MultiShareResult struct {
	PublicBaseURL  string          `json:"publicBaseUrl"`
	BundleURL      string          `json:"bundleUrl"`
	LocalBundleURL string          `json:"localBundleUrl"`
	Files          []FileShareItem `json:"files"`
	LocalURLs      []string        `json:"localUrls"`
	TotalSize      string          `json:"totalSize"`
	NetworkMode    string          `json:"networkMode"`
}

// ReceiveResult contains the public upload URL, local addresses, and destination directory
type ReceiveResult struct {
	PublicURL      string   `json:"publicUrl"`
	UploadURL      string   `json:"uploadUrl"`
	LocalUploadURL string   `json:"localUploadUrl"`
	LocalURLs      []string `json:"localUrls"`
	DownloadDir    string   `json:"downloadDir"`
	NetworkMode    string   `json:"networkMode"`
}

// CloudflaredStatus represents the installation status of cloudflared
type CloudflaredStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
}

// App struct
type App struct {
	ctx            context.Context
	tunnelManager  *tunnel.Manager
	server         *transfer.Server
	receiveServer  *transfer.ReceiveServer
	isReceiving    bool
	mu             sync.Mutex
	activeFiles    []*transfer.SharedFile
	bundleCode     string
	usedCodes      map[string]bool
	downloadCancel context.CancelFunc
	downloadMu     sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	tm := tunnel.NewManager()
	app := &App{
		tunnelManager: tm,
	}
	tm.SetOnUnexpectedExit(func() {
		app.mu.Lock()
		wasSharing := app.server != nil
		wasReceiving := app.receiveServer != nil
		app.mu.Unlock()

		if wasSharing {
			_ = app.StopShare()
		}
		if wasReceiving {
			_ = app.StopReceive()
		}
		if app.ctx != nil && (wasSharing || wasReceiving) {
			wailsRuntime.EventsEmit(app.ctx, "tunnel_unexpected_exit", tunnel.ErrTunnelFailed)
		}
	})
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.SaveWindowDimensions()
	return false
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.StopShare()
	_ = a.StopReceive()
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

// GetCloudflaredStatus returns whether cloudflared is installed and its binary path
func (a *App) GetCloudflaredStatus() *CloudflaredStatus {
	path, err := tunnel.FindCloudflared()
	if err != nil {
		return &CloudflaredStatus{
			Installed: false,
			Path:      "",
		}
	}
	return &CloudflaredStatus{
		Installed: true,
		Path:      path,
	}
}

// CancelCloudflaredDownload cancels any ongoing cloudflared download
func (a *App) CancelCloudflaredDownload() {
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if a.downloadCancel != nil {
		a.downloadCancel()
		a.downloadCancel = nil
	}
}

// EnsureCloudflared verifies if cloudflared is available; if not, downloads it with progress updates
func (a *App) EnsureCloudflared() (string, error) {
	if path, err := tunnel.FindCloudflared(); err == nil {
		return path, nil
	}

	a.downloadMu.Lock()
	if a.downloadCancel != nil {
		a.downloadCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.downloadCancel = cancel
	a.downloadMu.Unlock()

	defer func() {
		a.downloadMu.Lock()
		a.downloadCancel = nil
		a.downloadMu.Unlock()
	}()

	path, err := tunnel.DownloadCloudflared(ctx, func(p tunnel.DownloadProgress) {
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "cloudflared_download_progress", p)
		}
	})

	if err != nil {
		errCode := ErrCloudflaredDownloadFailed
		if errors.Is(err, context.Canceled) {
			errCode = ErrDownloadCancelled
		}
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "cloudflared_download_progress", tunnel.DownloadProgress{
				Done:  true,
				Error: errCode,
			})
		}
		return "", errors.New(errCode)
	}

	return path, nil
}

// DownloadCloudflared manually initiates the cloudflared download (e.g. from Settings)
func (a *App) DownloadCloudflared() error {
	_, err := a.EnsureCloudflared()
	return err
}

// CheckInternetAccess verifies active internet connection
func (a *App) CheckInternetAccess() bool {
	return tunnel.CheckInternet()
}

// SelectFiles opens native Windows file dialog allowing multiple selection
func (a *App) SelectFiles() ([]*transfer.SharedFile, error) {
	title := "Select one or more files"
	if a.GetConfig().EffectiveLanguage() == config.LanguageES {
		title = "Seleccionar uno o más archivos"
	}
	filePaths, err := wailsRuntime.OpenMultipleFilesDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return nil, fmt.Errorf("error opening file picker: %w", err)
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

func mapServerBindError(err error, fallbackCode string) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "address already in use") ||
		strings.Contains(lower, "only one usage of each socket address") ||
		strings.Contains(lower, "bind:") {
		return errors.New(tunnel.ErrPortInUse)
	}
	return errors.New(fallbackCode)
}

// StartShare initiates the local server and optionally Cloudflare tunnel for multiple files
func (a *App) StartShare(files []*transfer.SharedFile) (*MultiShareResult, error) {
	seenPaths := make(map[string]bool)
	var uniqueFiles []*transfer.SharedFile
	for _, f := range files {
		if f != nil && f.Path != "" && !seenPaths[f.Path] {
			seenPaths[f.Path] = true
			uniqueFiles = append(uniqueFiles, f)
		}
	}
	if len(uniqueFiles) == 0 {
		return nil, errors.New(ErrNoFilesToShare)
	}
	files = uniqueFiles

	cfg := config.Load()
	networkMode := cfg.NetworkMode
	if networkMode != config.NetworkModeLANOnly && networkMode != config.NetworkModeBoth {
		networkMode = config.NetworkModeBoth
	}

	// 1. Ensure internet connection & cloudflared are ready only if we need Internet tunnel
	if networkMode == config.NetworkModeBoth {
		if !tunnel.CheckInternet() {
			return nil, errors.New(tunnel.ErrNoInternet)
		}
		if _, err := a.EnsureCloudflared(); err != nil {
			return nil, tunnel.FormatTunnelError(err, false)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Stop any existing share or receive
	if a.server != nil {
		a.server.Stop()
		a.server = nil
	}
	if a.receiveServer != nil {
		a.receiveServer.Stop()
		a.receiveServer = nil
	}
	a.isReceiving = false
	a.tunnelManager.Stop()

	// Assign unique 6-digit random codes to each file and bundle
	a.usedCodes = make(map[string]bool)
	a.bundleCode = generate6DigitCode(a.usedCodes)
	for _, f := range files {
		f.ID = generate6DigitCode(a.usedCodes)
	}
	a.activeFiles = files

	// Only bind FixedPort when Internet tunnel with custom domain is active
	port := 0
	if networkMode == config.NetworkModeBoth && cfg.UseCustomDomain && cfg.FixedPort > 0 {
		port = cfg.FixedPort
	}

	// 2. Start HTTP Server with bundleCode
	srv, err := transfer.NewServer(files, a.bundleCode, port, func(stats transfer.TransferStats) {
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "transfer_update", stats)
		}
	})
	if err != nil {
		return nil, mapServerBindError(err, ErrServerStartFailed)
	}
	a.server = srv

	primaryLocal := getPrimaryLocalURL(srv.Port())
	localBundleURL := fmt.Sprintf("%s/%s", primaryLocal, a.bundleCode)
	localIPs := getLocalIPs(srv.Port())

	// 3. Start Cloudflare Tunnel if networkMode == "both"
	var publicURL string
	if networkMode == config.NetworkModeBoth {
		if cfg.UseCustomDomain && cfg.TunnelToken != "" && cfg.CustomDomain != "" {
			publicURL, err = a.tunnelManager.StartWithToken(cfg.TunnelToken, cfg.CustomDomain)
			if err != nil {
				srv.Stop()
				a.server = nil
				return nil, tunnel.FormatTunnelError(err, true)
			}
		} else {
			publicURL, err = a.tunnelManager.Start(srv.Port())
			if err != nil {
				srv.Stop()
				a.server = nil
				return nil, tunnel.FormatTunnelError(err, false)
			}
		}
	}

	// Build direct links per file
	var fileItems []FileShareItem
	for _, f := range files {
		directURL := ""
		if publicURL != "" {
			directURL = fmt.Sprintf("%s/%s", publicURL, f.ID)
		}
		localURL := fmt.Sprintf("%s/%s", primaryLocal, f.ID)

		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Path:      f.Path,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: directURL,
			LocalURL:  localURL,
		})
	}

	var totalBytes int64
	for _, f := range files {
		totalBytes += f.RawBytes
	}

	bundleURL := ""
	if publicURL != "" {
		bundleURL = fmt.Sprintf("%s/%s", publicURL, a.bundleCode)
	}

	return &MultiShareResult{
		PublicBaseURL:  publicURL,
		BundleURL:      bundleURL,
		LocalBundleURL: localBundleURL,
		Files:          fileItems,
		LocalURLs:      localIPs,
		TotalSize:      formatBytes(totalBytes),
		NetworkMode:    networkMode,
	}, nil
}

// AddFilesToShare dynamically adds new files to an already active sharing tunnel
func (a *App) AddFilesToShare(newFiles []*transfer.SharedFile) (*MultiShareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil {
		return nil, errors.New(ErrNoActiveSession)
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

	cfg := config.Load()
	networkMode := cfg.NetworkMode
	if networkMode != config.NetworkModeLANOnly && networkMode != config.NetworkModeBoth {
		networkMode = config.NetworkModeBoth
	}

	var publicURL string
	if a.tunnelManager.IsRunning() {
		publicURL = a.tunnelManager.GetURL()
	}

	primaryLocal := getPrimaryLocalURL(a.server.Port())
	localBundleURL := fmt.Sprintf("%s/%s", primaryLocal, a.bundleCode)

	var fileItems []FileShareItem
	var totalBytes int64
	for _, f := range a.activeFiles {
		totalBytes += f.RawBytes
		directURL := ""
		if publicURL != "" {
			directURL = fmt.Sprintf("%s/%s", publicURL, f.ID)
		}
		localURL := fmt.Sprintf("%s/%s", primaryLocal, f.ID)

		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Path:      f.Path,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: directURL,
			LocalURL:  localURL,
		})
	}

	bundleURL := ""
	if publicURL != "" {
		bundleURL = fmt.Sprintf("%s/%s", publicURL, a.bundleCode)
	}

	return &MultiShareResult{
		PublicBaseURL:  publicURL,
		BundleURL:      bundleURL,
		LocalBundleURL: localBundleURL,
		Files:          fileItems,
		LocalURLs:      getLocalIPs(a.server.Port()),
		TotalSize:      formatBytes(totalBytes),
		NetworkMode:    networkMode,
	}, nil
}

// RemoveFileFromShare removes an individual file from an active sharing session and cancels active downloads for it
func (a *App) RemoveFileFromShare(fileID string) (*MultiShareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil {
		return nil, errors.New(ErrNoActiveSession)
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

	cfg := config.Load()
	networkMode := cfg.NetworkMode
	if networkMode != config.NetworkModeLANOnly && networkMode != config.NetworkModeBoth {
		networkMode = config.NetworkModeBoth
	}

	var publicURL string
	if a.tunnelManager.IsRunning() {
		publicURL = a.tunnelManager.GetURL()
	}

	primaryLocal := getPrimaryLocalURL(a.server.Port())
	localBundleURL := fmt.Sprintf("%s/%s", primaryLocal, a.bundleCode)

	var fileItems []FileShareItem
	var totalBytes int64
	for _, f := range a.activeFiles {
		totalBytes += f.RawBytes
		directURL := ""
		if publicURL != "" {
			directURL = fmt.Sprintf("%s/%s", publicURL, f.ID)
		}
		localURL := fmt.Sprintf("%s/%s", primaryLocal, f.ID)

		fileItems = append(fileItems, FileShareItem{
			ID:        f.ID,
			Path:      f.Path,
			Name:      f.Name,
			Size:      f.Size,
			DirectURL: directURL,
			LocalURL:  localURL,
		})
	}

	bundleURL := ""
	if publicURL != "" {
		bundleURL = fmt.Sprintf("%s/%s", publicURL, a.bundleCode)
	}

	return &MultiShareResult{
		PublicBaseURL:  publicURL,
		BundleURL:      bundleURL,
		LocalBundleURL: localBundleURL,
		Files:          fileItems,
		LocalURLs:      getLocalIPs(a.server.Port()),
		TotalSize:      formatBytes(totalBytes),
		NetworkMode:    networkMode,
	}, nil
}

// StopShare terminates the active sharing session
func (a *App) StopShare() error {
	// Abort any in-flight tunnel startup before acquiring a.mu
	a.tunnelManager.Abort()

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

func detectOutboundIPv4() string {
	conn, err := net.DialTimeout("udp4", "8.8.8.8:80", 100*time.Millisecond)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok && udpAddr.IP != nil {
		if ip4 := udpAddr.IP.To4(); ip4 != nil && ip4.IsPrivate() && !ip4.IsLinkLocalUnicast() {
			return ip4.String()
		}
	}
	return ""
}

func getLocalIPs(port int) []string {
	var preferred []string
	var prioritized []string
	var secondary []string

	outboundIP := detectOutboundIPv4()

	ifaces, err := net.Interfaces()
	if err != nil {
		if outboundIP != "" {
			return []string{fmt.Sprintf("http://%s:%d", outboundIP, port)}
		}
		return []string{fmt.Sprintf("http://127.0.0.1:%d", port)}
	}

	for _, iface := range ifaces {
		// Must be up and not loopback
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		nameLower := strings.ToLower(iface.Name)
		isVirtual := strings.Contains(nameLower, "vethernet") ||
			strings.Contains(nameLower, "virtual") ||
			strings.Contains(nameLower, "vmware") ||
			strings.Contains(nameLower, "docker") ||
			strings.Contains(nameLower, "wsl") ||
			strings.Contains(nameLower, "hyper-v") ||
			strings.Contains(nameLower, "tailscale") ||
			strings.Contains(nameLower, "zerotier")

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
			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}

			// Exclude link-local / APIPA (169.254.x.x)
			if ip4.IsLinkLocalUnicast() {
				continue
			}

			// Only private IPs
			if ip4.IsPrivate() {
				ipStr := ip4.String()
				url := fmt.Sprintf("http://%s:%d", ipStr, port)
				// VirtualBox default Host-Only subnet is 192.168.56.x
				isVBoxSubnet := strings.HasPrefix(ipStr, "192.168.56.")
				if outboundIP != "" && ipStr == outboundIP && !isVirtual && !isVBoxSubnet {
					preferred = append(preferred, url)
				} else if isVirtual || isVBoxSubnet {
					secondary = append(secondary, url)
				} else {
					prioritized = append(prioritized, url)
				}
			}
		}
	}

	results := append(preferred, prioritized...)
	results = append(results, secondary...)
	if len(results) == 0 {
		return []string{fmt.Sprintf("http://127.0.0.1:%d", port)}
	}
	return results
}

func getPrimaryLocalURL(port int) string {
	ips := getLocalIPs(port)
	if len(ips) > 0 {
		return ips[0]
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// StartReceive initiates the local receiver server and optionally Cloudflare tunnel for incoming transfers
func (a *App) StartReceive() (*ReceiveResult, error) {
	cfg := config.Load()
	networkMode := cfg.NetworkMode
	if networkMode != config.NetworkModeLANOnly && networkMode != config.NetworkModeBoth {
		networkMode = config.NetworkModeBoth
	}

	// 1. Ensure internet connection & cloudflared are ready only if we need Internet tunnel
	if networkMode == config.NetworkModeBoth {
		if !tunnel.CheckInternet() {
			return nil, errors.New(tunnel.ErrNoInternet)
		}
		if _, err := a.EnsureCloudflared(); err != nil {
			return nil, tunnel.FormatTunnelError(err, false)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Stop any existing share or receive
	if a.server != nil {
		a.server.Stop()
		a.server = nil
	}
	if a.receiveServer != nil {
		a.receiveServer.Stop()
		a.receiveServer = nil
	}
	a.isReceiving = false
	a.tunnelManager.Stop()

	// Only bind FixedPort when Internet tunnel with custom domain is active
	port := 0
	if networkMode == config.NetworkModeBoth && cfg.UseCustomDomain && cfg.FixedPort > 0 {
		port = cfg.FixedPort
	}
	downloadDir := config.GetEffectiveDownloadDir(cfg)

	// 2. Start Receiver Server
	rcv, err := transfer.NewReceiveServer(downloadDir, port, transfer.ReceiveCallbacks{
		OnProgress: func(p transfer.ReceiveProgress) {
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "receive_file_progress", p)
			}
		},
		OnFileDone: func(f transfer.ReceivedFile) {
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "receive_file_completed", f)
			}
		},
	})
	if err != nil {
		return nil, mapServerBindError(err, ErrReceiveServerStartFailed)
	}
	a.receiveServer = rcv

	primaryLocal := getPrimaryLocalURL(rcv.Port())
	localIPs := getLocalIPs(rcv.Port())

	// 3. Start Cloudflare Tunnel if networkMode == "both"
	var publicURL string
	if networkMode == config.NetworkModeBoth {
		if cfg.UseCustomDomain && cfg.TunnelToken != "" && cfg.CustomDomain != "" {
			publicURL, err = a.tunnelManager.StartWithToken(cfg.TunnelToken, cfg.CustomDomain)
			if err != nil {
				rcv.Stop()
				a.receiveServer = nil
				return nil, tunnel.FormatTunnelError(err, true)
			}
		} else {
			publicURL, err = a.tunnelManager.Start(rcv.Port())
			if err != nil {
				rcv.Stop()
				a.receiveServer = nil
				return nil, tunnel.FormatTunnelError(err, false)
			}
		}
	}

	a.isReceiving = true

	uploadURL := ""
	if publicURL != "" {
		uploadURL = publicURL
	}

	return &ReceiveResult{
		PublicURL:      publicURL,
		UploadURL:      uploadURL,
		LocalUploadURL: primaryLocal,
		LocalURLs:      localIPs,
		DownloadDir:    downloadDir,
		NetworkMode:    networkMode,
	}, nil
}

// SetNetworkMode updates the network mode ("both" or "lan_only") in configuration
func (a *App) SetNetworkMode(mode string) error {
	if mode != config.NetworkModeBoth && mode != config.NetworkModeLANOnly {
		return errors.New(ErrInvalidNetworkMode)
	}
	cfg := config.Load()
	cfg.NetworkMode = mode
	return config.Save(cfg)
}

// GetNetworkMode returns the current network mode ("both" or "lan_only")
func (a *App) GetNetworkMode() string {
	cfg := config.Load()
	if cfg.NetworkMode != config.NetworkModeLANOnly && cfg.NetworkMode != config.NetworkModeBoth {
		return config.NetworkModeBoth
	}
	return cfg.NetworkMode
}

// StopReceive terminates the active receive session
func (a *App) StopReceive() error {
	// Abort any in-flight tunnel startup before acquiring a.mu
	a.tunnelManager.Abort()

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.receiveServer != nil {
		a.receiveServer.Stop()
		a.receiveServer = nil
	}

	a.tunnelManager.Stop()
	a.isReceiving = false

	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "receive_stopped", true)
	}

	return nil
}

// GetDownloadDir returns the active download folder path
func (a *App) GetDownloadDir() string {
	cfg := config.Load()
	return config.GetEffectiveDownloadDir(cfg)
}

// SetDownloadDir sets and saves a new download directory path
func (a *App) SetDownloadDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = config.DefaultDownloadDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.New(ErrDownloadDirAccess)
	}

	cfg := config.Load()
	cfg.DownloadDir = dir
	if err := config.Save(cfg); err != nil {
		return err
	}

	a.mu.Lock()
	if a.receiveServer != nil {
		_ = a.receiveServer.SetDownloadDir(dir)
	}
	a.mu.Unlock()

	return nil
}

// SelectDownloadDir opens native directory selection dialog and persists the choice
func (a *App) SelectDownloadDir() (string, error) {
	current := a.GetDownloadDir()
	title := "Select download folder"
	if a.GetConfig().EffectiveLanguage() == config.LanguageES {
		title = "Seleccionar carpeta de descargas"
	}
	chosen, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: current,
	})
	if err != nil {
		return "", fmt.Errorf("error opening folder picker: %w", err)
	}
	if chosen == "" {
		return current, nil
	}

	if err := a.SetDownloadDir(chosen); err != nil {
		return "", err
	}
	return chosen, nil
}

func startDetachedCmd(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// OpenDownloadDir opens the current download folder in the system file manager
func (a *App) OpenDownloadDir() error {
	dir := a.GetDownloadDir()
	_ = os.MkdirAll(dir, 0755)

	if runtime.GOOS == "windows" {
		return startDetachedCmd(exec.Command("explorer", filepath.Clean(dir)))
	} else if runtime.GOOS == "darwin" {
		return startDetachedCmd(exec.Command("open", dir))
	}
	return startDetachedCmd(exec.Command("xdg-open", dir))
}

// OpenFile opens a file with the system default program
func (a *App) OpenFile(filePath string) error {
	filePath = filepath.Clean(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return errors.New(ErrFileNotFound)
	}

	if runtime.GOOS == "windows" {
		return startDetachedCmd(exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", filePath))
	} else if runtime.GOOS == "darwin" {
		return startDetachedCmd(exec.Command("open", filePath))
	}
	return startDetachedCmd(exec.Command("xdg-open", filePath))
}

// ShowFileInFolder opens the system file manager and selects/highlights the file
func (a *App) ShowFileInFolder(filePath string) error {
	filePath = filepath.Clean(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return errors.New(ErrFileNotFound)
	}

	if runtime.GOOS == "windows" {
		return startDetachedCmd(exec.Command("explorer", fmt.Sprintf("/select,%s", filePath)))
	} else if runtime.GOOS == "darwin" {
		return startDetachedCmd(exec.Command("open", "-R", filePath))
	}
	dir := filepath.Dir(filePath)
	return startDetachedCmd(exec.Command("xdg-open", dir))
}

