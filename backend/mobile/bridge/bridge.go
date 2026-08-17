package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tgdrive/internal/cache"
	"tgdrive/internal/service"
)

type telegramConfig struct {
	BotToken      string `json:"bot_token"`
	APIID         int    `json:"api_id"`
	APIHash       string `json:"api_hash"`
	ChatID        int64  `json:"chat_id"`
	EncryptionKey string `json:"encryption_key"`
	MaxCacheMB    *int   `json:"max_cache_mb"`
	CacheTTLHours *int   `json:"cache_ttl_hours"`
}

var runtime = struct {
	sync.Mutex
	service *service.Service
	cancel  context.CancelFunc
}{}

// Start starts the Go Telegram gateway on a loopback port and returns that port.
func Start(configJSON, dataDir string) (int, error) {
	runtime.Lock()
	defer runtime.Unlock()
	if runtime.service != nil {
		return runtime.service.Port(), nil
	}

	var telegram telegramConfig
	if err := json.Unmarshal([]byte(configJSON), &telegram); err != nil {
		return 0, err
	}
	if telegram.BotToken == "" || telegram.APIHash == "" || telegram.EncryptionKey == "" || telegram.APIID <= 0 || telegram.ChatID == 0 {
		return 0, errors.New("Telegram config is incomplete")
	}
	if dataDir == "" {
		return 0, errors.New("application data directory is empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return 0, err
	}
	sessionPath := filepath.Join(dataDir, "tgdrive.session")
	if err := resetSessionIfAuthChanged(sessionPath, telegram); err != nil {
		return 0, err
	}

	maxCacheMB := 500
	if telegram.MaxCacheMB != nil && *telegram.MaxCacheMB > 0 {
		maxCacheMB = *telegram.MaxCacheMB
	}
	cacheTTLHours := 168
	if telegram.CacheTTLHours != nil {
		cacheTTLHours = *telegram.CacheTTLHours
	}
	if cacheTTLHours < 0 {
		return 0, errors.New("cache TTL cannot be negative")
	}
	maxCache, err := cache.ParseMaxSize(fmt.Sprintf("%dM", maxCacheMB))
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	instance, err := service.Start(ctx, service.Config{
		Token:      telegram.BotToken,
		Hash:       telegram.APIHash,
		APIID:      telegram.APIID,
		ChatID:     telegram.ChatID,
		Key:        telegram.EncryptionKey,
		Listen:     "127.0.0.1:0",
		Index:      filepath.Join(dataDir, "tgdrive-index.json"),
		LocalIndex: false,
		Session:    sessionPath,
		MaxCache:   maxCache,
		CacheTTL:   time.Duration(cacheTTLHours) * time.Hour,
	})
	if err != nil {
		cancel()
		return 0, err
	}
	runtime.service = instance
	runtime.cancel = cancel
	return instance.Port(), nil
}

// Stop stops the embedded gateway.
func Stop() error {
	runtime.Lock()
	instance := runtime.service
	cancel := runtime.cancel
	runtime.service = nil
	runtime.cancel = nil
	runtime.Unlock()
	if cancel != nil {
		cancel()
	}
	if instance == nil {
		return nil
	}
	return instance.Wait()
}

// Port returns the active loopback port, or zero when the gateway is stopped.
func resetSessionIfAuthChanged(sessionPath string, config telegramConfig) error {
	markerPath := sessionPath + ".auth"
	fingerprint := sessionFingerprint(config)
	previous, err := os.ReadFile(markerPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read MTProto session marker: %w", err)
	}
	if string(previous) == fingerprint {
		return nil
	}
	if err := os.Remove(sessionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous MTProto session: %w", err)
	}
	markerTempPath := markerPath + ".tmp"
	if err := os.WriteFile(markerTempPath, []byte(fingerprint), 0o600); err != nil {
		return fmt.Errorf("write MTProto session marker: %w", err)
	}
	if err := os.Rename(markerTempPath, markerPath); err != nil {
		return fmt.Errorf("save MTProto session marker: %w", err)
	}
	return nil
}

func sessionFingerprint(config telegramConfig) string {
	value := fmt.Sprintf("%d\x00%s\x00%s", config.APIID, config.APIHash, config.BotToken)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func Port() int {
	runtime.Lock()
	defer runtime.Unlock()
	if runtime.service == nil {
		return 0
	}
	return runtime.service.Port()
}
