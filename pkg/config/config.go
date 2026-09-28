package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AppConfig stores user preferences and custom domain tunnel settings
type AppConfig struct {
	UseCustomDomain bool   `json:"useCustomDomain"`
	CustomDomain    string `json:"customDomain"`
	TunnelToken     string `json:"tunnelToken"`
	FixedPort       int    `json:"fixedPort"`
	SavePortable    bool   `json:"savePortable"`
	WindowWidth     int    `json:"windowWidth,omitempty"`
	WindowHeight    int    `json:"windowHeight,omitempty"`
}

var mu sync.RWMutex

// GetLocalConfigPath returns the path to config.json next to the executable
func GetLocalConfigPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), "config.json"), nil
}

// GetAppDataConfigPath returns the path to config.json in %APPDATA%
func GetAppDataConfigPath() string {
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
	return strings.TrimSpace(raw)
}

// Load reads the configuration from disk: first executable dir, then %APPDATA%
func Load() *AppConfig {
	mu.RLock()
	defer mu.RUnlock()

	defaultAppData := GetAppDataConfigPath()
	cfg := &AppConfig{
		UseCustomDomain: false,
		CustomDomain:    "",
		TunnelToken:     "",
		FixedPort:       56789,
		SavePortable:    false,
		WindowWidth:     960,
		WindowHeight:    720,
	}

	// 1. Check portable mode: next to executable
	if localPath, err := GetLocalConfigPath(); err == nil {
		if data, err := os.ReadFile(localPath); err == nil {
			if err := json.Unmarshal(data, cfg); err == nil {
				cfg.SavePortable = true
				cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)
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

	// 2. Default OS User Config directory (%APPDATA% on Windows, ~/.config on Linux)
	if data, err := os.ReadFile(defaultAppData); err == nil {
		if err := json.Unmarshal(data, cfg); err == nil {
			cfg.SavePortable = false
			cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)
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
	mu.Lock()
	defer mu.Unlock()

	// Clean domain: strip protocol, paths, and trailing slashes so it's always stored without https://
	cfg.CustomDomain = CleanDomainHost(cfg.CustomDomain)

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

	if cfg.SavePortable && localErr == nil {
		targetPath = localPath
	} else {
		targetPath = appDataPath
		// If migrating from portable to AppData, clean up local file if exists
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
