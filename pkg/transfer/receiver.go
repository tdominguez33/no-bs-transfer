package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ReceivedFile contains metadata of a successfully saved file
type ReceivedFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	RawBytes    int64  `json:"rawBytes"`
	SavedPath   string `json:"savedPath"`
	CompletedAt string `json:"completedAt"`
}

// ReceiveProgress reports live progress of an incoming file transfer
type ReceiveProgress struct {
	ID         string  `json:"id"`
	FileName   string  `json:"fileName"`
	BytesRecv  int64   `json:"bytesRecv"`
	TotalBytes int64   `json:"totalBytes"`
	Speed      float64 `json:"speed"`
	Progress   float64 `json:"progress"`
	Status     string  `json:"status"` // "uploading", "completed", "error"
	Error      string  `json:"error,omitempty"`
}

// ReceiveCallbacks defines event hooks for receiver activity
type ReceiveCallbacks struct {
	OnProgress func(p ReceiveProgress)
	OnFileDone func(f ReceivedFile)
}

// ReceiveServer hosts the upload endpoint and web interface for receiving files
type ReceiveServer struct {
	downloadDir string
	listener    net.Listener
	httpSrv     *http.Server
	port        int
	callbacks   ReceiveCallbacks
	mu          sync.RWMutex
	running     bool
}

// NewReceiveServer starts an HTTP server listening for file uploads
func NewReceiveServer(downloadDir string, fixedPort int, callbacks ReceiveCallbacks) (*ReceiveServer, error) {
	if downloadDir == "" {
		return nil, fmt.Errorf("carpeta de descargas no especificada")
	}

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return nil, fmt.Errorf("no se pudo crear la carpeta de descargas: %w", err)
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

	s := &ReceiveServer{
		downloadDir: downloadDir,
		listener:    listener,
		port:        port,
		callbacks:   callbacks,
		running:     true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRouter)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/api/status", s.handleStatus)

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

func (s *ReceiveServer) Port() int {
	return s.port
}

func (s *ReceiveServer) DownloadDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.downloadDir
}

func (s *ReceiveServer) SetDownloadDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("ruta de carpeta no válida")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	s.mu.Lock()
	s.downloadDir = dir
	s.mu.Unlock()
	return nil
}

func (s *ReceiveServer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.httpSrv.Shutdown(ctx); err != nil {
			_ = s.httpSrv.Close()
		}
	}
}

func (s *ReceiveServer) handleRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.setCORSHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	if path == "" || path == "upload" || path == "recibir" {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.handleUploadPage(w, r)
			return
		}
		if r.Method == http.MethodPost {
			s.handleUpload(w, r)
			return
		}
	}

	if r.Method == http.MethodGet {
		// Serve landing page for any friendly direct link
		s.handleUploadPage(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *ReceiveServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

func (s *ReceiveServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
		"mode":   "receive",
	})
}

func (s *ReceiveServer) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = receivePageTemplate.Execute(w, nil)
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

func generateReceiveFileID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rf_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func resolveUniqueFilename(targetDir, originalName string) string {
	cleanName := filepath.Base(originalName)
	cleanName = strings.ReplaceAll(cleanName, "\\", "_")
	cleanName = strings.ReplaceAll(cleanName, "/", "_")
	cleanName = strings.ReplaceAll(cleanName, "\x00", "")
	cleanName = strings.TrimSpace(cleanName)
	if cleanName == "" || cleanName == "." {
		cleanName = fmt.Sprintf("archivo_%d", time.Now().Unix())
	}

	destPath := filepath.Join(targetDir, cleanName)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		if _, errPart := os.Stat(destPath + ".part"); os.IsNotExist(errPart) {
			return destPath
		}
	}

	ext := filepath.Ext(cleanName)
	base := strings.TrimSuffix(cleanName, ext)
	counter := 1
	for {
		candidate := filepath.Join(targetDir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if _, errPart := os.Stat(candidate + ".part"); os.IsNotExist(errPart) {
				return candidate
			}
		}
		counter++
	}
}

