package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Status     string  `json:"status"` // "uploading", "stalled", "completed", "cancelled", "error"
	Error      string  `json:"error,omitempty"`
}

// ReceiveCallbacks defines event hooks for receiver activity
type ReceiveCallbacks struct {
	OnProgress func(p ReceiveProgress)
	OnFileDone func(f ReceivedFile)
}

type chunkUploadState struct {
	mu         sync.Mutex
	fileID     string
	fileName   string
	targetPath string
	tempPath   string
	totalSize  int64
	bytesRecv  int64
	nextChunk  int
	lastEmit   time.Time
	// Speed reference point, set when the first chunk finishes. Tunnels (Cloudflare)
	// buffer each chunk and deliver it in a burst, so speed is only meaningful as an
	// average measured across whole chunk cycles, starting at a chunk boundary.
	rateStart time.Time
	rateBytes int64
	finished  bool // completed or aborted; guarded by mu

	clientSpeed atomic.Int64 // Speed in bytes/sec measured and reported by client
	// Liveness, read by the watchdog without mu (mu is held for a whole chunk copy).
	lastSeen  atomic.Int64 // unix nanos of the last data or heartbeat from the sender
	liveBytes atomic.Int64 // bytes written, including the in-flight chunk
	stalled   atomic.Bool
	// abortActive interrupts the in-flight chunk request, if any. Guarded by ReceiveServer.mu.
	abortActive func()
}

// Upload liveness thresholds. The sender page pings every few seconds while its
// upload is progressing; with tunnel buffering, chunk data itself can be silent
// for a long time, so heartbeats are the primary signal.
var (
	uploadStallAfter    = 15 * time.Second
	uploadAbortAfter    = 2 * time.Minute
	uploadWatchInterval = 2 * time.Second
)

// ReceiveServer hosts the upload endpoint and web interface for receiving files
type ReceiveServer struct {
	downloadDir   string
	listener      net.Listener
	httpSrv       *http.Server
	port          int
	callbacks     ReceiveCallbacks
	mu            sync.RWMutex
	fileAllocMu   sync.Mutex
	running       bool
	activeUploads map[int64]context.CancelFunc
	nextUploadID  int64
	chunkUploads  map[string]*chunkUploadState
	stallAfter    time.Duration
	abortAfter    time.Duration
	stopWatch     chan struct{}
}

// NewReceiveServer starts an HTTP server listening for file uploads
func NewReceiveServer(downloadDir string, fixedPort int, callbacks ReceiveCallbacks) (*ReceiveServer, error) {
	if downloadDir == "" {
		return nil, fmt.Errorf("carpeta de descargas no especificada")
	}

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return nil, fmt.Errorf("no se pudo crear la carpeta de descargas: %w", err)
	}

	listenAddr := "0.0.0.0:0"
	if fixedPort > 0 {
		listenAddr = fmt.Sprintf("0.0.0.0:%d", fixedPort)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("error al abrir puerto local (%s): %w", listenAddr, err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	s := &ReceiveServer{
		downloadDir:   downloadDir,
		listener:      listener,
		port:          port,
		callbacks:     callbacks,
		running:       true,
		activeUploads: make(map[int64]context.CancelFunc),
		chunkUploads:  make(map[string]*chunkUploadState),
		stallAfter:    uploadStallAfter,
		abortAfter:    uploadAbortAfter,
		stopWatch:     make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRouter)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/upload/ping", s.handleUploadPing)
	mux.HandleFunc("/upload/cancel", s.handleUploadCancel)
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
	go s.watchUploads(uploadWatchInterval)

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

func (s *ReceiveServer) registerUpload(cancel context.CancelFunc) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return 0, false
	}
	s.nextUploadID++
	id := s.nextUploadID
	if s.activeUploads == nil {
		s.activeUploads = make(map[int64]context.CancelFunc)
	}
	s.activeUploads[id] = cancel
	return id, true
}

func (s *ReceiveServer) unregisterUpload(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeUploads != nil {
		delete(s.activeUploads, id)
	}
}

