package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"tgdrive/internal/storage"
)

type Backend interface {
	Entries(directory string) []storage.Entry
	List() []storage.File
	DirectoryPaths() []string
	IsDir(directory string) bool
	Get(name string) (storage.File, bool)
	Put(ctx context.Context, name string, input io.Reader, size int64) (storage.File, error)
	OpenRange(ctx context.Context, file storage.File, start int64) (io.ReadCloser, error)
	Delete(ctx context.Context, file storage.File) error
	Mkdir(directory string) error
	RemoveDir(ctx context.Context, directory string) error
	Rename(oldName, newName string) error
}

type ThumbnailBackend interface {
	GetThumbnail(file storage.File) ([]byte, bool)
	PutThumbnail(file storage.File, content []byte)
}

type Server struct {
	Backend     Backend
	AccessToken string
	Logger      *log.Logger
	Debug       bool

	progressMu     sync.Mutex
	uploads        map[string]uploadProgressState
	uploadControls map[string]*uploadControl
}

type uploadProgressState struct {
	UploadedBytes int64  `json:"uploaded_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Complete      bool   `json:"complete"`
	Error         string `json:"error,omitempty"`
	updatedAt     time.Time
}

type uploadControl struct {
	mu      sync.Mutex
	paused  bool
	resumed chan struct{}
	cancel  context.CancelFunc
}

func newUploadControl(cancel context.CancelFunc) *uploadControl {
	return &uploadControl{resumed: make(chan struct{}), cancel: cancel}
}

func (c *uploadControl) pause() {
	c.mu.Lock()
	c.paused = true
	c.mu.Unlock()
}

func (c *uploadControl) resume() {
	c.mu.Lock()
	if c.paused {
		c.paused = false
		close(c.resumed)
		c.resumed = make(chan struct{})
	}
	c.mu.Unlock()
}

func (c *uploadControl) wait(ctx context.Context) error {
	c.mu.Lock()
	paused := c.paused
	resumed := c.resumed
	c.mu.Unlock()
	if !paused {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-resumed:
		return nil
	}
}

func NewServer(backend Backend, accessToken string, logger *log.Logger, debug bool) *Server {
	return &Server{
		Backend:        backend,
		AccessToken:    accessToken,
		Logger:         logger,
		Debug:          debug,
		uploads:        make(map[string]uploadProgressState),
		uploadControls: make(map[string]*uploadControl),
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, X-File-Path, X-Upload-ID")
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	if request.URL.Path == "/api/v1/health" && request.Method == http.MethodGet {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !s.authorized(request) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch {
	case request.URL.Path == "/api/v1/entries" && request.Method == http.MethodGet:
		s.listEntries(writer, request)
	case request.URL.Path == "/api/v1/search" && request.Method == http.MethodGet:
		s.search(writer, request)
	case request.URL.Path == "/api/v1/stats" && request.Method == http.MethodGet:
		s.stats(writer, request)
	case request.URL.Path == "/api/v1/uploads/progress" && request.Method == http.MethodGet:
		s.uploadProgress(writer, request)
	case request.URL.Path == "/api/v1/uploads/control" && request.Method == http.MethodPost:
		s.controlUpload(writer, request)
	case request.URL.Path == "/api/v1/cache" && request.Method == http.MethodDelete:
		s.clearCache(writer, request)
	case request.URL.Path == "/api/v1/files" && request.Method == http.MethodPost:
		s.upload(writer, request)
	case request.URL.Path == "/api/v1/files" && request.Method == http.MethodPatch:
		s.rename(writer, request)
	case request.URL.Path == "/api/v1/files" && request.Method == http.MethodDelete:
		s.delete(writer, request)
	case request.URL.Path == "/api/v1/files/content" && request.Method == http.MethodGet:
		s.content(writer, request)
	case request.URL.Path == "/api/v1/files/thumbnail" && request.Method == http.MethodGet:
		s.thumbnail(writer, request)
	case request.URL.Path == "/api/v1/folders" && request.Method == http.MethodPost:
		s.createFolder(writer, request)
	case request.URL.Path == "/api/v1/folders" && request.Method == http.MethodDelete:
		s.removeFolder(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "endpoint not found")
	}
}

func (s *Server) refreshIndex(writer http.ResponseWriter, ctx context.Context) bool {
	refresher, ok := s.Backend.(interface{ Refresh(context.Context) error })
	if !ok {
		return true
	}
	if err := refresher.Refresh(ctx); err != nil {
		s.logf("refresh index: %v", err)
		writeError(writer, http.StatusBadGateway, "Telegram index is unavailable")
		return false
	}
	return true
}

func (s *Server) authorized(request *http.Request) bool {
	if s.AccessToken == "" {
		return true
	}
	provided := strings.TrimSpace(request.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(provided), "bearer ") {
		provided = strings.TrimSpace(provided[len("Bearer "):])
	}
	if provided == "" {
		provided = request.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.AccessToken)) == 1
}

func (s *Server) listEntries(writer http.ResponseWriter, request *http.Request) {
	if !s.refreshIndex(writer, request.Context()) {
		return
	}
	directory, err := requestPath(request)
	if err != nil || !s.Backend.IsDir(directory) {
		writeError(writer, http.StatusNotFound, "directory not found")
		return
	}

	entries := make([]entryResponse, 0)
	for _, entry := range s.Backend.Entries(directory) {
		entryPath := joinPath(directory, entry.Name)
		entries = append(entries, entryResponse{
			Name:        entry.Name,
			Path:        entryPath,
			IsDir:       entry.IsDir,
			Size:        entry.Size,
			ContentHash: entry.ContentHash,
			UpdatedAt:   entry.Updated,
			MimeType:    entryMimeType(entry.Name, entry.IsDir),
			Thumbnail:   !entry.IsDir && supportsThumbnail(entry.Name),
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"path":    directory,
		"entries": entries,
	})
}

func (s *Server) search(writer http.ResponseWriter, request *http.Request) {
	if !s.refreshIndex(writer, request.Context()) {
		return
	}
	query := strings.TrimSpace(strings.ToLower(request.URL.Query().Get("q")))
	if query == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"query": "", "entries": []entryResponse{}})
		return
	}

	entries := make([]entryResponse, 0)
	for _, file := range s.Backend.List() {
		if !strings.Contains(strings.ToLower(file.Name), query) {
			continue
		}
		entries = append(entries, entryResponse{
			Name:        path.Base(file.Name),
			Path:        file.Name,
			Size:        file.Size,
			ContentHash: file.ContentHash,
			UpdatedAt:   file.UpdatedAt,
			MimeType:    entryMimeType(file.Name, false),
			Thumbnail:   supportsThumbnail(file.Name),
		})
	}
	for _, directory := range s.Backend.DirectoryPaths() {
		if !strings.Contains(strings.ToLower(directory), query) {
			continue
		}
		entries = append(entries, entryResponse{
			Name:     path.Base(directory),
			Path:     directory,
			IsDir:    true,
			MimeType: entryMimeType(directory, true),
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"query": query, "entries": entries})
}

func (s *Server) stats(writer http.ResponseWriter, request *http.Request) {
	if !s.refreshIndex(writer, request.Context()) {
		return
	}
	files := s.Backend.List()
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.Size
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"file_count":  len(files),
		"total_bytes": totalBytes,
	})
}

func (s *Server) upload(writer http.ResponseWriter, request *http.Request) {
	name, err := requestPath(request)
	if err != nil || name == "/" {
		writeError(writer, http.StatusBadRequest, "file path is required")
		return
	}
	if s.Backend.IsDir(name) {
		writeError(writer, http.StatusConflict, "path is a directory")
		return
	}

	uploadID := strings.TrimSpace(request.Header.Get("X-Upload-ID"))
	uploadCtx, control := s.beginUpload(request.Context(), uploadID)
	defer s.endUpload(uploadID, control)
	if uploadID != "" {
		s.setUploadProgress(uploadID, 0, -1, false, "")
	}

	var file storage.File
	var putErr error
	if progressBackend, ok := s.Backend.(interface {
		PutWithProgress(context.Context, string, io.Reader, int64, func(int64, int64) error) (storage.File, error)
	}); ok {
		file, putErr = progressBackend.PutWithProgress(
			uploadCtx,
			name,
			request.Body,
			request.ContentLength,
			func(uploadedBytes, totalBytes int64) error {
				if control != nil {
					if err := control.wait(uploadCtx); err != nil {
						return err
					}
				}
				s.setUploadProgress(uploadID, uploadedBytes, totalBytes, false, "")
				return nil
			},
		)
	} else {
		file, putErr = s.Backend.Put(uploadCtx, name, request.Body, request.ContentLength)
	}
	if putErr != nil {
		s.finishUploadProgress(uploadID, putErr)
		writeError(writer, http.StatusBadRequest, putErr.Error())
		return
	}
	s.finishUploadProgress(uploadID, nil)
	writeJSON(writer, http.StatusCreated, map[string]storage.File{"file": file})
}

func (s *Server) uploadProgress(writer http.ResponseWriter, request *http.Request) {
	uploadID := strings.TrimSpace(request.URL.Query().Get("upload_id"))
	if uploadID == "" {
		writeError(writer, http.StatusBadRequest, "upload_id is required")
		return
	}
	progress, ok := s.getUploadProgress(uploadID)
	if !ok {
		writeError(writer, http.StatusNotFound, "upload progress not found")
		return
	}
	writeJSON(writer, http.StatusOK, progress)
}

func (s *Server) controlUpload(writer http.ResponseWriter, request *http.Request) {
	uploadID := strings.TrimSpace(request.URL.Query().Get("upload_id"))
	action := strings.TrimSpace(request.URL.Query().Get("action"))
	if uploadID == "" || action == "" {
		writeError(writer, http.StatusBadRequest, "upload_id and action are required")
		return
	}

	s.progressMu.Lock()
	control := s.uploadControls[uploadID]
	s.progressMu.Unlock()
	if control == nil {
		writeError(writer, http.StatusNotFound, "upload is not active")
		return
	}

	switch action {
	case "pause":
		control.pause()
	case "resume":
		control.resume()
	case "cancel":
		control.cancel()
	default:
		writeError(writer, http.StatusBadRequest, "unsupported upload action")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearCache(writer http.ResponseWriter, request *http.Request) {
	cacheBackend, ok := s.Backend.(interface{ ClearCache() })
	if !ok {
		writeError(writer, http.StatusNotImplemented, "cache cannot be cleared")
		return
	}
	cacheBackend.ClearCache()
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) beginUpload(parent context.Context, uploadID string) (context.Context, *uploadControl) {
	if uploadID == "" {
		return parent, nil
	}
	ctx, cancel := context.WithCancel(parent)
	control := newUploadControl(cancel)
	s.progressMu.Lock()
	if existing := s.uploadControls[uploadID]; existing != nil {
		existing.cancel()
	}
	s.uploadControls[uploadID] = control
	s.progressMu.Unlock()
	return ctx, control
}

func (s *Server) endUpload(uploadID string, control *uploadControl) {
	if uploadID == "" || control == nil {
		return
	}
	s.progressMu.Lock()
	if s.uploadControls[uploadID] == control {
		delete(s.uploadControls, uploadID)
	}
	s.progressMu.Unlock()
	control.cancel()
}

func (s *Server) setUploadProgress(uploadID string, uploadedBytes, totalBytes int64, complete bool, errorMessage string) {
	if uploadID == "" {
		return
	}
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.pruneUploadProgressLocked(time.Now())
	if s.uploads == nil {
		s.uploads = make(map[string]uploadProgressState)
	}
	progress, exists := s.uploads[uploadID]
	if !exists {
		progress.TotalBytes = -1
	}
	if uploadedBytes < progress.UploadedBytes {
		uploadedBytes = progress.UploadedBytes
	}
	if totalBytes < 0 && exists && progress.TotalBytes >= 0 {
		totalBytes = progress.TotalBytes
	}
	progress.UploadedBytes = uploadedBytes
	progress.TotalBytes = totalBytes
	progress.Complete = complete
	progress.Error = errorMessage
	progress.updatedAt = time.Now()
	s.uploads[uploadID] = progress
}

func (s *Server) finishUploadProgress(uploadID string, err error) {
	if uploadID == "" {
		return
	}
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.uploads == nil {
		s.uploads = make(map[string]uploadProgressState)
	}
	progress := s.uploads[uploadID]
	progress.Complete = true
	progress.Error = errorMessage
	progress.updatedAt = time.Now()
	s.uploads[uploadID] = progress
}

func (s *Server) getUploadProgress(uploadID string) (uploadProgressState, bool) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.pruneUploadProgressLocked(time.Now())
	progress, ok := s.uploads[uploadID]
	return progress, ok
}

func (s *Server) pruneUploadProgressLocked(now time.Time) {
	for uploadID, progress := range s.uploads {
		if progress.Complete && now.Sub(progress.updatedAt) > 10*time.Minute {
			delete(s.uploads, uploadID)
		}
	}
}

func (s *Server) rename(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid rename request")
		return
	}
	oldPath, err := storage.NormalizePath(payload.OldPath)
	if err != nil || oldPath == "/" {
		writeError(writer, http.StatusBadRequest, "invalid source path")
		return
	}
	newPath, err := storage.NormalizePath(payload.NewPath)
	if err != nil || newPath == "/" {
		writeError(writer, http.StatusBadRequest, "invalid target path")
		return
	}
	if err := s.Backend.Rename(oldPath, newPath); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"old_path": oldPath, "new_path": newPath})
}

func (s *Server) delete(writer http.ResponseWriter, request *http.Request) {
	if !s.refreshIndex(writer, request.Context()) {
		return
	}
	name, err := requestPath(request)
	if err != nil || name == "/" {
		writeError(writer, http.StatusBadRequest, "invalid path")
		return
	}
	file, ok := s.Backend.Get(name)
	if !ok {
		if s.Backend.IsDir(name) {
			writeError(writer, http.StatusConflict, "path is a folder; use the folders endpoint")
			return
		}
		writeError(writer, http.StatusNotFound, "file not found")
		return
	}
	if err := s.Backend.Delete(request.Context(), file); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) createFolder(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Path   string `json:"path"`
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid folder request")
		return
	}
	folderPath := payload.Path
	if folderPath == "" {
		parent, parentErr := storage.NormalizePath(payload.Parent)
		if parentErr != nil {
			writeError(writer, http.StatusBadRequest, "invalid parent path")
			return
		}
		folderPath = joinPath(parent, payload.Name)
	}
	folderPath, err := storage.NormalizePath(folderPath)
	if err != nil || folderPath == "/" {
		writeError(writer, http.StatusBadRequest, "invalid folder path")
		return
	}
	if err := s.Backend.Mkdir(folderPath); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]string{"path": folderPath})
}

func (s *Server) removeFolder(writer http.ResponseWriter, request *http.Request) {
	folderPath, err := requestPath(request)
	if err != nil || folderPath == "/" {
		writeError(writer, http.StatusBadRequest, "invalid folder path")
		return
	}
	if err := s.Backend.RemoveDir(request.Context(), folderPath); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) content(writer http.ResponseWriter, request *http.Request) {
	filePath, err := requestPath(request)
	if err != nil || filePath == "/" {
		writeError(writer, http.StatusBadRequest, "file path is required")
		return
	}
	file, ok := s.Backend.Get(filePath)
	if !ok {
		writeError(writer, http.StatusNotFound, "file not found")
		return
	}

	start, end, partial, err := parseRange(request.Header.Get("Range"), file.Size)
	if err != nil {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", file.Size))
		writeError(writer, http.StatusRequestedRangeNotSatisfiable, "invalid range")
		return
	}
	contentLength := file.Size
	status := http.StatusOK
	if partial {
		contentLength = end - start + 1
		status = http.StatusPartialContent
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Size))
	}
	var reader io.ReadCloser
	if contentLength > 0 {
		reader, err = s.Backend.OpenRange(request.Context(), file, start)
		if err != nil {
			s.logf("open content path=%s start=%d: %v", file.Name, start, err)
			writeError(writer, http.StatusBadGateway, "file stream is unavailable")
			return
		}
		defer reader.Close()
	}

	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	writer.Header().Set("Content-Type", entryMimeType(file.Name, false))
	writer.Header().Set("Last-Modified", file.UpdatedAt.UTC().Format(http.TimeFormat))
	writer.Header().Set("ETag", fmt.Sprintf("\"%d-%d\"", file.MessageID, file.Size))
	if request.URL.Query().Get("download") == "1" {
		writer.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(path.Base(file.Name)))
	} else {
		writer.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(path.Base(file.Name)))
	}
	writer.WriteHeader(status)
	if contentLength == 0 {
		return
	}
	if _, err := io.CopyN(writer, reader, contentLength); err != nil && !errors.Is(err, context.Canceled) {
		s.logf("stream content path=%s start=%d length=%d: %v", file.Name, start, contentLength, err)
	}
}

func (s *Server) thumbnail(writer http.ResponseWriter, request *http.Request) {
	filePath, err := requestPath(request)
	if err != nil || filePath == "/" {
		writeError(writer, http.StatusBadRequest, "file path is required")
		return
	}
	file, ok := s.Backend.Get(filePath)
	if !ok || !supportsThumbnail(file.Name) {
		writeError(writer, http.StatusNotFound, "thumbnail not available")
		return
	}

	var thumbnail []byte
	cached := false
	cacheable := false
	if provider, ok := s.Backend.(ThumbnailBackend); ok {
		thumbnail, cached = provider.GetThumbnail(file)
		cacheable = cached
	}
	if !cached {
		if isVideo(file.Name) {
			writeError(writer, http.StatusServiceUnavailable, "video thumbnail is generated on device")
			return
		}
		reader, readerErr := s.Backend.OpenRange(request.Context(), file, 0)
		if readerErr == nil {
			thumbnail, _ = imageThumbnail(io.LimitReader(reader, 32<<20), 512)
			_ = reader.Close()
		}
		if len(thumbnail) == 0 {
			// The client retries this response. A fake image masks transient
			// Telegram rate limits and can be cached as if it were real.
			writeError(writer, http.StatusServiceUnavailable, "thumbnail temporarily unavailable")
			return
		}
		cacheable = true
		if provider, ok := s.Backend.(ThumbnailBackend); ok {
			provider.PutThumbnail(file, thumbnail)
		}
	}
	writer.Header().Set("Content-Type", "image/png")
	if cacheable {
		writer.Header().Set("Cache-Control", "private, max-age=3600")
	} else {
		writer.Header().Set("Cache-Control", "no-store")
	}
	writer.Header().Set("X-Thumbnail-Source", "generated-or-placeholder")
	writer.Header().Set("Content-Length", strconv.Itoa(len(thumbnail)))
	_, _ = writer.Write(thumbnail)
}

type entryResponse struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	ContentHash string    `json:"content_hash,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	MimeType    string    `json:"mime_type"`
	Thumbnail   bool      `json:"thumbnail"`
}

