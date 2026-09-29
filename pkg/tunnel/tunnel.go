package tunnel

import (
	"bufio"
	"context"
	"fmt"
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

var tunnelRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// fileExists checks if a file exists, is not a directory, and has non-zero size
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Manager handles the cloudflared process lifecycle
type Manager struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	publicURL string
	waitChan  chan struct{}
	mu        sync.Mutex
	running   bool
}

// NewManager creates a new tunnel manager
func NewManager() *Manager {
	return &Manager{}
}

func hasDevFlag(flagName, envName string) bool {
	if os.Getenv(envName) == "1" || strings.EqualFold(os.Getenv(envName), "true") {
		return true
	}
	for _, arg := range os.Args {
		clean := strings.TrimLeft(arg, "-")
		if idx := strings.Index(clean, "="); idx != -1 {
			clean = clean[:idx]
		}
		if strings.EqualFold(clean, flagName) {
			return true
		}
	}
	return false
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

			// WinGet
			if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
				commonPaths = append(commonPaths,
					filepath.Join(localApp, `Microsoft\WinGet\Packages\Cloudflare.cloudflared\cloudflared.exe`),
				)
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

// Start launches a quick tunnel pointing to the given local port
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

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	cmd := exec.CommandContext(ctx, binPath, "tunnel", "--url", fmt.Sprintf("http://127.0.0.1:%d", port), "--no-autoupdate")
	prepareCmdPlatform(cmd)
	m.cmd = cmd

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("error al conectar stderr de cloudflared: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("error al iniciar cloudflared: %w", err)
	}

	m.running = true
	waitChan := make(chan struct{})
	m.waitChan = waitChan

	// Channel to receive the detected tunnel URL
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

			if match := tunnelRegex.FindString(line); match != "" {
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

	// Single goroutine waits for the command to terminate
	go func() {
		_ = cmd.Wait()
		close(waitChan)
		m.mu.Lock()
		m.running = false
		m.publicURL = ""
		m.mu.Unlock()
	}()

	// Wait up to 30 seconds for Cloudflare to assign an ephemeral URL
	select {
	case url := <-urlChan:
		m.publicURL = url
		return url, nil
	case err := <-errChan:
		m.stopLocked()
		return "", fmt.Errorf("fallo al leer logs de cloudflared: %w", err)
	case <-waitChan:
		m.stopLocked()
		errMu.Lock()
		detail := lastErrLine
		errMu.Unlock()
		if detail != "" {
			return "", fmt.Errorf("cloudflared se cerró inesperadamente: %s", detail)
		}
		return "", fmt.Errorf("cloudflared se cerró inesperadamente")
	case <-time.After(30 * time.Second):
		m.stopLocked()
		return "", fmt.Errorf("tiempo de espera agotado esperando la URL de Cloudflare")
	}
}

// Stop shuts down the cloudflared process
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if !m.running {
		return
	}

	if m.cancel != nil {
		m.cancel()
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}

	waitChan := m.waitChan
	m.running = false
	m.publicURL = ""

	if waitChan != nil {
		select {
		case <-waitChan:
		case <-time.After(2 * time.Second):
			if m.cmd != nil && m.cmd.Process != nil {
				killProcessTree(m.cmd.Process)
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

	cmd := exec.CommandContext(ctx, binPath, "tunnel", "run", "--token", token)
	prepareCmdPlatform(cmd)
	m.cmd = cmd

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("error al conectar stderr de cloudflared: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("error al iniciar túnel con token: %w", err)
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
	}()

	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(waitChan)
		m.mu.Lock()
		m.running = false
		m.publicURL = ""
		m.mu.Unlock()
	}()

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
