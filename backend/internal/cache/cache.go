package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tgdrive/internal/storage"
)

const (
	DefaultMaxSize = int64(500 * 1024 * 1024)
	DefaultTTL     = 2 * time.Hour
)

var errCacheFull = errors.New("cache capacity is full")

type Source func(context.Context, int64) (io.ReadCloser, error)

type Manager struct {
	root     string
	maxBytes int64
	ttl      time.Duration
	mu       sync.Mutex
	segments map[string][]*segment
	Debug    bool
}

type segment struct {
	mu         sync.Mutex
	fillMu     sync.Mutex
	key        string
	fileName   string
	start      int64
	size       int64
	filePath   string
	complete   bool
	lastAccess time.Time
	readers    int
	removed    bool
}

func New(root string, maxBytes int64, ttl time.Duration) (*Manager, error) {
	if maxBytes <= 0 {
		return nil, errors.New("cache size must be positive")
	}
	if ttl < 0 {
		return nil, errors.New("cache TTL cannot be negative")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	manager := &Manager{root: root, maxBytes: maxBytes, ttl: ttl, segments: make(map[string][]*segment)}
	if err := manager.loadPersistent(); err != nil {
		return nil, err
	}
	manager.trimToLimit()
	return manager, nil
}

func (m *Manager) loadPersistent() error {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return fmt.Errorf("read cache directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cache") {
			continue
		}
		filePath := filepath.Join(m.root, entry.Name())
		markerPath := filePath + ".complete"
		if _, err := os.Stat(markerPath); err != nil {
			_ = os.Remove(filePath)
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".cache")
		separator := strings.LastIndexByte(base, '-')
		if separator <= 0 || separator == len(base)-1 {
			_ = os.Remove(filePath)
			_ = os.Remove(markerPath)
			continue
		}
		start, parseErr := strconv.ParseInt(base[separator+1:], 10, 64)
		if parseErr != nil || start < 0 {
			_ = os.Remove(filePath)
			_ = os.Remove(markerPath)
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() <= 0 {
			_ = os.Remove(filePath)
			_ = os.Remove(markerPath)
			continue
		}
		markerInfo, markerErr := os.Stat(markerPath)
		lastAccess := info.ModTime()
		if markerErr == nil && markerInfo.ModTime().After(lastAccess) {
			lastAccess = markerInfo.ModTime()
		}
		key := base[:separator]
		fileNameBytes, _ := os.ReadFile(filePath + ".name")
		m.segments[key] = append(m.segments[key], &segment{
			key:        key,
			fileName:   string(fileNameBytes),
			start:      start,
			size:       info.Size(),
			filePath:   filePath,
			complete:   false,
			lastAccess: lastAccess,
		})
	}
	return nil
}

func (m *Manager) Start(ctx context.Context) {
	if m.ttl == 0 {
		return
	}
	interval := m.ttl / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *Manager) Open(ctx context.Context, file storage.File, start int64, source Source) (io.ReadCloser, error) {
	if start < 0 || start > file.Size {
		return nil, fmt.Errorf("invalid cache offset %d", start)
	}
	key := cacheKey(file)
	m.invalidateOtherVersions(file.Name, key)
	if cached, ok := m.openSegment(ctx, key, file.Size, start, source); ok {
		m.debugf("cache hit key=%s start=%d", key, start)
		return cached, nil
	}
	m.debugf("cache miss key=%s start=%d file_size=%d", key, start, file.Size)
	reader, err := source(ctx, start)
	if err != nil {
		return nil, err
	}
	return m.newSegmentReader(ctx, key, file.Name, file.Size, start, source, reader)
}

func (m *Manager) openSegment(ctx context.Context, key string, fileSize, start int64, source Source) (io.ReadCloser, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, item := range m.segments[key] {
		item.mu.Lock()
		lastAccess := item.lastAccess
		itemStart := item.start
		cachedSize := item.size
		complete := item.complete
		removed := item.removed
		item.mu.Unlock()

		if removed {
			continue
		}
		if m.ttl > 0 && time.Since(lastAccess) >= m.ttl {
			m.removeSegmentLocked(item)
			continue
		}
		if start < itemStart || start > itemStart+cachedSize {
			continue
		}

		readFile, err := os.Open(item.filePath)
		if err != nil {
			m.removeSegmentLocked(item)
			continue
		}
		if _, err := readFile.Seek(start-itemStart, io.SeekStart); err != nil {
			_ = readFile.Close()
			continue
		}
		var appendFile *os.File
		if !complete {
			appendFile, err = os.OpenFile(item.filePath, os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				_ = readFile.Close()
				continue
			}
		}
		item.mu.Lock()
		if item.removed {
			item.mu.Unlock()
			_ = readFile.Close()
			if appendFile != nil {
				_ = appendFile.Close()
			}
			continue
		}
		item.readers++
		item.lastAccess = time.Now()
		item.mu.Unlock()
		return &segmentReader{ctx: ctx, manager: m, source: source, item: item, fileSize: fileSize, readFile: readFile, appendFile: appendFile, readPosition: start}, true
	}
	return nil, false
}

func (m *Manager) newSegmentReader(ctx context.Context, key, fileName string, fileSize, start int64, source Source, sourceReader io.ReadCloser) (io.ReadCloser, error) {
	filePath := filepath.Join(m.root, fmt.Sprintf("%s-%d.cache", key, start))
	appendFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		_ = sourceReader.Close()
		if os.IsExist(err) {
			if cached, ok := m.openSegment(ctx, key, fileSize, start, source); ok {
				return cached, nil
			}
			return source(ctx, start)
		}
		return sourceReader, nil
	}
	readFile, err := os.Open(filePath)
	if err != nil {
		_ = appendFile.Close()
		_ = os.Remove(filePath)
		return sourceReader, nil
	}
	item := &segment{key: key, fileName: fileName, start: start, filePath: filePath, lastAccess: time.Now(), readers: 1}
	if err := os.WriteFile(filePath+".name", []byte(fileName), 0o600); err != nil {
		m.debugf("persist cache name %s: %v", filePath, err)
	}
	m.mu.Lock()
	m.segments[key] = append(m.segments[key], item)
	m.mu.Unlock()
	return &segmentReader{ctx: ctx, manager: m, source: source, initialReader: sourceReader, item: item, fileSize: fileSize, readFile: readFile, appendFile: appendFile, readPosition: start}, nil
}