// countingWriter wraps an io.Writer and emits speed and progress updates
type countingWriter struct {
	writer      io.Writer
	fileID      string
	fileName    string
	totalBytes  int64
	bytesRecv   int64
	lastEmit    time.Time
	lastBytes   int64
	onProgress  func(ReceiveProgress)
	emitLock    sync.Mutex
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.writer.Write(p)
	if n > 0 {
		cw.bytesRecv += int64(n)
		cw.emitLock.Lock()
		now := time.Now()
		elapsed := now.Sub(cw.lastEmit)
		if elapsed >= 200*time.Millisecond && cw.onProgress != nil {
			bytesDiff := cw.bytesRecv - cw.lastBytes
			speed := float64(bytesDiff) / elapsed.Seconds()
			var progress float64
			if cw.totalBytes > 0 {
				progress = (float64(cw.bytesRecv) / float64(cw.totalBytes)) * 100
				if progress > 99.9 {
					progress = 99.9
				}
			}

			cw.onProgress(ReceiveProgress{
				ID:         cw.fileID,
				FileName:   cw.fileName,
				BytesRecv:  cw.bytesRecv,
				TotalBytes: cw.totalBytes,
				Speed:      speed,
				Progress:   progress,
				Status:     "uploading",
			})
			cw.lastEmit = now
			cw.lastBytes = cw.bytesRecv
		}
		cw.emitLock.Unlock()
	}
	return n, err
}

func (s *ReceiveServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error al procesar formulario multipart: %v", err), http.StatusBadRequest)
		return
	}

	targetDir := s.DownloadDir()
	_ = os.MkdirAll(targetDir, 0755)

	var savedFiles []ReceivedFile

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Error al leer parte de archivo: %v", err), http.StatusBadRequest)
			return
		}

		filename := part.FileName()
		if filename == "" {
			// Skip non-file form fields
			_ = part.Close()
			continue
		}

		fileID := generateReceiveFileID()
		targetPath := resolveUniqueFilename(targetDir, filename)
		actualFileName := filepath.Base(targetPath)
		tempPath := targetPath + ".part"

		out, err := os.Create(tempPath)
		if err != nil {
			_ = part.Close()
			http.Error(w, fmt.Sprintf("Error al crear archivo de destino: %v", err), http.StatusInternalServerError)
			return
		}

		totalEstimated := r.ContentLength
		cw := &countingWriter{
			writer:     out,
			fileID:     fileID,
			fileName:   actualFileName,
			totalBytes: totalEstimated,
			lastEmit:   time.Now(),
			onProgress: s.callbacks.OnProgress,
		}

		if s.callbacks.OnProgress != nil {
			s.callbacks.OnProgress(ReceiveProgress{
				ID:         fileID,
				FileName:   actualFileName,
				BytesRecv:  0,
				TotalBytes: totalEstimated,
				Speed:      0,
				Progress:   0,
				Status:     "uploading",
			})
		}

		copied, copyErr := io.Copy(cw, part)
		closeErr := out.Close()
		_ = part.Close()

		if copyErr == nil && closeErr != nil {
			copyErr = closeErr
		}

		if copyErr != nil {
			_ = os.Remove(tempPath)
			if s.callbacks.OnProgress != nil {
				s.callbacks.OnProgress(ReceiveProgress{
					ID:       fileID,
					FileName: actualFileName,
					Status:   "error",
					Error:    copyErr.Error(),
				})
			}
			http.Error(w, fmt.Sprintf("Error al guardar archivo: %v", copyErr), http.StatusInternalServerError)
			return
		}

		// Rename from temporary .part to final destination
		if err := os.Rename(tempPath, targetPath); err != nil {
			_ = os.Remove(tempPath)
			if s.callbacks.OnProgress != nil {
				s.callbacks.OnProgress(ReceiveProgress{
					ID:       fileID,
					FileName: actualFileName,
					Status:   "error",
					Error:    err.Error(),
				})
			}
			http.Error(w, fmt.Sprintf("Error al finalizar archivo: %v", err), http.StatusInternalServerError)
			return
		}

		item := ReceivedFile{
			ID:          fileID,
			Name:        actualFileName,
			Size:        formatBytes(copied),
			RawBytes:    copied,
			SavedPath:   targetPath,
			CompletedAt: time.Now().Format("15:04:05"),
		}

		if s.callbacks.OnProgress != nil {
			s.callbacks.OnProgress(ReceiveProgress{
				ID:         fileID,
				FileName:   actualFileName,
				BytesRecv:  copied,
				TotalBytes: copied,
				Speed:      0,
				Progress:   100,
				Status:     "completed",
			})
		}

		if s.callbacks.OnFileDone != nil {
			s.callbacks.OnFileDone(item)
		}

		savedFiles = append(savedFiles, item)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"files":   savedFiles,
	})
}