func (s *ReceiveServer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopWatch)

	var cancels []context.CancelFunc
	for _, cancel := range s.activeUploads {
		cancels = append(cancels, cancel)
	}
	s.activeUploads = nil

	var orphanedTempFiles []string
	for _, state := range s.chunkUploads {
		if state.tempPath != "" {
			orphanedTempFiles = append(orphanedTempFiles, state.tempPath)
		}
	}
	s.chunkUploads = nil
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.httpSrv.Shutdown(ctx); err != nil {
			_ = s.httpSrv.Close()
		}
	}

	for _, tempPath := range orphanedTempFiles {
		_ = os.Remove(tempPath)
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

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func sanitizeFileName(originalName string) string {
	// Normalize both slashes first so filepath.Base works consistently across OSes
	normalized := strings.ReplaceAll(originalName, "\\", "/")
	cleanName := filepath.Base(normalized)

	var b strings.Builder
	b.Grow(len(cleanName))
	for _, r := range cleanName {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}

	cleanName = strings.TrimSpace(b.String())
	cleanName = strings.TrimRight(cleanName, ". ")
	if cleanName == "" || cleanName == "." || cleanName == ".." {
		return fmt.Sprintf("archivo_%d", time.Now().Unix())
	}

	ext := filepath.Ext(cleanName)
	base := strings.TrimSuffix(cleanName, ext)
	baseTrimmed := strings.TrimRight(base, ". ")
	if windowsReservedNames[strings.ToUpper(baseTrimmed)] {
		cleanName = "_" + cleanName
	}

	return cleanName
}

func resolveUniqueFilename(targetDir, originalName string) string {
	cleanName := sanitizeFileName(originalName)

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

// createUniquePartFile atomically reserves a unique destination path and creates its .part file
func (s *ReceiveServer) createUniquePartFile(targetDir, originalName string) (*os.File, string, string, error) {
	s.fileAllocMu.Lock()
	defer s.fileAllocMu.Unlock()

	cleanName := sanitizeFileName(originalName)
	ext := filepath.Ext(cleanName)
	base := strings.TrimSuffix(cleanName, ext)

	for counter := 0; ; counter++ {
		var candidate string
		if counter == 0 {
			candidate = filepath.Join(targetDir, cleanName)
		} else {
			candidate = filepath.Join(targetDir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
		}

		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			continue
		}

		tempPath := candidate + ".part"
		out, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, "", "", err
		}

		return out, candidate, tempPath, nil
	}
}

// countingWriter wraps an io.Writer and emits speed and progress updates
type countingWriter struct {
	ctx        context.Context
	writer     io.Writer
	fileID     string
	fileName   string
	totalBytes int64
	bytesRecv  int64
	lastEmit   time.Time
	// Speed = (bytesRecv - rateBytes) / since(rateStart). Zero rateStart means no
	// reliable reference yet, and speed is reported as 0 (hidden in the UI).
	rateStart   time.Time
	rateBytes   int64
	clientSpeed func() int64
	onProgress  func(ReceiveProgress)
	onData      func(bytesRecv int64) // optional, called on every successful write
	emitLock    sync.Mutex
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	if cw.ctx != nil {
		select {
		case <-cw.ctx.Done():
			return 0, errors.New("subida cancelada: servidor detenido")
		default:
		}
	}
	n, err := cw.writer.Write(p)
	if n > 0 {
		cw.bytesRecv += int64(n)
		if cw.onData != nil {
			cw.onData(cw.bytesRecv)
		}
		cw.emitLock.Lock()
		now := time.Now()
		if now.Sub(cw.lastEmit) >= 200*time.Millisecond && cw.onProgress != nil {
			var speed float64
			if cw.clientSpeed != nil && cw.clientSpeed() > 0 {
				speed = float64(cw.clientSpeed())
			} else if !cw.rateStart.IsZero() {
				if secs := now.Sub(cw.rateStart).Seconds(); secs > 0 {
					speed = float64(cw.bytesRecv-cw.rateBytes) / secs
				}
			}
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

	upCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upID, ok := s.registerUpload(cancel)
	if !ok {
		http.Error(w, "Servidor deteniéndose", http.StatusServiceUnavailable)
		return
	}
	defer s.unregisterUpload(upID)

	r = r.WithContext(upCtx)

	if r.URL.Query().Get("uploadId") != "" {
		s.handleChunkedUpload(w, r, upCtx, cancel)
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
		out, targetPath, tempPath, err := s.createUniquePartFile(targetDir, filename)
		if err != nil {
			_ = part.Close()
			http.Error(w, fmt.Sprintf("Error al crear archivo de destino: %v", err), http.StatusInternalServerError)
			return
		}
		actualFileName := filepath.Base(targetPath)

		totalEstimated := r.ContentLength
		cw := &countingWriter{
			ctx:        upCtx,
			writer:     out,
			fileID:     fileID,
			fileName:   actualFileName,
			totalBytes: totalEstimated,
			lastEmit:   time.Now(),
			rateStart:  time.Now(),
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

func (s *ReceiveServer) handleChunkedUpload(w http.ResponseWriter, r *http.Request, upCtx context.Context, cancel context.CancelFunc) {
	q := r.URL.Query()
	uploadID := strings.TrimSpace(q.Get("uploadId"))
	chunkIndex, errIdx := strconv.Atoi(q.Get("chunkIndex"))
	totalChunks, errTotal := strconv.Atoi(q.Get("totalChunks"))
	totalSize, errSize := strconv.ParseInt(q.Get("totalSize"), 10, 64)
	origFileName := q.Get("fileName")

	if uploadID == "" || errIdx != nil || errTotal != nil || errSize != nil ||
		chunkIndex < 0 || totalChunks < 1 || chunkIndex >= totalChunks || totalSize < 0 {
		http.Error(w, "Parámetros de fragmento inválidos", http.StatusBadRequest)
		return
	}

	targetDir := s.DownloadDir()
	_ = os.MkdirAll(targetDir, 0755)

	var state *chunkUploadState
	var out *os.File

	s.mu.RLock()
	state = s.chunkUploads[uploadID]
	s.mu.RUnlock()

	if state == nil && chunkIndex == 0 {
		newOut, targetPath, tempPath, err := s.createUniquePartFile(targetDir, origFileName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error al crear archivo de destino: %v", err), http.StatusInternalServerError)
			return
		}
		out = newOut
		state = &chunkUploadState{
			fileID:     generateReceiveFileID(),
			fileName:   filepath.Base(targetPath),
			targetPath: targetPath,
			tempPath:   tempPath,
			totalSize:  totalSize,
			bytesRecv:  0,
			nextChunk:  0,
			lastEmit:   time.Now(),
		}

		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			_ = out.Close()
			_ = os.Remove(tempPath)
			http.Error(w, "Servidor deteniéndose", http.StatusServiceUnavailable)
			return
		}
		if existing := s.chunkUploads[uploadID]; existing != nil {
			// A concurrent retry registered the session first: use that one.
			s.mu.Unlock()
			_ = out.Close()
			_ = os.Remove(tempPath)
			out = nil
			state = existing
		} else {
			s.chunkUploads[uploadID] = state
			s.mu.Unlock()

			if s.callbacks.OnProgress != nil {
				s.callbacks.OnProgress(ReceiveProgress{
					ID:         state.fileID,
					FileName:   state.fileName,
					BytesRecv:  0,
					TotalBytes: state.totalSize,
					Speed:      0,
					Progress:   0,
					Status:     "uploading",
				})
			}
		}
	} else if state == nil {
		http.Error(w, "Sesión de subida no encontrada", http.StatusBadRequest)
		return
	}

	s.touchUpload(state)
	if spStr := q.Get("clientSpeed"); spStr != "" {
		if sp, err := strconv.ParseInt(spStr, 10, 64); err == nil && sp >= 0 {
			state.clientSpeed.Store(sp)
		}
	}

	// Let abortChunkUpload interrupt this request even if the body read is blocked
	// (sender vanished mid-chunk): cancelling the context alone doesn't unblock reads.
	rc := http.NewResponseController(w)
	s.mu.Lock()
	state.abortActive = func() {
		cancel()
		_ = rc.SetReadDeadline(time.Now())
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		state.abortActive = nil
		s.mu.Unlock()
	}()

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.finished {
		if out != nil {
			_ = out.Close()
		}
		http.Error(w, "Subida cancelada", http.StatusGone)
		return
	}

	if chunkIndex != state.nextChunk {
		if out != nil {
			_ = out.Close()
		}
		http.Error(w, fmt.Sprintf("Fragmento fuera de orden: esperado %d, recibido %d", state.nextChunk, chunkIndex), http.StatusConflict)
		return
	}

	if out == nil {
		var err error
		out, err = os.OpenFile(state.tempPath, os.O_WRONLY, 0644)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error al abrir archivo temporal: %v", err), http.StatusInternalServerError)
			return
		}
		if _, err := out.Seek(state.bytesRecv, io.SeekStart); err != nil {
			_ = out.Close()
			http.Error(w, fmt.Sprintf("Error al posicionar archivo temporal: %v", err), http.StatusInternalServerError)
			return
		}
		_ = out.Truncate(state.bytesRecv)
	}

	cw := &countingWriter{
		ctx:         upCtx,
		writer:      out,
		fileID:      state.fileID,
		fileName:    state.fileName,
		totalBytes:  state.totalSize,
		bytesRecv:   state.bytesRecv,
		lastEmit:    state.lastEmit,
		rateStart:   state.rateStart,
		rateBytes:   state.rateBytes,
		clientSpeed: state.clientSpeed.Load,
		onProgress:  s.callbacks.OnProgress,
		onData: func(b int64) {
			state.liveBytes.Store(b)
			s.touchUpload(state)
		},
	}

	copied, copyErr := io.Copy(cw, r.Body)
	closeErr := out.Close()
	if copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}

	if copyErr != nil {
		// Keep state so client can retry this chunk; truncate partial bytes on next attempt.
		// The sender most likely went away: stop showing a live speed right now.
		state.liveBytes.Store(state.bytesRecv)
		if state.stalled.CompareAndSwap(false, true) {
			s.emitLive(state, "stalled")
		}
		http.Error(w, fmt.Sprintf("Error al escribir fragmento: %v", copyErr), http.StatusInternalServerError)
		return
	}

	state.bytesRecv += copied
	state.lastEmit = cw.lastEmit
	if state.rateStart.IsZero() {
		state.rateStart = time.Now()
		state.rateBytes = state.bytesRecv
	}
	state.nextChunk++

	isLastChunk := chunkIndex == totalChunks-1
	if !isLastChunk {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"chunkIndex": chunkIndex,
			"bytesRecv":  state.bytesRecv,
		})
		return
	}

	// Final chunk completed: remove from active chunk sessions and rename .part to targetPath
	s.mu.Lock()
	delete(s.chunkUploads, uploadID)
	s.mu.Unlock()
	state.finished = true

	if err := os.Rename(state.tempPath, state.targetPath); err != nil {
		_ = os.Remove(state.tempPath)
		if s.callbacks.OnProgress != nil {
			s.callbacks.OnProgress(ReceiveProgress{
				ID:       state.fileID,
				FileName: state.fileName,
				Status:   "error",
				Error:    err.Error(),
			})
		}
		http.Error(w, fmt.Sprintf("Error al finalizar archivo: %v", err), http.StatusInternalServerError)
		return
	}

	item := ReceivedFile{
		ID:          state.fileID,
		Name:        state.fileName,
		Size:        formatBytes(state.bytesRecv),
		RawBytes:    state.bytesRecv,
		SavedPath:   state.targetPath,
		CompletedAt: time.Now().Format("15:04:05"),
	}

	if s.callbacks.OnProgress != nil {
		s.callbacks.OnProgress(ReceiveProgress{
			ID:         state.fileID,
			FileName:   state.fileName,
			BytesRecv:  state.bytesRecv,
			TotalBytes: state.bytesRecv,
			Speed:      0,
			Progress:   100,
			Status:     "completed",
		})
	}

	if s.callbacks.OnFileDone != nil {
		s.callbacks.OnFileDone(item)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"files":   []ReceivedFile{item},
	})
}

