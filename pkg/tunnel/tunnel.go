package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var tunnelRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

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

// FindCloudflared locates the cloudflared executable on the system
func FindCloudflared() (string, error) {
	// 1. Check PATH
	if path, err := exec.LookPath("cloudflared"); err == nil {
		return path, nil
	}

	// 2. Common Windows paths
	commonPaths := []string{
		`C:\Program Files (x86)\cloudflared\cloudflared.exe`,
		`C:\Program Files\cloudflared\cloudflared.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\WinGet\Packages\Cloudflare.cloudflared\cloudflared.exe`),
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("cloudflared no encontrado en PATH ni en rutas estándar")
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
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

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
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
			// Safety fallback for Windows if process tree lingers
			if m.cmd != nil && m.cmd.Process != nil {
				_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", m.cmd.Process.Pid)).Run()
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
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
