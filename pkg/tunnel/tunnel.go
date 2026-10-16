package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"no-bs-transfer/pkg/config"
)

// CheckInternet verifies if there is active internet connectivity
// by attempting fast concurrent HTTP probes against standard endpoints
// (Cloudflare, Google, Microsoft) and fallback DNS resolution.
// Does NOT use port 53 dials which are falsely answered by offline local routers.
func CheckInternet() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	endpoints := []string{
		"http://cp.cloudflare.com/generate_204",
		"http://connectivitycheck.gstatic.com/generate_204",
		"http://www.msftconnecttest.com/connecttest.txt",
	}

	tr := &http.Transport{
		DisableKeepAlives: true,
		Proxy:             http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 1000 * time.Millisecond,
		}).DialContext,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   1100 * time.Millisecond,
	}

	success := make(chan struct{}, 1)
	done := make(chan struct{})
	var wg sync.WaitGroup

	for _, ep := range endpoints {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == 200 || resp.StatusCode == 204 {
					select {
					case success <- struct{}{}:
					default:
					}
				}
			}
		}(ep)
	}

	// Fallback: fast DNS lookup of Cloudflare and TCP 443 check
	wg.Add(1)
	go func() {
		defer wg.Done()
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", "cloudflare.com")
		if err == nil && len(ips) > 0 {
			var d net.Dialer
			d.Timeout = 1000 * time.Millisecond
			conn, err := d.DialContext(ctx, "tcp", "1.1.1.1:443")
			if err == nil {
				_ = conn.Close()
				select {
				case success <- struct{}{}:
				default:
				}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-success:
		return true
	case <-done:
		return false
	case <-ctx.Done():
		return false
	}
}

// Standard tunnel error codes for frontend internationalization
const (
	ErrNoInternet               = "ERR_NO_INTERNET"
	ErrInvalidTunnelToken       = "ERR_INVALID_TUNNEL_TOKEN"
	ErrTunnelRateLimited        = "ERR_TUNNEL_RATE_LIMITED"
	ErrPortInUse                = "ERR_PORT_IN_USE"
	ErrCloudflaredNotFound      = "ERR_CLOUDFLARED_NOT_FOUND"
	ErrTunnelTimeout            = "ERR_TUNNEL_TIMEOUT"
	ErrCustomDomainTunnelFailed = "ERR_CUSTOM_DOMAIN_TUNNEL_FAILED"
	ErrTunnelFailed             = "ERR_TUNNEL_FAILED"
)

// FormatTunnelError translates raw cloudflared or networking errors into
// standardized error codes for the frontend i18n dictionary.
func FormatTunnelError(err error, isCustomDomain bool) error {
	if err == nil {
		return nil
	}

	errMsg := err.Error()
	errLower := strings.ToLower(errMsg)

	// 1. Connectivity / DNS / Offline issues
	if strings.Contains(errLower, "err_no_internet") ||
		strings.Contains(errLower, "dnsquery") ||
		strings.Contains(errLower, "argotunnel.com") ||
		strings.Contains(errLower, "lookup") ||
		strings.Contains(errLower, "no such host") ||
		strings.Contains(errLower, "network is unreachable") ||
		strings.Contains(errLower, "network unreachable") ||
		strings.Contains(errLower, "connect: connection refused") ||
		strings.Contains(errLower, "se agotó el tiempo de espera") ||
		strings.Contains(errLower, "no hay conexión a internet") ||
		strings.Contains(errLower, "failed to connect to edge") ||
		strings.Contains(errLower, "could not lookup srv records") ||
		strings.Contains(errLower, "dial tcp") ||
		strings.Contains(errLower, "i/o timeout") ||
		strings.Contains(errLower, "quic: timeout") {
		return fmt.Errorf(ErrNoInternet)
	}

	// 2. Custom Domain / Token validation issues
	if isCustomDomain && (strings.Contains(errLower, "err_invalid_tunnel_token") ||
		strings.Contains(errLower, "token") ||
		strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "credentials") ||
		strings.Contains(errLower, "auth") ||
		strings.Contains(errLower, "invalid") ||
		strings.Contains(errLower, "cannot determine default configuration path")) {
		return fmt.Errorf(ErrInvalidTunnelToken)
	}

	// 3. Quick tunnel service busy / rate limited
	if strings.Contains(errLower, "err_tunnel_rate_limited") ||
		strings.Contains(errLower, "rate limit") ||
		strings.Contains(errLower, "trycloudflare.com") ||
		strings.Contains(errLower, "503") ||
		strings.Contains(errLower, "quick tunnel") {
		return fmt.Errorf(ErrTunnelRateLimited)
	}

	// 4. Port collision
	if strings.Contains(errLower, "err_port_in_use") ||
		strings.Contains(errLower, "address already in use") ||
		strings.Contains(errLower, "already in use") ||
		strings.Contains(errLower, "only one usage of each socket address") ||
		strings.Contains(errLower, "solo se permite un uso de cada dirección") ||
		strings.Contains(errLower, "bind") {
		return fmt.Errorf(ErrPortInUse)
	}

	// 5. Binary execution / installation issues
	if strings.Contains(errLower, "err_cloudflared_not_found") ||
		strings.Contains(errLower, "cloudflared no encontrado") ||
		strings.Contains(errLower, "executable file not found") ||
		strings.Contains(errLower, "permission denied") {
		return fmt.Errorf(ErrCloudflaredNotFound)
	}

	// 6. Generic timeout
	if strings.Contains(errLower, "err_tunnel_timeout") ||
		strings.Contains(errLower, "tiempo de espera agotado") ||
		strings.Contains(errLower, "timeout") {
		return fmt.Errorf(ErrTunnelTimeout)
	}

	// Fallback
	if isCustomDomain {
		return fmt.Errorf(ErrCustomDomainTunnelFailed)
	}
	return fmt.Errorf(ErrTunnelFailed)
}

var tunnelRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// isValidTunnelURL checks if the detected URL is a legitimate ephemeral tunnel endpoint
// and not Cloudflare's management API URL (api.trycloudflare.com).
func isValidTunnelURL(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "https://api.trycloudflare.com") || strings.HasPrefix(u, "http://api.trycloudflare.com") {
		return false
	}
	return true
}

// fileExists checks if a file exists, is not a directory, and has non-zero size
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Manager handles the cloudflared process lifecycle
type Manager struct {
	cmd              *exec.Cmd
	cancel           context.CancelFunc
	attemptCancel    context.CancelFunc
	attemptMu        sync.Mutex
	publicURL        string
	waitChan         chan struct{}
	mu               sync.Mutex
	running          bool
	onUnexpectedExit func()
}

// NewManager creates a new tunnel manager
func NewManager() *Manager {
	return &Manager{}
}

// SetOnUnexpectedExit registers a callback invoked if cloudflared terminates unexpectedly mid-session
func (m *Manager) SetOnUnexpectedExit(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUnexpectedExit = fn
}

func (m *Manager) setAttemptCancel(cancel context.CancelFunc) {
	m.attemptMu.Lock()
	m.attemptCancel = cancel
	m.attemptMu.Unlock()
}

// Abort immediately cancels any in-flight tunnel connection attempt
func (m *Manager) Abort() {
	m.attemptMu.Lock()
	cancel := m.attemptCancel
	m.attemptCancel = nil
	m.attemptMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func hasDevFlag(flagName, envName string) bool {
	if os.Getenv(envName) == "1" || strings.EqualFold(os.Getenv(envName), "true") {
		return true
	}
	for _, arg := range os.Args {
		clean := strings.TrimLeft(arg, "-")
		val := ""
		if idx := strings.Index(clean, "="); idx != -1 {
			val = clean[idx+1:]
			clean = clean[:idx]
		}
		if strings.EqualFold(clean, flagName) {
			if val == "" || val == "1" || strings.EqualFold(val, "true") {
				return true
			}
			return false
		}
	}
	return false
}

// ShouldForceHTTP2 checks if HTTP/2 protocol is forced via CLI flag (--force-http2 / --http2)
// or environment variable (NO_BS_FORCE_HTTP2 / NO_BS_HTTP2).
func ShouldForceHTTP2() bool {
	return hasDevFlag("force-http2", "NO_BS_FORCE_HTTP2") || hasDevFlag("http2", "NO_BS_HTTP2")
}

// FindCloudflared locates the cloudflared executable on the system,
// prioritizing the version installed by the user on their machine.
func FindCloudflared() (string, error) {
	binName := "cloudflared"
	if runtime.GOOS == "windows" {
		binName = "cloudflared.exe"
	}

	// 0. Portable mode via portable.txt marker:
	// In portable mode, we search ONLY in the portable folder (prioritizing bin/ subfolder).
	// We ignore all local machine settings and installations.
	if config.HasPortableMarker() {
		dir := config.GetPortableDir()
		p := filepath.Join(dir, "bin", binName)
		if fileExists(p) {
			return p, nil
		}
		p = filepath.Join(dir, binName)
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("cloudflared no encontrado en la carpeta bin (modo portable)")
	}

	// Dev flag: ignore system/local installations and look ONLY in AppData download folder
	ignoreLocalInstall := hasDevFlag("ignore-local-install", "NO_BS_IGNORE_LOCAL_INSTALL")

	if !ignoreLocalInstall {
		// 1. Prioritize user's system PATH
		if path, err := exec.LookPath(binName); err == nil {
			if fileExists(path) {
				return path, nil
			}
		}

		// 2. Prioritize common user/system installation paths (WinGet, Program Files, Chocolatey, Scoop)
		if runtime.GOOS == "windows" {
			var commonPaths []string

			// Program Files
			commonPaths = append(commonPaths,
				`C:\Program Files\cloudflared\cloudflared.exe`,
				`C:\Program Files (x86)\cloudflared\cloudflared.exe`,
			)

			// WinGet (Links shim and Packages folder with source suffix)
			if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
				commonPaths = append(commonPaths,
					filepath.Join(localApp, `Microsoft\WinGet\Links\cloudflared.exe`),
					filepath.Join(localApp, `Microsoft\WinGet\Packages\Cloudflare.cloudflared\cloudflared.exe`),
				)
				if matches, err := filepath.Glob(filepath.Join(localApp, `Microsoft\WinGet\Packages\Cloudflare.cloudflared*\cloudflared.exe`)); err == nil {
					commonPaths = append(commonPaths, matches...)
				}
			}

			// Chocolatey
			if progData := os.Getenv("PROGRAMDATA"); progData != "" {
				commonPaths = append(commonPaths,
					filepath.Join(progData, `chocolatey\bin\cloudflared.exe`),
				)
			}

			// Scoop
			if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
				commonPaths = append(commonPaths,
					filepath.Join(userProfile, `scoop\shims\cloudflared.exe`),
				)
			}

			for _, p := range commonPaths {
				if fileExists(p) {
					return p, nil
				}
			}
		}

		// 3. Portable folder next to running executable
		if dir, err := config.GetExeDir(); err == nil {
			p := filepath.Join(dir, binName)
			if fileExists(p) {
				return p, nil
			}
			p = filepath.Join(dir, "bin", binName)
			if fileExists(p) {
				return p, nil
			}
		}
	}

	// 4. App internal auto-download cache (%LOCALAPPDATA%\no-bs-transfer\bin\cloudflared.exe)
	defaultPath := DefaultBinaryPath()
	if fileExists(defaultPath) {
		return defaultPath, nil
	}

	return "", fmt.Errorf("cloudflared no encontrado en el sistema")
}