// touchUpload records sender activity and leaves the stalled state if needed.
func (s *ReceiveServer) touchUpload(st *chunkUploadState) {
	st.lastSeen.Store(time.Now().UnixNano())
	if st.stalled.CompareAndSwap(true, false) {
		s.emitLive(st, "uploading")
	}
}

// emitLive reports a status change from outside the data path.
func (s *ReceiveServer) emitLive(st *chunkUploadState, status string) {
	if s.callbacks.OnProgress == nil {
		return
	}
	recv := st.liveBytes.Load()
	var progress float64
	if st.totalSize > 0 {
		progress = min(float64(recv)/float64(st.totalSize)*100, 99.9)
	}
	var speed float64
	if status == "uploading" {
		speed = float64(st.clientSpeed.Load())
	}
	s.callbacks.OnProgress(ReceiveProgress{
		ID:         st.fileID,
		FileName:   st.fileName,
		BytesRecv:  recv,
		TotalBytes: st.totalSize,
		Speed:      speed,
		Progress:   progress,
		Status:     status,
	})
}

// watchUploads marks silent uploads as stalled and aborts them once the sender
// has been gone for abortAfter (closed tab, lost connection, killed browser).
func (s *ReceiveServer) watchUploads(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopWatch:
			return
		case <-ticker.C:
		}

		now := time.Now()
		var stalled []*chunkUploadState
		var abort []string
		s.mu.RLock()
		for id, st := range s.chunkUploads {
			idle := now.Sub(time.Unix(0, st.lastSeen.Load()))
			if idle >= s.abortAfter {
				abort = append(abort, id)
			} else if idle >= s.stallAfter && st.stalled.CompareAndSwap(false, true) {
				stalled = append(stalled, st)
			}
		}
		s.mu.RUnlock()

		for _, st := range stalled {
			s.emitLive(st, "stalled")
		}
		for _, id := range abort {
			// May wait for an in-flight chunk handler to unwind; don't block the watchdog.
			go s.abortChunkUpload(id)
		}
	}
}