func requestPath(request *http.Request) (string, error) {
	value := request.URL.Query().Get("path")
	if value == "" {
		value = "/"
	}
	return storage.NormalizePath(value)
}

func joinPath(directory, name string) string {
	if directory == "/" || directory == "" {
		return name
	}
	return path.Join(directory, name)
}

func entryMimeType(name string, isDir bool) string {
	if isDir {
		return "inode/directory"
	}
	if value := mime.TypeByExtension(strings.ToLower(path.Ext(name))); value != "" {
		return value
	}
	return "application/octet-stream"
}

func supportsThumbnail(name string) bool {
	return isVideo(name) || strings.HasPrefix(entryMimeType(name, false), "image/")
}

func isVideo(name string) bool {
	if strings.HasPrefix(entryMimeType(name, false), "video/") {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".m4v", ".mkv", ".mov", ".avi", ".webm", ".3gp", ".ts":
		return true
	default:
		return false
	}
}

func parseRange(value string, size int64) (int64, int64, bool, error) {
	if value == "" {
		if size == 0 {
			return 0, 0, false, nil
		}
		return 0, size - 1, false, nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size <= 0 {
		return 0, 0, false, errors.New("unsupported range")
	}
	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(value, "bytes=")), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, errors.New("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false, errors.New("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false, errors.New("invalid range start")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false, errors.New("invalid range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true, nil
}

func imageThumbnail(content io.Reader, maxDimension int) ([]byte, error) {
	decoded, _, err := image.Decode(content)
	if err != nil {
		return nil, err
	}
	resized := resizeImage(decoded, maxDimension)
	var output bytes.Buffer
	if err := png.Encode(&output, resized); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func resizeImage(source image.Image, maxDimension int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxDimension && height <= maxDimension {
		return source
	}
	scale := float64(maxDimension) / float64(width)
	if height > width {
		scale = float64(maxDimension) / float64(height)
	}
	destinationWidth := max(1, int(float64(width)*scale))
	destinationHeight := max(1, int(float64(height)*scale))
	destination := image.NewRGBA(image.Rect(0, 0, destinationWidth, destinationHeight))
	for y := 0; y < destinationHeight; y++ {
		sourceY := bounds.Min.Y + y*height/destinationHeight
		for x := 0; x < destinationWidth; x++ {
			sourceX := bounds.Min.X + x*width/destinationWidth
			destination.Set(x, y, source.At(sourceX, sourceY))
		}
	}
	return destination
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	return decoder.Decode(target)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func (s *Server) logf(format string, args ...any) {
	if s.Debug && s.Logger != nil {
		s.Logger.Printf("http: "+format, args...)
	}
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
