package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SharedFile represents a local file exposed for transfer
type SharedFile struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     string `json:"size"`
	RawBytes int64  `json:"rawBytes"`
}

// Server handles distribution of one or multiple files
type Server struct {
	files           map[string]*SharedFile
	fileList        []*SharedFile
	bundleCode      string
	listener        net.Listener
	httpSrv         *http.Server
	tracker         *Tracker
	port            int
	mu              sync.Mutex
	running         bool
	activeDownloads map[string]map[int64]context.CancelFunc
	nextDownloadID  int64
}

// NewServer initializes a transfer server for a list of files
func NewServer(files []*SharedFile, bundleCode string, fixedPort int, onUpdate func(stats TransferStats)) (*Server, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no se especificaron archivos para compartir")
	}

	listenAddr := "127.0.0.1:0"
	if fixedPort > 0 {
		listenAddr = fmt.Sprintf("127.0.0.1:%d", fixedPort)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("error al abrir puerto local (%s): %w", listenAddr, err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	tracker := NewTracker(onUpdate)

	fileMap := make(map[string]*SharedFile)
	for _, f := range files {
		fileMap[f.ID] = f
	}

	s := &Server{
		files:           fileMap,
		fileList:        files,
		bundleCode:      bundleCode,
		listener:        listener,
		tracker:         tracker,
		port:            port,
		running:         true,
		activeDownloads: make(map[string]map[int64]context.CancelFunc),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRouter)

	s.httpSrv = &http.Server{
		Handler:      mux,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		_ = s.httpSrv.Serve(listener)
	}()

	return s, nil
}

func (s *Server) Port() int {
	return s.port
}

// AddFiles dynamically registers new files to an active server
func (s *Server) AddFiles(newFiles []*SharedFile) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, f := range newFiles {
		s.files[f.ID] = f
		s.fileList = append(s.fileList, f)
	}
}

func (s *Server) registerDownload(fileID string, cancel context.CancelFunc) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return 0, false
	}

	s.nextDownloadID++
	id := s.nextDownloadID
	if s.activeDownloads == nil {
		s.activeDownloads = make(map[string]map[int64]context.CancelFunc)
	}
	if s.activeDownloads[fileID] == nil {
		s.activeDownloads[fileID] = make(map[int64]context.CancelFunc)
	}
	s.activeDownloads[fileID][id] = cancel
	return id, true
}

func (s *Server) unregisterDownload(fileID string, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeDownloads != nil && s.activeDownloads[fileID] != nil {
		delete(s.activeDownloads[fileID], id)
		if len(s.activeDownloads[fileID]) == 0 {
			delete(s.activeDownloads, fileID)
		}
	}
}

// RemoveFile removes a file from the server and cancels any ongoing downloads of it
func (s *Server) RemoveFile(fileID string) {
	s.mu.Lock()
	delete(s.files, fileID)

	newFileList := make([]*SharedFile, 0, len(s.fileList))
	for _, f := range s.fileList {
		if f.ID != fileID {
			newFileList = append(newFileList, f)
		}
	}
	s.fileList = newFileList

	var cancels []context.CancelFunc
	if s.activeDownloads != nil && s.activeDownloads[fileID] != nil {
		for _, cancel := range s.activeDownloads[fileID] {
			cancels = append(cancels, cancel)
		}
		delete(s.activeDownloads, fileID)
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	s.tracker.RemoveFileTransfers(fileID)
}

func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}

	var cancels []context.CancelFunc
	for _, m := range s.activeDownloads {
		for _, cancel := range m {
			cancels = append(cancels, cancel)
		}
	}
	s.activeDownloads = nil
	s.running = false
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	s.tracker.Stop()
	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.httpSrv.Shutdown(ctx); err != nil {
			_ = s.httpSrv.Close()
		}
	}
}

func isValidGuestID(val string) bool {
	if len(val) == 0 || len(val) > 32 {
		return false
	}
	for i := 0; i < len(val); i++ {
		c := val[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Server) getGuestID(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie("nbs_guest")
	if err == nil && isValidGuestID(cookie.Value) {
		return cookie.Value
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     "nbs_guest",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(48 * time.Hour),
	})

	return id
}

type countingReadSeeker struct {
	rs      io.ReadSeeker
	key     string
	tracker *Tracker
	ctx     context.Context
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, errors.New("descarga cancelada: archivo eliminado de la sesión")
	default:
	}
	n, err := c.rs.Read(p)
	if n > 0 {
		c.tracker.AddBytes(c.key, int64(n))
	}
	return n, err
}

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	select {
	case <-c.ctx.Done():
		return 0, errors.New("descarga cancelada: archivo eliminado de la sesión")
	default:
	}
	return c.rs.Seek(offset, whence)
}

// handleRouter routes /{code} to bundle landing (if bundleCode) or directly to file download (if file ID), and hides root /
func (s *Server) handleRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	if s.bundleCode != "" && (path == s.bundleCode || path == "b/"+s.bundleCode) {
		s.handleLanding(w, r)
		return
	}

	s.mu.Lock()
	fileInfo, exists := s.files[path]
	s.mu.Unlock()

	if exists {
		s.serveFile(w, r, fileInfo)
		return
	}

	http.NotFound(w, r)
}