type segmentReader struct {
	ctx           context.Context
	manager       *Manager
	source        Source
	initialReader io.ReadCloser
	item          *segment
	fileSize      int64
	readFile      *os.File
	appendFile    *os.File
	activeSource  io.ReadCloser
	readPosition  int64
	closed        sync.Once
}

func (r *segmentReader) Read(output []byte) (int, error) {
	if len(output) == 0 {
		return 0, nil
	}

	written := 0
	for len(output) > 0 {
		r.manager.touch(r.item)
		cachedSize, complete := r.item.snapshot()
		if r.readPosition < r.item.start+cachedSize {
			n, err := r.readFile.Read(output)
			if n > 0 {
				r.readPosition += int64(n)
				written += n
				output = output[n:]
			}
			if err != nil {
				if written > 0 {
					return written, nil
				}
				return 0, err
			}
			continue
		}
		if complete {
			if r.readPosition >= r.fileSize {
				if written > 0 {
					return written, nil
				}
				return 0, io.EOF
			}
			if written > 0 {
				return written, io.ErrUnexpectedEOF
			}
			return 0, io.ErrUnexpectedEOF
		}
		if r.readPosition >= r.fileSize {
			r.markComplete()
			if written > 0 {
				return written, nil
			}
			return 0, io.EOF
		}

		r.item.fillMu.Lock()
		cachedSize, complete = r.item.snapshot()
		if r.readPosition < r.item.start+cachedSize {
			r.item.fillMu.Unlock()
			continue
		}
		if complete || r.readPosition >= r.fileSize {
			r.item.fillMu.Unlock()
			continue
		}
		if err := r.openContinuation(r.readPosition); err != nil {
			r.item.fillMu.Unlock()
			if written > 0 {
				return written, nil
			}
			return 0, err
		}
		n, readErr := r.activeSource.Read(output)
		if n > 0 {
			if r.appendFile != nil {
				if err := r.manager.cacheBytes(r.item, r.appendFile, output[:n]); err != nil {
					if errors.Is(err, errCacheFull) {
						r.stopCaching()
					} else {
						r.disableCache()
					}
				}
			}
			r.readPosition += int64(n)
			written += n
			output = output[n:]
		}
		reachedEnd := r.readPosition >= r.fileSize
		r.item.fillMu.Unlock()

		// io.CopyN may stop exactly at Content-Length and never issue a final
		// read for io.EOF. Mark the segment complete as soon as the full file
		// has been consumed, otherwise it is discarded on the next restart.
		if readErr == io.EOF || reachedEnd {
			r.markComplete()
		}
		if readErr != nil {
			if written > 0 {
				return written, nil
			}
			return 0, readErr
		}
		if n == 0 {
			if written > 0 {
				return written, nil
			}
			return 0, io.ErrNoProgress
		}
	}
	return written, nil
}

