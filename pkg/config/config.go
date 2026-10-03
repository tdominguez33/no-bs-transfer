package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Network mode constants
const (
	NetworkModeBoth    = "both"     // Red local + Internet
	NetworkModeLANOnly = "lan_only" // Solo red local
)

// AppConfig stores user preferences and custom domain tunnel settings
type AppConfig struct {
	NetworkMode     string `json:"networkMode"`
	UseCustomDomain bool   `json:"useCustomDomain"`
	CustomDomain    string `json:"customDomain"`
	TunnelToken     string `json:"tunnelToken"`
	FixedPort       int    `json:"fixedPort"`
	SavePortable    bool   `json:"savePortable"`
	DownloadDir     string `json:"downloadDir"`
	WindowWidth     int    `json:"windowWidth,omitempty"`
	WindowHeight    int    `json:"windowHeight,omitempty"`
}

var (
	configMu       sync.RWMutex
	overrideMu     sync.RWMutex
	exeDirOverride string

	markerNames = []string{
		"portable.txt",
		"portable.txt.txt",
		"portable",
		"PORTABLE.TXT",
		"PORTABLE",
	}
)

// SetExeDirOverride allows overriding the executable directory (used for testing)
func SetExeDirOverride(dir string) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	exeDirOverride = dir
}

// GetExeDir returns the directory containing the running executable
func GetExeDir() (string, error) {
	overrideMu.RLock()
	override := exeDirOverride
	overrideMu.RUnlock()

	if override != "" {
		return override, nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return filepath.Dir(exePath), nil
}

func hasMarkerInDir(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range markerNames {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// HasPortableMarker checks if a portable marker file exists next to the executable
// or in the current working directory.
func HasPortableMarker() bool {
	dir, err := GetExeDir()
	if err == nil && hasMarkerInDir(dir) {
		return true
	}
	if wd, err := os.Getwd(); err == nil && hasMarkerInDir(wd) {
		return true
	}
	return false
}

// GetPortableDir returns the active directory for portable mode files
func GetPortableDir() string {
	dir, err := GetExeDir()
	if err == nil && hasMarkerInDir(dir) {
		return dir
	}
	if wd, err := os.Getwd(); err == nil && hasMarkerInDir(wd) {
		return wd
	}
	if err == nil {
		return dir
	}
	return "."
}

// GetLocalConfigPath returns the path to config.json for portable mode or next to executable
func GetLocalConfigPath() (string, error) {
	if HasPortableMarker() {
		return filepath.Join(GetPortableDir(), "config.json"), nil
	}
	dir, err := GetExeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// GetAppDataConfigPath returns the path to config.json in %LOCALAPPDATA% on Windows,
// or UserConfigDir on other platforms.
func GetAppDataConfigPath() string {
	if runtime.GOOS == "windows" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			return filepath.Join(localApp, "no-bs-transfer", "config.json")
		}
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	return filepath.Join(configDir, "no-bs-transfer", "config.json")
}

// CleanDomainHost strips http/https protocols and trailing paths, returning just the domain/host
func CleanDomainHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "https://") {
		raw = raw[8:]
	} else if strings.HasPrefix(lower, "http://") {
		raw = raw[7:]
	} else if strings.HasPrefix(lower, "https:/") {
		raw = raw[7:]
	} else if strings.HasPrefix(lower, "http:/") {
		raw = raw[6:]
	} else if strings.HasPrefix(lower, "https:") {
		raw = raw[6:]
	} else if strings.HasPrefix(lower, "http:") {
		raw = raw[5:]
	}
	raw = strings.TrimLeft(raw, "/")
	raw = strings.TrimRight(raw, "/")
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, "?"); idx != -1 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, "#"); idx != -1 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// DefaultDownloadDir returns the user's Downloads directory, or home directory as fallback
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}
	return home
}

// GetEffectiveDownloadDir returns the validated download directory
func GetEffectiveDownloadDir(cfg *AppConfig) string {
	if cfg != nil && cfg.DownloadDir != "" {
		if info, err := os.Stat(cfg.DownloadDir); err == nil && info.IsDir() {
			return cfg.DownloadDir
		}
	}
	return DefaultDownloadDir()
}