// parseRangeStart extracts the beginning byte offset from an HTTP Range header.
// Supports standard ranges ("bytes=123-"), full ranges ("bytes=123-456"), and suffix ranges ("bytes=-100").
// Returns 0 if missing, invalid, or starting from the beginning.
func parseRangeStart(rangeHeader string, fileSize int64) int64 {
	if rangeHeader == "" {
		return 0
	}
	parts := strings.Split(rangeHeader, "=")
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "bytes") {
		return 0
	}
	ranges := strings.Split(parts[1], ",")
	firstRange := strings.TrimSpace(ranges[0])
	rangeParts := strings.Split(firstRange, "-")
	if len(rangeParts) < 2 {
		return 0
	}
	startStr := strings.TrimSpace(rangeParts[0])
	endStr := strings.TrimSpace(rangeParts[1])

	if startStr == "" {
		// Suffix range: bytes=-500 (meaning last 500 bytes)
		if endStr != "" && fileSize > 0 {
			suffixLen, err := strconv.ParseInt(endStr, 10, 64)
			if err == nil && suffixLen > 0 {
				start := fileSize - suffixLen
				if start < 0 {
					return 0
				}
				return start
			}
		}
		return 0
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0
	}
	return start
}

// serveFile streams the requested file with Range requests and tracking
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, fileInfo *SharedFile) {
	file, err := os.Open(fileInfo.Path)
	if err != nil {
		http.Error(w, "Archivo no disponible", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		http.Error(w, "Error al leer archivo", http.StatusInternalServerError)
		return
	}

	escapedName := url.PathEscape(fileInfo.Name)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fileInfo.Name, escapedName))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// If HEAD request, serve headers only without creating a transfer record
	if r.Method == http.MethodHead {
		http.ServeContent(w, r, fileInfo.Name, fi.ModTime(), file)
		return
	}

	dlCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	dlID, ok := s.registerDownload(fileInfo.ID, cancel)
	if !ok {
		http.Error(w, "Servidor deteniéndose", http.StatusServiceUnavailable)
		return
	}
	defer s.unregisterDownload(fileInfo.ID, dlID)

	r = r.WithContext(dlCtx)

	guestID := s.getGuestID(w, r)
	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("CF-Connecting-IP"); forwarded != "" {
		clientIP = strings.TrimSpace(forwarded)
	} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		clientIP = strings.TrimSpace(parts[0])
	}
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	startOffset := parseRangeStart(r.Header.Get("Range"), fi.Size())
	gt := s.tracker.StartTransfer(guestID, fileInfo.ID, fileInfo.Name, fi.Size(), clientIP, r.UserAgent(), startOffset)
	defer s.tracker.OnConnectionClosed(gt.ID)

	countingSeeker := &countingReadSeeker{
		rs:      file,
		key:     gt.ID,
		tracker: s.tracker,
		ctx:     dlCtx,
	}

	http.ServeContent(w, r, fileInfo.Name, fi.ModTime(), countingSeeker)
}

// handleLanding renders the clean minimalist download page listing all shared files
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	s.getGuestID(w, r)

	s.mu.Lock()
	filesCopy := make([]*SharedFile, len(s.fileList))
	copy(filesCopy, s.fileList)
	s.mu.Unlock()

	data := struct {
		Files []*SharedFile
	}{
		Files: filesCopy,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = landingTemplate.Execute(w, data)
}

var landingTemplate = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Descarga de archivos</title>
  <style>
    :root {
      --bg: #09090b;
      --card: #18181b;
      --border: #27272a;
      --text: #fafafa;
      --muted: #a1a1aa;
      --accent: #2563eb;
      --accent-hover: #1d4ed8;
    }
    @media (prefers-color-scheme: light) {
      :root {
        --bg: #f8fafc;
        --card: #ffffff;
        --border: #e2e8f0;
        --text: #0f172a;
        --muted: #64748b;
        --accent: #2563eb;
        --accent-hover: #1d4ed8;
      }
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 1.5rem;
    }
    .container {
      max-width: 560px;
      width: 100%;
    }
    .header {
      margin-bottom: 1.5rem;
      text-align: left;
    }
    .title {
      font-size: 1.25rem;
      font-weight: 600;
      letter-spacing: -0.02em;
      margin-bottom: 0.25rem;
    }
    .subtitle {
      font-size: 0.875rem;
      color: var(--muted);
    }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      overflow: hidden;
      box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1);
    }
    .file-item {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 1rem;
      padding: 1rem 1.25rem;
      border-bottom: 1px solid var(--border);
    }
    .file-item:last-child {
      border-bottom: none;
    }
    .file-info {
      min-width: 0;
      flex: 1;
    }
    .file-name {
      font-size: 0.9375rem;
      font-weight: 500;
      word-break: break-all;
      margin-bottom: 0.2rem;
    }
    .file-size {
      font-size: 0.8125rem;
      color: var(--muted);
      font-family: monospace;
    }
    .btn {
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
      background: var(--accent);
      color: #ffffff;
      font-size: 0.8125rem;
      font-weight: 500;
      padding: 0.5rem 0.875rem;
      border-radius: 0.5rem;
      text-decoration: none;
      transition: background 0.15s ease;
      white-space: nowrap;
    }
    .btn:hover {
      background: var(--accent-hover);
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <h1 class="title">Archivos disponibles</h1>
      <p class="subtitle">Haz clic en descargar para transferir directamente desde el anfitrión.</p>
    </div>

    <div class="card">
      {{range .Files}}
      <div class="file-item">
        <div class="file-info">
          <div class="file-name">{{.Name}}</div>
          <div class="file-size">{{.Size}}</div>
        </div>
        <a href="/{{.ID}}" class="btn" download>
          Descargar
        </a>
      </div>
      {{end}}
    </div>
  </div>
</body>
</html>`))