func (r *segmentReader) openContinuation(offset int64) error {
	if r.activeSource != nil {
		return nil
	}
	if r.initialReader != nil {
		r.activeSource = r.initialReader
		r.initialReader = nil
		return nil
	}
	reader, err := r.source(r.ctx, offset)
	if err != nil {
		return err
	}
	r.activeSource = reader
	return nil
}

func (r *segmentReader) markComplete() {
	r.item.mu.Lock()
	if r.item.removed || r.appendFile == nil {
		r.item.mu.Unlock()
		return
	}
	r.item.complete = true
	r.item.lastAccess = time.Now()
	r.item.mu.Unlock()
	r.persist()
}

func (r *segmentReader) persist() {
	r.item.mu.Lock()
	if r.item.removed || r.appendFile == nil || r.item.size == 0 {
		r.item.mu.Unlock()
		return
	}
	markerPath := r.item.filePath + ".complete"
	appendFile := r.appendFile
	r.item.mu.Unlock()
	if err := appendFile.Sync(); err != nil {
		r.manager.debugf("sync cache segment %s: %v", r.item.filePath, err)
		return
	}
	if err := os.WriteFile(markerPath, nil, 0o600); err != nil {
		r.manager.debugf("persist cache marker %s: %v", markerPath, err)
	}
}

func (r *segmentReader) stopCaching() {
	if r.appendFile == nil {
		return
	}
	r.persist()
	_ = r.appendFile.Close()
	r.appendFile = nil
}

func (r *segmentReader) disableCache() {
	if r.appendFile != nil {
		_ = r.appendFile.Close()
		r.appendFile = nil
	}
	r.manager.removeSegment(r.item)
}

func (r *segmentReader) Close() error {
	r.closed.Do(func() {
		if r.activeSource != nil {
			_ = r.activeSource.Close()
		}
		if r.initialReader != nil {
			_ = r.initialReader.Close()
		}
		_ = r.readFile.Close()
		r.persist()
		if r.appendFile != nil {
			_ = r.appendFile.Close()
		}
		r.manager.release(r.item)
	})
	return nil
}

func (m *Manager) debugf(format string, args ...any) {
	if m.Debug {
		log.Printf("cache: "+format, args...)
	}
}

func (m *Manager) touch(item *segment) {
	item.mu.Lock()
	item.lastAccess = time.Now()
	item.mu.Unlock()
}

func (m *Manager) trimToLimit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	candidates := make([]*segment, 0)
	for _, items := range m.segments {
		for _, item := range items {
			_, size, readers, removed := item.snapshotState()
			if removed {
				continue
			}
			total += size
			if readers == 0 {
				candidates = append(candidates, item)
			}
		}
	}
	if total <= m.maxBytes {
		return
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].accessTime().Before(candidates[j].accessTime())
	})
	for _, candidate := range candidates {
		if total <= m.maxBytes {
			break
		}
		_, size, _, removed := candidate.snapshotState()
		if removed {
			continue
		}
		m.removeSegmentLocked(candidate)
		total -= size
	}
}

func (m *Manager) cacheBytes(item *segment, appendFile *os.File, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.mu.Lock()
	removed := item.removed
	item.mu.Unlock()
	if removed {
		return errors.New("cache segment removed")
	}

	incoming := int64(len(data))
	var total int64
	for _, items := range m.segments {
		for _, value := range items {
			_, size, _, removed := value.snapshotState()
			if !removed {
				total += size
			}
		}
	}
	if total+incoming > m.maxBytes {
		candidates := make([]*segment, 0)
		for _, items := range m.segments {
			for _, value := range items {
				if value == item {
					continue
				}
				_, _, readers, removed := value.snapshotState()
				if !removed && readers == 0 {
					candidates = append(candidates, value)
				}
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].accessTime().Before(candidates[j].accessTime())
		})
		for _, candidate := range candidates {
			if total+incoming <= m.maxBytes {
				break
			}
			_, size, _, removed := candidate.snapshotState()
			if removed {
				continue
			}
			m.removeSegmentLocked(candidate)
			total -= size
		}
	}
	if total+incoming > m.maxBytes {
		return errCacheFull
	}

	item.mu.Lock()
	defer item.mu.Unlock()
	if item.removed {
		return errors.New("cache segment removed")
	}
	written, err := appendFile.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	item.size += int64(written)
	item.lastAccess = time.Now()
	return nil
}