// Load reads the configuration from disk:
// 1. If portable mode is active (portable.txt marker), ONLY uses the portable folder and completely ignores %LOCALAPPDATA%.
// 2. If a local config.json exists next to executable, it reads it.
// 3. Otherwise, it uses %LOCALAPPDATA%\no-bs-transfer\config.json.
func Load() *AppConfig {
	configMu.RLock()
	defer configMu.RUnlock()

	defaultAppData := GetAppDataConfigPath()
	cfg := &AppConfig{
		NetworkMode:     NetworkModeBoth,
		UseCustomDomain: false,
		CustomDomain:    "",
		TunnelToken:     "",
		FixedPort:       56789,
		SavePortable:    false,
		DownloadDir:     DefaultDownloadDir(),
		WindowWidth:     960,
		WindowHeight:    720,
	}

	isPortable := HasPortableMarker()

	// 1. Check portable mode / config next to executable
	if localPath, err := GetLocalConfigPath(); err == nil {
		if data, err := os.ReadFile(localPath); err == nil {
			if err := json.Unmarshal(data, cfg); err == nil {
				cfg.SavePortable = true
				cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)
				if cfg.NetworkMode != NetworkModeLANOnly && cfg.NetworkMode != NetworkModeBoth {
					cfg.NetworkMode = NetworkModeBoth
				}
				if cfg.DownloadDir == "" {
					cfg.DownloadDir = DefaultDownloadDir()
				}
				if cfg.FixedPort <= 0 {
					cfg.FixedPort = 56789
				}
				if cfg.WindowWidth < 450 {
					cfg.WindowWidth = 960
				}
				if cfg.WindowHeight < 300 {
					cfg.WindowHeight = 720
				}
				return cfg
			}
		}
	}

	// In portable mode: if config.json does not exist yet in the portable folder,
	// return default configuration with SavePortable=true. Completely IGNORE %LOCALAPPDATA%.
	if isPortable {
		cfg.SavePortable = true
		return cfg
	}

	// 2. Default LocalAppData directory (%LOCALAPPDATA%\no-bs-transfer\config.json)
	if data, err := os.ReadFile(defaultAppData); err == nil {
		if err := json.Unmarshal(data, cfg); err == nil {
			cfg.SavePortable = false
			cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)
			if cfg.NetworkMode != NetworkModeLANOnly && cfg.NetworkMode != NetworkModeBoth {
				cfg.NetworkMode = NetworkModeBoth
			}
			if cfg.DownloadDir == "" {
				cfg.DownloadDir = DefaultDownloadDir()
			}
			if cfg.FixedPort <= 0 {
				cfg.FixedPort = 56789
			}
			if cfg.WindowWidth < 450 {
				cfg.WindowWidth = 960
			}
			if cfg.WindowHeight < 300 {
				cfg.WindowHeight = 720
			}
			return cfg
		}
	}

	return cfg
}

// Save writes the configuration to disk
func Save(cfg *AppConfig) error {
	configMu.Lock()
	defer configMu.Unlock()

	// Clean domain: strip protocol, paths, and trailing slashes so it's always stored without https://
	cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)

	if cfg.DownloadDir == "" {
		cfg.DownloadDir = DefaultDownloadDir()
	}

	if cfg.FixedPort <= 0 {
		cfg.FixedPort = 56789
	}

	if cfg.WindowWidth < 450 {
		cfg.WindowWidth = 960
	}
	if cfg.WindowHeight < 300 {
		cfg.WindowHeight = 720
	}

	var targetPath string
	localPath, localErr := GetLocalConfigPath()
	appDataPath := GetAppDataConfigPath()

	if HasPortableMarker() || (cfg.SavePortable && localErr == nil) {
		targetPath = localPath
		cfg.SavePortable = true
	} else {
		targetPath = appDataPath
		// If switching from portable to AppData, clean up local file if exists
		if localErr == nil {
			_ = os.Remove(localPath)
		}
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(targetPath, data, 0644)
}
