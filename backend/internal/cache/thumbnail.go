package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tgdrive/internal/storage"
)

// ThumbnailCache stores generated thumbnails separately from streamed file ranges.
// Its identity uses the Telegram message identity, so replacing a file invalidates the old thumbnail.
type ThumbnailCache struct {
	root     string
	maxBytes int64
	ttl      time.Duration
	mu       sync.Mutex
	items    map[string]thumbnailItem
}

type thumbnailItem struct {
	size       int64
	lastAccess time.Time
}

func NewThumbnailCache(root string, maxBytes int64, ttl time.Duration) (*ThumbnailCache, error) {
	if maxBytes <= 0 {
		return nil, errors.New("thumbnail cache size must be positive")
	}
	if ttl < 0 {
		return nil, errors.New("thumbnail cache TTL cannot be negative")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create thumbnail cache directory: %w", err)
	}
	cache := &ThumbnailCache{
		root:     root,
		maxBytes: maxBytes,
		ttl:      ttl,
		items:    make(map[string]thumbnailItem),
	}
	if err := cache.load(); err != nil {
		return nil, err
	}
	cache.trimLocked()
	return cache, nil
}

func (c *ThumbnailCache) Start(ctx context.Context) {
	if c.ttl == 0 {
		return
	}
	interval := c.ttl / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *ThumbnailCache) Get(file storage.File) ([]byte, bool) {
	key := Key(file)
	c.mu.Lock()
	item, ok := c.items[key]
	if !ok || c.expired(item.lastAccess) {
		if ok {
			c.removeLocked(key)
		}
		c.mu.Unlock()
		return nil, false
	}
	c.mu.Unlock()

	content, err := os.ReadFile(filepath.Join(c.root, key+".thumb"))
	if err != nil {
		c.Invalidate(file)
		return nil, false
	}
	c.mu.Lock()
	if current, exists := c.items[key]; exists {
		current.lastAccess = time.Now()
		c.items[key] = current
	}
	c.mu.Unlock()
	return content, true
}

func (c *ThumbnailCache) Put(file storage.File, content []byte) {
	if len(content) == 0 || int64(len(content)) > c.maxBytes {
		return
	}
	key := Key(file)
	temporary, err := os.CreateTemp(c.root, ".thumbnail-*.tmp")
	if err != nil {
		return
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(content); err != nil {
		return
	}
	if err := temporary.Sync(); err != nil {
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	finalPath := filepath.Join(c.root, key+".thumb")
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(c.root, key+".complete"), nil, 0o600); err != nil {
		_ = os.Remove(finalPath)
		return
	}

	c.mu.Lock()
	c.items[key] = thumbnailItem{size: int64(len(content)), lastAccess: time.Now()}
	c.trimLocked()
	c.mu.Unlock()
}

func (c *ThumbnailCache) Invalidate(file storage.File) {
	key := Key(file)
	c.mu.Lock()
	c.removeLocked(key)
	c.mu.Unlock()
}

func (c *ThumbnailCache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]thumbnailItem)
	c.mu.Unlock()
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			_ = os.Remove(filepath.Join(c.root, entry.Name()))
		}
	}
}

func (c *ThumbnailCache) load() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return fmt.Errorf("read thumbnail cache directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".thumb") {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".thumb")
		if _, err := os.Stat(filepath.Join(c.root, key+".complete")); err != nil {
			_ = os.Remove(filepath.Join(c.root, entry.Name()))
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 {
			_ = os.Remove(filepath.Join(c.root, entry.Name()))
			_ = os.Remove(filepath.Join(c.root, key+".complete"))
			continue
		}
		c.items[key] = thumbnailItem{size: info.Size(), lastAccess: info.ModTime()}
	}
	return nil
}

func (c *ThumbnailCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, item := range c.items {
		if c.expired(item.lastAccess) {
			c.removeLocked(key)
		}
	}
}

func (c *ThumbnailCache) trimLocked() {
	var total int64
	candidates := make([]string, 0, len(c.items))
	for key, item := range c.items {
		total += item.size
		candidates = append(candidates, key)
	}
	if total <= c.maxBytes {
		return
	}
	sort.Slice(candidates, func(i, j int) bool {
		return c.items[candidates[i]].lastAccess.Before(c.items[candidates[j]].lastAccess)
	})
	for _, key := range candidates {
		if total <= c.maxBytes {
			return
		}
		total -= c.items[key].size
		c.removeLocked(key)
	}
}

func (c *ThumbnailCache) removeLocked(key string) {
	delete(c.items, key)
	_ = os.Remove(filepath.Join(c.root, key+".thumb"))
	_ = os.Remove(filepath.Join(c.root, key+".complete"))
}

func (c *ThumbnailCache) expired(lastAccess time.Time) bool {
	return c.ttl > 0 && time.Since(lastAccess) >= c.ttl
}