// startAttempt tries to launch cloudflared with the specified CLI arguments.
func (m *Manager) startAttempt(binPath string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m.setAttemptCancel(cancel)
	defer m.setAttemptCancel(nil)

	cmd := exec.CommandContext(ctx, binPath, args...)
	prepareCmdPlatform(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("error al conectar stderr de cloudflared: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("error al iniciar cloudflared: %w", err)
	}

	waitChan := make(chan struct{})
	urlChan := make(chan string, 1)
	errChan := make(chan error, 1)

	var lastErrLine string
	var errMu sync.Mutex

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			errMu.Lock()
			lastErrLine = line
			errMu.Unlock()

			if match := tunnelRegex.FindString(line); match != "" && isValidTunnelURL(match) {
				select {
				case urlChan <- match:
				default:
				}
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	go func() {
		_ = cmd.Wait()
		close(waitChan)
	}()

	select {
	case url := <-urlChan:
		m.cmd = cmd
		m.cancel = cancel
		m.running = true
		m.waitChan = waitChan
		m.publicURL = url
		go func(currentCmd *exec.Cmd) {
			<-waitChan
			var unexpected bool
			var cb func()
			m.mu.Lock()
			if m.cmd == currentCmd {
				m.running = false
				m.publicURL = ""
				m.cmd = nil
				unexpected = true
				cb = m.onUnexpectedExit
			}
			m.mu.Unlock()
			if unexpected && cb != nil {
				cb()
			}
		}(cmd)
		return url, nil
	case err := <-errChan:
		cancel()
		if cmd.Process != nil {
			killProcessTree(cmd.Process)
		}
		select {
		case <-waitChan:
		case <-time.After(1 * time.Second):
		}
		return "", fmt.Errorf("fallo al leer logs de cloudflared: %w", err)
	case <-waitChan:
		cancel()
		errMu.Lock()
		detail := lastErrLine
		errMu.Unlock()
		if detail != "" {
			return "", fmt.Errorf("cloudflared se cerró inesperadamente: %s", detail)
		}
		return "", fmt.Errorf("cloudflared se cerró inesperadamente")
	case <-time.After(timeout):
		cancel()
		if cmd.Process != nil {
			killProcessTree(cmd.Process)
		}
		select {
		case <-waitChan:
		case <-time.After(1 * time.Second):
		}
		return "", fmt.Errorf("tiempo de espera agotado esperando la URL de Cloudflare")
	}
}

// Start launches a quick tunnel pointing to the given local port.
// If standard QUIC protocol fails or is blocked, it retries automatically using HTTP/2 over TCP.
// When ShouldForceHTTP2() is active, it directly connects via HTTP/2 without trying QUIC first.
func (m *Manager) Start(port int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return m.publicURL, nil
	}

	binPath, err := FindCloudflared()
	if err != nil {
		return "", err
	}

	targetURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	if ShouldForceHTTP2() {
		url, err := m.startAttempt(binPath, []string{"tunnel", "--protocol", "http2", "--url", targetURL, "--no-autoupdate"}, 20*time.Second)
		if err != nil {
			return "", fmt.Errorf("no se pudo establecer el túnel forzando HTTP/2. Intenta usar el enlace de red local (IP). Detalle: %w", err)
		}
		return url, nil
	}

	// Attempt 1: Standard Quick Tunnel (QUIC / UDP)
	url, err := m.startAttempt(binPath, []string{"tunnel", "--url", targetURL, "--no-autoupdate"}, 10*time.Second)
	if err == nil {
		return url, nil
	}

	// Attempt 2: Corporate Firewall Fallback using HTTP/2 over TCP (port 443)
	url, err2 := m.startAttempt(binPath, []string{"tunnel", "--protocol", "http2", "--url", targetURL, "--no-autoupdate"}, 12*time.Second)
	if err2 == nil {
		return url, nil
	}

	return "", fmt.Errorf("no se pudo establecer el túnel de Cloudflare (QUIC: %v; HTTP/2: %w). Intenta usar el enlace de red local (IP)", err, err2)
}

// Stop shuts down the cloudflared process
func (m *Manager) Stop() {
	m.Abort()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if !m.running {
		return
	}

	cmd := m.cmd
	waitChan := m.waitChan
	cancel := m.cancel

	m.cmd = nil
	m.cancel = nil
	m.waitChan = nil
	m.running = false
	m.publicURL = ""

	if cancel != nil {
		cancel()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	if waitChan != nil {
		select {
		case <-waitChan:
		case <-time.After(2 * time.Second):
			if cmd != nil && cmd.Process != nil {
				killProcessTree(cmd.Process)
			}
		}
	}
}

// GetURL returns the active public URL if running
func (m *Manager) GetURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publicURL
}

// IsRunning checks if the tunnel is currently active
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// StartWithToken launches a named tunnel using a Cloudflare Tunnel Token
func (m *Manager) StartWithToken(token, publicDomain string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return m.publicURL, nil
	}

	binPath, err := FindCloudflared()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.setAttemptCancel(cancel)
	defer m.setAttemptCancel(nil)

	args := []string{"tunnel", "run", "--token", token}
	if ShouldForceHTTP2() {
		args = []string{"tunnel", "run", "--protocol", "http2", "--token", token}
	}
	cmd := exec.CommandContext(ctx, binPath, args...)
	prepareCmdPlatform(cmd)
	m.cmd = cmd

	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.cmd = nil
		m.cancel = nil
		cancel()
		return "", fmt.Errorf("error al conectar stderr de cloudflared: %w", err)
	}

	if err := cmd.Start(); err != nil {
		m.cmd = nil
		m.cancel = nil
		cancel()
		return "", fmt.Errorf("error al ejecutar cloudflared (dominio propio): %w", err)
	}

	m.running = true
	m.publicURL = FormatPublicURL(publicDomain)
	waitChan := make(chan struct{})
	m.waitChan = waitChan

	var lastErrLine string
	var errMu sync.Mutex
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			txt := scanner.Text()
			errMu.Lock()
			lastErrLine = txt
			errMu.Unlock()
		}
		if err := scanner.Err(); err != nil {
			errMu.Lock()
			if lastErrLine == "" {
				lastErrLine = fmt.Sprintf("error al leer logs: %v", err)
			}
			errMu.Unlock()
		}
	}()

	var waitErr error
	go func(currentCmd *exec.Cmd) {
		waitErr = currentCmd.Wait()
		close(waitChan)
		var unexpected bool
		var cb func()
		m.mu.Lock()
		if m.cmd == currentCmd {
			m.running = false
			m.publicURL = ""
			m.cmd = nil
			unexpected = true
			cb = m.onUnexpectedExit
		}
		m.mu.Unlock()
		if unexpected && cb != nil {
			cb()
		}
	}(cmd)

	select {
	case <-waitChan:
		m.stopLocked()
		errMu.Lock()
		detail := lastErrLine
		errMu.Unlock()
		if detail != "" {
			return "", fmt.Errorf("cloudflared se detuvo: %s", detail)
		}
		if waitErr != nil {
			return "", fmt.Errorf("cloudflared se detuvo: %w", waitErr)
		}
		return "", fmt.Errorf("cloudflared se detuvo inesperadamente")
	case <-time.After(2 * time.Second):
		return m.publicURL, nil
	}
}

// FormatPublicURL standardizes a public domain or URL to https://domain
func FormatPublicURL(rawDomain string) string {
	domain := strings.TrimSpace(rawDomain)
	if domain == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(domain), "http://") {
		domain = "https://" + domain[7:]
	} else if !strings.HasPrefix(strings.ToLower(domain), "https://") {
		domain = "https://" + domain
	}
	domain = strings.TrimRight(domain, "/")
	if idx := strings.Index(domain[8:], "/"); idx != -1 {
		domain = domain[:8+idx]
	}
	return domain
}