// abortChunkUpload drops an unfinished chunked upload, interrupts its in-flight
// request, deletes the .part file and reports it as cancelled.
func (s *ReceiveServer) abortChunkUpload(uploadID string) {
	s.mu.Lock()
	st := s.chunkUploads[uploadID]
	if st == nil {
		s.mu.Unlock()
		return
	}
	delete(s.chunkUploads, uploadID)
	abortActive := st.abortActive
	s.mu.Unlock()

	if abortActive != nil {
		abortActive()
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.finished {
		return
	}
	st.finished = true
	_ = os.Remove(st.tempPath)
	s.emitLive(st, "cancelled")
}

// handleUploadPing is the sender page heartbeat while an upload is progressing.
func (s *ReceiveServer) handleUploadPing(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	uploadID := strings.TrimSpace(r.URL.Query().Get("uploadId"))
	s.mu.RLock()
	st := s.chunkUploads[uploadID]
	s.mu.RUnlock()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if spStr := r.URL.Query().Get("speed"); spStr != "" {
		if sp, err := strconv.ParseInt(spStr, 10, 64); err == nil && sp >= 0 {
			st.clientSpeed.Store(sp)
		}
	}
	s.touchUpload(st)
	if !st.stalled.Load() && st.clientSpeed.Load() > 0 {
		s.emitLive(st, "uploading")
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUploadCancel is sent by the sender page (sendBeacon) when it is closed or
// gives up, so the host doesn't have to wait for the inactivity timeout.
func (s *ReceiveServer) handleUploadCancel(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	if r.Method == http.MethodPost {
		if uploadID := strings.TrimSpace(r.URL.Query().Get("uploadId")); uploadID != "" {
			go s.abortChunkUpload(uploadID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

var receivePageTemplate = template.Must(template.New("receive").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
  <title>Upload files - no-bs-transfer</title>
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
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-variant-numeric: tabular-nums;
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
        <span id="badgeText">Receiver connected</span>
      </div>
      <h1 class="title" id="pageTitle">Send files to your PC</h1>
      <p class="subtitle" id="pageSubtitle">Upload photos, videos or documents from this device to receive them instantly.</p>
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
        <span class="desktop-only" id="dropDesktop">Drag files here or click to select</span>
        <span class="mobile-only" id="dropMobile">Tap here to upload files</span>
      </div>
      <div class="drop-hint" id="dropHint">You can select multiple files at once</div>
      <button type="button" class="btn-select" id="btnBrowse">
        <span class="desktop-only" id="browseDesktop">Select files</span>
        <span class="mobile-only" id="browseMobile">Upload files</span>
      </button>
    </div>

    <!-- Active Uploads List -->
    <div class="files-list" id="filesList"></div>
  </div>

  <script>
    const isEs = (navigator.language || navigator.userLanguage || '').toLowerCase().startsWith('es');
    if (isEs) {
      document.documentElement.lang = 'es';
      document.title = 'Subir archivos - no-bs-transfer';
      const bt = document.getElementById('badgeText'); if (bt) bt.textContent = 'Receptor conectado';
      const pt = document.getElementById('pageTitle'); if (pt) pt.textContent = 'Enviar archivos a tu PC';
      const ps = document.getElementById('pageSubtitle'); if (ps) ps.textContent = 'Carga fotos, videos o documentos desde este dispositivo para recibirlos de inmediato.';
      const dd = document.getElementById('dropDesktop'); if (dd) dd.textContent = 'Arrastra archivos aquí o haz clic para seleccionar';
      const dm = document.getElementById('dropMobile'); if (dm) dm.textContent = 'Toca aquí para cargar archivos';
      const dh = document.getElementById('dropHint'); if (dh) dh.textContent = 'Puedes seleccionar varios archivos a la vez';
      const bd = document.getElementById('browseDesktop'); if (bd) bd.textContent = 'Seleccionar archivos';
      const bm = document.getElementById('browseMobile'); if (bm) bm.textContent = 'Cargar archivos';
    }

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
      if (!bytes || bytes <= 0 || isNaN(bytes)) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.min(sizes.length - 1, Math.max(0, Math.floor(Math.log(bytes) / Math.log(k))));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    function formatSpeed(bytesPerSec) {
      return formatBytes(bytesPerSec) + '/s';
    }

    const CHUNK_SIZE = 25 * 1024 * 1024;
    const MAX_CONCURRENT = 2;
    const PING_INTERVAL_MS = 5000;
    const uploadQueue = [];
    const inFlightUploads = new Set();
    let activeUploadsCount = 0;

    function pingHost(uploadId, speed) {
      let url = '/upload/ping?uploadId=' + encodeURIComponent(uploadId);
      if (speed && speed > 0) {
        url += '&speed=' + encodeURIComponent(Math.round(speed));
      }
      fetch(url, { method: 'POST' }).catch(() => {});
    }

    function cancelOnHost(uploadId) {
      const url = '/upload/cancel?uploadId=' + encodeURIComponent(uploadId);
      if (!(navigator.sendBeacon && navigator.sendBeacon(url))) {
        fetch(url, { method: 'POST', keepalive: true }).catch(() => {});
      }
    }

    // Closing or leaving the page kills in-flight uploads: tell the host right away
    // instead of letting it wait for the inactivity timeout.
    window.addEventListener('pagehide', () => {
      inFlightUploads.forEach(cancelOnHost);
    });

    function uploadFiles(files) {
      files.forEach(file => {
        const ui = createUploadCard(file);
        uploadQueue.push({ file, ui });
      });
      processQueue();
    }

    function processQueue() {
      while (activeUploadsCount < MAX_CONCURRENT && uploadQueue.length > 0) {
        const item = uploadQueue.shift();
        activeUploadsCount++;
        uploadSingleFileChunked(item.file, item.ui).finally(() => {
          activeUploadsCount--;
          processQueue();
        });
      }
    }

    function createUploadCard(file) {
      const card = document.createElement('div');
      card.className = 'file-card';

      const fileId = 'f_' + Math.random().toString(36).slice(2) + '_' + Date.now();
      const initialStatus = isEs ? 'En cola...' : 'Queued...';

      card.innerHTML = 
        '<div class="file-meta">' +
          '<span class="file-name" id="fn_' + fileId + '"></span>' +
          '<span class="file-info">' + formatBytes(file.size) + '</span>' +
        '</div>' +
        '<div class="progress-bar-bg">' +
          '<div class="progress-bar-fill" id="pb_' + fileId + '" style="width: 0%;"></div>' +
        '</div>' +
        '<div class="file-status-text">' +
          '<span class="status-badge" id="st_' + fileId + '">' + initialStatus + '</span>' +
          '<span id="sp_' + fileId + '">0%</span>' +
        '</div>';

      card.querySelector('#fn_' + fileId).textContent = file.name;
      filesList.prepend(card);

      return {
        fileId: fileId,
        pb: document.getElementById('pb_' + fileId),
        st: document.getElementById('st_' + fileId),
        sp: document.getElementById('sp_' + fileId)
      };
    }

    function sendChunkWithRetry(url, blob, onChunkProgress, maxRetries) {
      return new Promise((resolve, reject) => {
        let attempt = 0;

        function trySend() {
          attempt++;
          const xhr = new XMLHttpRequest();
          xhr.open('POST', url, true);
          xhr.setRequestHeader('Content-Type', 'application/octet-stream');

          xhr.upload.addEventListener('progress', (e) => {
            if (e.lengthComputable && onChunkProgress) {
              onChunkProgress(e.loaded);
            }
          });

          xhr.addEventListener('load', () => {
            if (xhr.status >= 200 && xhr.status < 300) {
              resolve();
            } else if ((xhr.status >= 500 || xhr.status === 429) && attempt <= maxRetries) {
              setTimeout(trySend, 1000 * attempt);
            } else {
              const statusMsg = xhr.statusText || (xhr.status ? 'HTTP ' + xhr.status : (isEs ? 'Error de red' : 'Network error'));
              reject({ type: 'http', message: statusMsg });
            }
          });

          xhr.addEventListener('error', () => {
            if (attempt <= maxRetries) {
              setTimeout(trySend, 1000 * attempt);
            } else {
              reject({ type: 'network', message: '' });
            }
          });

          xhr.send(blob);
        }

        trySend();
      });
    }

    async function uploadSingleFileChunked(file, ui) {
      const { fileId, pb, st, sp } = ui;
      st.textContent = isEs ? 'Subiendo...' : 'Uploading...';

      const totalSize = file.size;
      const totalChunks = Math.max(1, Math.ceil(totalSize / CHUNK_SIZE));

      // Rolling window for real-time speed calculation (last 2.5 seconds)
      const speedSamples = [];
      let currentSpeed = 0;
      let lastSpeedUpdate = 0;
      let displaySpeedStr = '';

      // Heartbeat so the host can tell "still uploading" from "sender gone". Driven by
      // a timer and by progress events, since background tabs throttle timers.
      inFlightUploads.add(fileId);
      let lastPing = Date.now();
      const maybePing = () => {
        if (Date.now() - lastPing >= PING_INTERVAL_MS) {
          lastPing = Date.now();
          pingHost(fileId, currentSpeed);
        }
      };
      const pingTimer = setInterval(maybePing, PING_INTERVAL_MS);

      try {
        for (let chunkIndex = 0; chunkIndex < totalChunks; chunkIndex++) {
          const start = chunkIndex * CHUNK_SIZE;
          const end = Math.min(start + CHUNK_SIZE, totalSize);
          const chunk = file.slice(start, end);

          const url = '/upload?uploadId=' + encodeURIComponent(fileId) +
            '&chunkIndex=' + chunkIndex +
            '&totalChunks=' + totalChunks +
            '&totalSize=' + totalSize +
            '&fileName=' + encodeURIComponent(file.name) +
            (currentSpeed > 0 ? '&clientSpeed=' + encodeURIComponent(Math.round(currentSpeed)) : '');

          await sendChunkWithRetry(url, chunk, (chunkLoaded) => {
            const now = Date.now();
            const totalLoaded = start + chunkLoaded;

            speedSamples.push({ t: now, b: totalLoaded });
            while (speedSamples.length > 1 && (now - speedSamples[0].t) > 2500) {
              speedSamples.shift();
            }

            // Update speed once every 1 second (1000ms) for consistency with desktop app
            if (now - lastSpeedUpdate >= 1000) {
              const dt = (now - speedSamples[0].t) / 1000;
              const db = totalLoaded - speedSamples[0].b;
              if (dt >= 0.4 && db >= 0) {
                currentSpeed = db / dt;
                displaySpeedStr = currentSpeed > 0 ? ' - ' + formatSpeed(currentSpeed) : '';
                lastSpeedUpdate = now;
              }
            }

            maybePing();

            const percent = totalSize > 0 ? Math.min(100, Math.round((totalLoaded / totalSize) * 100)) : 100;
            pb.style.width = percent + '%';
            sp.textContent = percent + '%' + displaySpeedStr;
          }, 3);
        }

        pb.style.width = '100%';
        pb.classList.add('done');
        st.textContent = isEs ? '✓ Completado' : '✓ Completed';
        st.className = 'status-badge done';
        sp.textContent = '100%';
      } catch (err) {
        cancelOnHost(fileId);
        pb.classList.add('error');
        st.className = 'status-badge error';
        if (err && err.type === 'network') {
          st.textContent = isEs ? 'Fallo de conexión' : 'Connection failed';
          sp.textContent = '';
        } else {
          st.textContent = isEs ? 'Error al subir' : 'Upload error';
          sp.textContent = (err && err.message) ? err.message : (isEs ? 'Error de red' : 'Network error');
        }
      } finally {
        clearInterval(pingTimer);
        inFlightUploads.delete(fileId);
      }
    }
  </script>
</body>
</html>`))

