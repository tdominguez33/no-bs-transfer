package tunnel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"no-bs-transfer/pkg/config"
)

// DownloadProgress reports progress for the cloudflared download
type DownloadProgress struct {
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Percent    float64 `json:"percent"`
	Speed      int64   `json:"speed"`
	Done       bool    `json:"done"`
	Error      string  `json:"error,omitempty"`
}

// DefaultDownloadDir returns the directory where cloudflared is stored
func DefaultDownloadDir() string {
	if config.HasPortableMarker() {
		return filepath.Join(config.GetPortableDir(), "bin")
	}

	if runtime.GOOS == "windows" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			return filepath.Join(localApp, "no-bs-transfer", "bin")
		}
	}

	if userCfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(userCfg, "no-bs-transfer", "bin")
	}

	return filepath.Join(os.TempDir(), "no-bs-transfer", "bin")
}

// DefaultBinaryPath returns the expected executable path in DefaultDownloadDir
func DefaultBinaryPath() string {
	name := "cloudflared.exe"
	if runtime.GOOS != "windows" {
		name = "cloudflared"
	}
	return filepath.Join(DefaultDownloadDir(), name)
}

// GetCloudflaredDownloadURL returns the official Cloudflare release URL for current OS/Arch
func GetCloudflaredDownloadURL() (string, error) {
	base := "https://github.com/cloudflare/cloudflared/releases/latest/download"
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return base + "/cloudflared-windows-amd64.exe", nil
		case "386":
			return base + "/cloudflared-windows-386.exe", nil
		case "arm64":
			return base + "/cloudflared-windows-arm64.exe", nil
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return base + "/cloudflared-linux-amd64", nil
		case "arm64":
			return base + "/cloudflared-linux-arm64", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return base + "/cloudflared-darwin-arm64", nil
		case "amd64":
			return base + "/cloudflared-darwin-amd64", nil
		}
	}
	return "", fmt.Errorf("sistema/arquitectura no soportado para descarga automática: %s/%s", runtime.GOOS, runtime.GOARCH)
}

// DownloadCloudflared downloads cloudflared executable from official releases
func DownloadCloudflared(ctx context.Context, onProgress func(p DownloadProgress)) (string, error) {
	url, err := GetCloudflaredDownloadURL()
	if err != nil {
		return "", err
	}

	destDir := DefaultDownloadDir()
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("no se pudo crear la carpeta de destino: %w", err)
	}

	finalPath := DefaultBinaryPath()
	tempPath := finalPath + ".tmp"
	_ = os.Remove(tempPath)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("error al crear petición de descarga: %w", err)
	}
	req.Header.Set("User-Agent", "no-bs-transfer-downloader")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error al conectar con servidor de descarga: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("servidor devolvió código HTTP %d al descargar", resp.StatusCode)
	}

	totalBytes := resp.ContentLength

	out, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return "", fmt.Errorf("no se pudo crear archivo temporal: %w", err)
	}

	cleanedUp := false
	cleanup := func() {
		if !cleanedUp {
			cleanedUp = true
			_ = out.Close()
			_ = os.Remove(tempPath)
		}
	}
	defer cleanup()

	buf := make([]byte, 64*1024)
	var downloadedBytes int64
	lastReportTime := time.Now()
	var lastDownloadedBytes int64

	// Emit initial progress
	if onProgress != nil {
		onProgress(DownloadProgress{
			Downloaded: 0,
			Total:      totalBytes,
			Percent:    0,
			Speed:      0,
			Done:       false,
		})
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			nw, werr := out.Write(buf[0:nr])
			if werr != nil {
				return "", fmt.Errorf("error al escribir en disco: %w", werr)
			}
			if nr != nw {
				return "", fmt.Errorf("escritura corta de archivo: escribió %d de %d bytes", nw, nr)
			}
			downloadedBytes += int64(nw)

			now := time.Now()
			timeDiff := now.Sub(lastReportTime)
			if onProgress != nil && (timeDiff >= 100*time.Millisecond || rerr == io.EOF) {
				var percent float64
				if totalBytes > 0 {
					percent = (float64(downloadedBytes) / float64(totalBytes)) * 100
					if percent > 100 {
						percent = 100
					}
				}
				var speed int64
				if timeDiff > 0 {
					speed = int64(float64(downloadedBytes-lastDownloadedBytes) / timeDiff.Seconds())
				}
				lastReportTime = now
				lastDownloadedBytes = downloadedBytes

				onProgress(DownloadProgress{
					Downloaded: downloadedBytes,
					Total:      totalBytes,
					Percent:    percent,
					Speed:      speed,
					Done:       false,
				})
			}
		}

		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", fmt.Errorf("error durante la descarga: %w", rerr)
		}
	}

	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("error al sincronizar archivo en disco: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("error al cerrar archivo: %w", err)
	}

	// Rename temp file to final file
	_ = os.Remove(finalPath)
	if err := os.Rename(tempPath, finalPath); err != nil {
		if copyErr := copyFile(tempPath, finalPath); copyErr != nil {
			return "", fmt.Errorf("error al mover binario final: %w", copyErr)
		}
		_ = os.Remove(tempPath)
	}

	cleanedUp = true

	if runtime.GOOS != "windows" {
		_ = os.Chmod(finalPath, 0755)
	}

	if onProgress != nil {
		onProgress(DownloadProgress{
			Downloaded: downloadedBytes,
			Total:      downloadedBytes,
			Percent:    100,
			Speed:      0,
			Done:       true,
		})
	}

	return finalPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