func (m *Manager) removeSegment(item *segment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeSegmentLocked(item)
}

func (m *Manager) removeSegmentLocked(item *segment) {
	items := m.segments[item.key]
	for index, candidate := range items {
		if candidate == item {
			m.segments[item.key] = append(items[:index], items[index+1:]...)
			break
		}
	}
	item.mu.Lock()
	item.removed = true
	readers := item.readers
	item.mu.Unlock()
	if readers == 0 {
		_ = os.Remove(item.filePath)
	}
	_ = os.Remove(item.filePath + ".complete")
	_ = os.Remove(item.filePath + ".name")
}

func (m *Manager) release(item *segment) {
	m.mu.Lock()
	item.mu.Lock()
	if item.readers > 0 {
		item.readers--
	}
	removeFile := item.removed && item.readers == 0
	item.mu.Unlock()
	m.mu.Unlock()
	if removeFile {
		_ = os.Remove(item.filePath)
	}
}

func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, items := range m.segments {
		for _, item := range items {
			item.mu.Lock()
			item.removed = true
			readers := item.readers
			item.mu.Unlock()
			if readers == 0 {
				_ = os.Remove(item.filePath)
			}
		}
		delete(m.segments, key)
	}
	if entries, err := os.ReadDir(m.root); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".cache") || strings.HasSuffix(entry.Name(), ".complete") || strings.HasSuffix(entry.Name(), ".name") {
				_ = os.Remove(filepath.Join(m.root, entry.Name()))
			}
		}
	}
}

func (m *Manager) Invalidate(file storage.File) {
	key := cacheKey(file)
	m.mu.Lock()
	items := m.segments[key]
	delete(m.segments, key)
	for _, item := range items {
		item.mu.Lock()
		item.removed = true
		readers := item.readers
		item.mu.Unlock()
		if readers == 0 {
			_ = os.Remove(item.filePath)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) invalidateOtherVersions(fileName, currentKey string) {
	if fileName == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, items := range m.segments {
		if key == currentKey {
			continue
		}
		for _, item := range items {
			item.mu.Lock()
			matches := item.fileName == fileName
			item.mu.Unlock()
			if matches {
				m.removeSegmentLocked(item)
			}
		}
		if len(m.segments[key]) == 0 {
			delete(m.segments, key)
		}
	}
}

func (m *Manager) cleanup() {
	now := time.Now()
	m.mu.Lock()
	for key, items := range m.segments {
		for _, item := range items {
			if m.ttl > 0 && now.Sub(item.accessTime()) >= m.ttl && item.readerCount() == 0 {
				m.removeSegmentLocked(item)
			}
		}
		if len(m.segments[key]) == 0 {
			delete(m.segments, key)
		}
	}
	m.mu.Unlock()
}

func (s *segment) snapshot() (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size, s.complete
}

func (s *segment) snapshotState() (time.Time, int64, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccess, s.size, s.readers, s.removed
}

func (s *segment) accessTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccess
}

func (s *segment) readerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readers
}

func Key(file storage.File) string {
	value := strings.Join([]string{file.Name, strconv.FormatInt(file.ChatID, 10), strconv.Itoa(file.MessageID), strconv.FormatInt(file.Size, 10), strconv.FormatInt(file.EncryptedSize, 10), file.UpdatedAt.UTC().Format(time.RFC3339Nano)}, "|")
	return hashKey(value)
}

func cacheKey(file storage.File) string {
	return Key(file)
}

func hashKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ParseMaxSize(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("cache size is empty")
	}
	unit := value[len(value)-1]
	if unit != 'M' && unit != 'G' {
		return 0, errors.New("cache size must use M or G")
	}
	amount, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	if err != nil || amount <= 0 {
		return 0, errors.New("cache size must be a positive integer")
	}
	multiplier := int64(1024 * 1024)
	if unit == 'G' {
		multiplier *= 1024
	}
	if amount > int64(64<<30)/multiplier {
		return 0, errors.New("cache size is too large")
	}
	return amount * multiplier, nil
}