var receivePageTemplate = template.Must(template.New("receive").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
  <title>Subir archivos - no-bs-transfer</title>
  <style>
    :root {
      --bg: #09090b;
      --card: #18181b;
      --card-hover: #27272a;
      --border: #27272a;
      --text: #fafafa;
      --muted: #a1a1aa;
      --accent: #2563eb;
      --accent-hover: #1d4ed8;
      --success: #10b981;
      --error: #ef4444;
    }
    @media (prefers-color-scheme: light) {
      :root {
        --bg: #f8fafc;
        --card: #ffffff;
        --card-hover: #f1f5f9;
        --border: #e2e8f0;
        --text: #0f172a;
        --muted: #64748b;
        --accent: #2563eb;
        --accent-hover: #1d4ed8;
        --success: #059669;
        --error: #dc2626;
      }
    }
    * { box-sizing: border-box; margin: 0; padding: 0; -webkit-tap-highlight-color: transparent; }
    html {
      height: 100%;
    }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      min-height: 100%;
      min-height: 100vh;
      min-height: 100dvh;
      display: flex;
      flex-direction: column;
      align-items: center;
      padding: 1.25rem 1rem;
      box-sizing: border-box;
      overflow-y: auto;
      overflow-x: hidden;
      overscroll-behavior-y: contain;
    }
    .container {
      max-width: 520px;
      width: 100%;
      margin: auto 0;
      display: flex;
      flex-direction: column;
      gap: 1.25rem;
    }
    .header {
      text-align: center;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
      font-size: 0.75rem;
      font-weight: 500;
      color: var(--success);
      background: rgba(16, 185, 129, 0.1);
      border: 1px solid rgba(16, 185, 129, 0.2);
      border-radius: 9999px;
      padding: 0.2rem 0.65rem;
      margin-bottom: 0.75rem;
    }
    .badge-dot {
      width: 0.45rem;
      height: 0.45rem;
      background: var(--success);
      border-radius: 50%;
      animation: pulse 2s infinite;
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; transform: scale(1); }
      50% { opacity: 0.4; transform: scale(0.9); }
    }
    .title {
      font-size: 1.35rem;
      font-weight: 600;
      letter-spacing: -0.02em;
      margin-bottom: 0.35rem;
    }
    .subtitle {
      font-size: 0.85rem;
      color: var(--muted);
      line-height: 1.4;
    }
    .dropzone {
      background: var(--card);
      border: 2px dashed var(--border);
      border-radius: 1rem;
      padding: 2.25rem 1.5rem;
      text-align: center;
      cursor: pointer;
      transition: all 0.2s ease;
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 0.75rem;
    }
    .dropzone.dragover {
      border-color: var(--accent);
      background: rgba(37, 99, 235, 0.05);
      transform: scale(1.01);
    }
    .icon-wrapper {
      width: 3.5rem;
      height: 3.5rem;
      background: rgba(37, 99, 235, 0.1);
      color: var(--accent);
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      margin-bottom: 0.25rem;
    }
    .icon-wrapper svg {
      width: 1.75rem;
      height: 1.75rem;
    }
    .drop-text {
      font-size: 0.95rem;
      font-weight: 500;
    }
    .mobile-only {
      display: none;
    }
    .desktop-only {
      display: inline;
    }
    @media (hover: none), (pointer: coarse), (max-width: 640px) {
      .mobile-only {
        display: inline;
      }
      .desktop-only {
        display: none;
      }
    }
    body.is-mobile .mobile-only {
      display: inline !important;
    }
    body.is-mobile .desktop-only {
      display: none !important;
    }
    .drop-hint {
      font-size: 0.8rem;
      color: var(--muted);
    }
    .btn-select {
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
      background: var(--accent);
      color: #ffffff;
      font-size: 0.85rem;
      font-weight: 500;
      padding: 0.6rem 1.1rem;
      border-radius: 0.5rem;
      border: none;
      cursor: pointer;
      transition: background 0.15s ease;
      margin-top: 0.25rem;
    }
    .btn-select:hover {
      background: var(--accent-hover);
    }
    .files-list {
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
    }
    .files-list:empty {
      display: none;
    }
    @media (max-width: 640px) {
      body {
        padding: 0.75rem 0.85rem;
      }
      .container {
        gap: 0.85rem;
      }
      .header {
        margin-bottom: 0;
      }
      .badge {
        margin-bottom: 0.4rem;
        padding: 0.15rem 0.55rem;
        font-size: 0.7rem;
      }
      .title {
        font-size: 1.2rem;
        margin-bottom: 0.25rem;
      }
      .subtitle {
        font-size: 0.8rem;
        line-height: 1.35;
      }
      .dropzone {
        padding: 1.25rem 1rem;
        gap: 0.5rem;
        border-radius: 0.85rem;
      }
      .icon-wrapper {
        width: 2.75rem;
        height: 2.75rem;
        margin-bottom: 0;
      }
      .icon-wrapper svg {
        width: 1.35rem;
        height: 1.35rem;
      }
      .drop-text {
        font-size: 0.9rem;
      }
      .drop-hint {
        font-size: 0.75rem;
      }
      .btn-select {
        padding: 0.5rem 1rem;
        font-size: 0.8rem;
        margin-top: 0.15rem;
      }
      .file-card {
        padding: 0.75rem 0.85rem;
      }
    }
    .file-card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      padding: 0.85rem 1rem;
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
    }
    .file-meta {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 0.75rem;
      font-size: 0.85rem;
    }
    .file-name {
      font-weight: 500;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      flex: 1;
    }
    .file-info {
      font-size: 0.75rem;
      color: var(--muted);
      font-family: monospace;
      white-space: nowrap;
    }
    .progress-bar-bg {
      width: 100%;
      height: 0.35rem;
      background: rgba(161, 161, 170, 0.2);
      border-radius: 9999px;
      overflow: hidden;
    }
    .progress-bar-fill {
      height: 100%;
      background: var(--accent);
      border-radius: 9999px;
      transition: width 0.15s ease;
    }
    .progress-bar-fill.done {
      background: var(--success);
    }
    .progress-bar-fill.error {
      background: var(--error);
    }
    .file-status-text {
      display: flex;
      justify-content: space-between;
      font-size: 0.75rem;
      color: var(--muted);
    }
    .status-badge {
      font-weight: 500;
    }
    .status-badge.done {
      color: var(--success);
    }
    .status-badge.error {
      color: var(--error);
    }
    .actions-footer {
      display: flex;
      justify-content: center;
      gap: 0.75rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <div class="badge">
        <span class="badge-dot"></span>
        <span>Receptor conectado</span>
      </div>
      <h1 class="title">Enviar archivos a tu PC</h1>
      <p class="subtitle">Carga fotos, videos o documentos desde este dispositivo para recibirlos de inmediato.</p>
    </div>

    <!-- Hidden native file input -->
    <input type="file" id="fileInput" multiple style="position:absolute; opacity:0; width:0; height:0; pointer-events:none;">

    <!-- Drop Zone -->
    <div class="dropzone" id="dropzone">
      <div class="icon-wrapper">
        <svg fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
        </svg>
      </div>
      <div class="drop-text">
        <span class="desktop-only">Arrastra archivos aquí o haz clic para seleccionar</span>
        <span class="mobile-only">Toca aquí para cargar archivos</span>
      </div>
      <div class="drop-hint">Puedes seleccionar varios archivos a la vez</div>
      <button type="button" class="btn-select" id="btnBrowse">
        <span class="desktop-only">Seleccionar archivos</span>
        <span class="mobile-only">Cargar archivos</span>
      </button>
    </div>

    <!-- Active Uploads List -->
    <div class="files-list" id="filesList"></div>
  </div>

  <script>
    const isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent) ||
      (window.matchMedia && (window.matchMedia('(hover: none)').matches || window.matchMedia('(pointer: coarse)').matches || window.matchMedia('(max-width: 640px)').matches));
    if (isMobile) {
      document.body.classList.add('is-mobile');
    }

    const fileInput = document.getElementById('fileInput');
    const dropzone = document.getElementById('dropzone');
    const btnBrowse = document.getElementById('btnBrowse');
    const filesList = document.getElementById('filesList');

    btnBrowse.addEventListener('click', (e) => {
      e.stopPropagation();
      fileInput.click();
    });

    dropzone.addEventListener('click', () => {
      fileInput.click();
    });

    ['dragenter', 'dragover'].forEach(name => {
      dropzone.addEventListener(name, (e) => {
        e.preventDefault();
        dropzone.classList.add('dragover');
      });
    });

    ['dragleave', 'drop'].forEach(name => {
      dropzone.addEventListener(name, (e) => {
        e.preventDefault();
        dropzone.classList.remove('dragover');
      });
    });

    dropzone.addEventListener('drop', (e) => {
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
        uploadFiles(Array.from(e.dataTransfer.files));
      }
    });

    fileInput.addEventListener('change', () => {
      if (fileInput.files && fileInput.files.length > 0) {
        uploadFiles(Array.from(fileInput.files));
        fileInput.value = '';
      }
    });

    function formatBytes(bytes) {
      if (bytes === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    function formatSpeed(bytesPerSec) {
      return formatBytes(bytesPerSec) + '/s';
    }

    function uploadFiles(files) {
      files.forEach(file => {
        uploadSingleFile(file);
      });
    }

    function uploadSingleFile(file) {
      const card = document.createElement('div');
      card.className = 'file-card';

      const fileId = 'f_' + Math.random().toString(36).slice(2);

      card.innerHTML = 
        '<div class="file-meta">' +
          '<span class="file-name" id="fn_' + fileId + '"></span>' +
          '<span class="file-info">' + formatBytes(file.size) + '</span>' +
        '</div>' +
        '<div class="progress-bar-bg">' +
          '<div class="progress-bar-fill" id="pb_' + fileId + '" style="width: 0%;"></div>' +
        '</div>' +
        '<div class="file-status-text">' +
          '<span class="status-badge" id="st_' + fileId + '">Subiendo...</span>' +
          '<span id="sp_' + fileId + '">0%</span>' +
        '</div>';

      card.querySelector('#fn_' + fileId).textContent = file.name;

      filesList.prepend(card);

      const pb = document.getElementById('pb_' + fileId);
      const st = document.getElementById('st_' + fileId);
      const sp = document.getElementById('sp_' + fileId);

      const formData = new FormData();
      formData.append('file', file);

      const xhr = new XMLHttpRequest();
      xhr.open('POST', '/upload', true);

      let startTime = Date.now();
      let lastLoaded = 0;

      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
          const percent = Math.min(100, Math.round((e.loaded / e.total) * 100));
          pb.style.width = percent + '%';
          
          const now = Date.now();
          const timeDiff = (now - startTime) / 1000;
          let speedStr = '';
          if (timeDiff > 0.3) {
            const speed = e.loaded / timeDiff;
            speedStr = ' · ' + formatSpeed(speed);
          }
          sp.textContent = percent + '%' + speedStr;
        }
      });

      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          pb.style.width = '100%';
          pb.classList.add('done');
          st.textContent = '✓ Completado';
          st.className = 'status-badge done';
          sp.textContent = '100%';
        } else {
          pb.classList.add('error');
          st.textContent = 'Error al subir';
          st.className = 'status-badge error';
          sp.textContent = xhr.statusText || 'Error de red';
        }
      });

      xhr.addEventListener('error', () => {
        pb.classList.add('error');
        st.textContent = 'Fallo de conexión';
        st.className = 'status-badge error';
        sp.textContent = '';
      });

      xhr.send(formData);
    }
  </script>
</body>
</html>`))
