package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"tgdrive/internal/cache"
	"tgdrive/internal/httpapi"
	"tgdrive/internal/storage"
	tgtelegram "tgdrive/internal/telegram"
)

type Config struct {
	Token      string
	Hash       string
	APIID      int
	ChatID     int64
	Key        string
	Listen     string
	HTTPToken  string
	Index      string
	LocalIndex bool
	Session    string
	MaxCache   int64
	CacheTTL   time.Duration
	Debug      bool
}

type Service struct {
	cancel context.CancelFunc
	ready  chan startResult
	done   chan struct{}

	mu      sync.RWMutex
	port    int
	runErr  error
	finish  sync.Once
	closeMu sync.Once
}

type startResult struct {
	port int
	err  error
}

const (
	telegramPrimaryDC          = 2
	remoteIndexRefreshInterval = 30 * time.Second
	mediaPoolTimeout           = 5 * time.Second
)

func Start(parent context.Context, cfg Config) (*Service, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.LocalIndex {
		if err := os.MkdirAll(filepath.Dir(cfg.Index), 0o700); err != nil {
			return nil, fmt.Errorf("create index directory: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Session), 0o700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	service := &Service{
		cancel: cancel,
		ready:  make(chan startResult, 1),
		done:   make(chan struct{}),
	}
	go service.run(ctx, cfg)

	select {
	case result := <-service.ready:
		if result.err != nil {
			service.cancel()
			service.Wait()
			return nil, result.err
		}
		return service, nil
	case <-service.done:
		return nil, service.Err()
	case <-parent.Done():
		service.cancel()
		service.Wait()
		return nil, parent.Err()
	}
}

func (s *Service) run(ctx context.Context, cfg Config) {
	defer s.finishRun(nil)

	index := storage.New("")
	if cfg.LocalIndex {
		openedIndex, openErr := storage.Open(cfg.Index)
		if openErr != nil {
			s.finishRun(fmt.Errorf("open local index: %w", openErr))
			return
		}
		index = openedIndex
	}

	cacheRoot := filepath.Join(filepath.Dir(cfg.Session), ".tgdrive-cache")
	cacheManager, err := cache.New(cacheRoot, streamCacheSize(cfg.MaxCache), cfg.CacheTTL)
	if err != nil {
		s.finishRun(err)
		return
	}
	cacheManager.Start(ctx)
	// Previous versions persisted placeholder PNGs after transient Telegram
	// failures. Discard that generation once so thumbnails can recover.
	if err := os.RemoveAll(filepath.Join(cacheRoot, "thumbnails")); err != nil {
		s.finishRun(fmt.Errorf("remove stale thumbnail cache: %w", err))
		return
	}
	thumbnailCache, err := cache.NewThumbnailCache(
		filepath.Join(cacheRoot, "thumbnails-v2"),
		thumbnailCacheSize(cfg.MaxCache),
		cfg.CacheTTL,
	)
	if err != nil {
		s.finishRun(err)
		return
	}
	thumbnailCache.Start(ctx)

	peer := tgtelegram.NewPeerStore(cfg.ChatID)
	dispatcher := tg.NewUpdateDispatcher()
	dispatcher.OnNewMessage(func(_ context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		if captured, ok := tgtelegram.CapturePeer(update, entities); ok && peer.Matches(captured) {
			peer.Set(captured)
			log.Printf("Telegram storage chat connected")
		}
		return nil
	})

	client := telegram.NewClient(cfg.APIID, cfg.Hash, telegram.Options{
		DC:             telegramPrimaryDC,
		UpdateHandler:  dispatcher,
		SessionStorage: &session.FileStorage{Path: cfg.Session},
	})
	clientErr := client.Run(ctx, func(clientCtx context.Context) error {
		if status, statusErr := client.Auth().Status(clientCtx); statusErr != nil {
			return statusErr
		} else if !status.Authorized {
			if _, authErr := client.Auth().Bot(clientCtx, cfg.Token); authErr != nil {
				return authErr
			}
		}

		resolved, resolveErr := tgtelegram.ResolveChatID(clientCtx, client.API(), cfg.ChatID)
		if resolveErr != nil {
			return resolveErr
		}
		peer.Set(resolved)
		log.Printf("Using Telegram chat %d", cfg.ChatID)

		store := tgtelegram.NewStore(client.API(), peer, cfg.Key)
		store.Debug = cfg.Debug
		store.SetMediaAPIProvider(func(ctx context.Context, dcID int) (*tg.Client, io.Closer, error) {
			telegramConfig := client.Config()
			mediaCtx, mediaCancel := context.WithTimeout(ctx, mediaPoolTimeout)
			defer mediaCancel()
			if telegramConfig.ThisDC == dcID {
				pool, poolErr := client.Pool(4)
				if poolErr != nil {
					return nil, nil, poolErr
				}
				return tg.NewClient(pool), pool, nil
			}

			mediaAvailable := false
			for _, option := range telegramConfig.DCOptions {
				if option.ID == dcID && option.MediaOnly && !option.CDN {
					mediaAvailable = true
					break
				}
			}
			if !mediaAvailable {
				return nil, nil, fmt.Errorf("no media-only route for dc=%d", dcID)
			}
			mediaInvoker, mediaErr := client.MediaOnly(mediaCtx, dcID, 4)
			if mediaErr != nil {
				return nil, nil, mediaErr
			}
			return tg.NewClient(mediaInvoker), mediaInvoker, nil
		})
		defer func() { _ = store.Close() }()

		backup, found, backupErr := store.RestoreIndex(clientCtx)
		if backupErr != nil {
			return fmt.Errorf("restore Telegram index: %w", backupErr)
		}
		if found {
			if restoreErr := index.Restore(backup); restoreErr != nil {
				return fmt.Errorf("restore Telegram index data: %w", restoreErr)
			}
		} else {
			log.Printf("no Telegram index backup; creating an empty remote index")
			snapshot, snapshotErr := index.Snapshot()
			if snapshotErr != nil {
				return fmt.Errorf("create initial index snapshot: %w", snapshotErr)
			}
			if backupErr := store.BackupIndex(clientCtx, snapshot); backupErr != nil {
				return fmt.Errorf("create initial Telegram index backup: %w", backupErr)
			}
		}

		cacheManager.Debug = cfg.Debug
		backend := &backend{
			index:             index,
			store:             store,
			cache:             cacheManager,
			thumbnails:        thumbnailCache,
			lastRemoteRefresh: time.Now(),
		}
		apiServer := httpapi.NewServer(backend, cfg.HTTPToken, log.Default(), cfg.Debug)
		httpServer := &http.Server{
			Handler:           apiServer,
			ReadHeaderTimeout: 15 * time.Second,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    32 * 1024,
		}
		listener, listenErr := net.Listen("tcp", cfg.Listen)
		if listenErr != nil {
			return listenErr
		}
		port := listener.Addr().(*net.TCPAddr).Port
		s.mu.Lock()
		s.port = port
		s.mu.Unlock()

		serveDone := make(chan error, 1)
		go func() { serveDone <- httpServer.Serve(listener) }()
		s.ready <- startResult{port: port}

		select {
		case <-clientCtx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			_ = httpServer.Shutdown(shutdownCtx)
			return nil
		case serveErr := <-serveDone:
			if errors.Is(serveErr, http.ErrServerClosed) {
				return nil
			}
			return serveErr
		}
	})
	if clientErr != nil && !errors.Is(clientErr, context.Canceled) {
		s.finishRun(clientErr)
		return
	}
	s.finishRun(nil)
}

func (s *Service) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

func (s *Service) Wait() error {
	<-s.done
	return s.Err()
}

func (s *Service) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runErr
}

func (s *Service) Close() error {
	s.closeMu.Do(func() { s.cancel() })
	return s.Wait()
}

func (s *Service) finishRun(err error) {
	s.finish.Do(func() {
		s.mu.Lock()
		s.runErr = err
		s.mu.Unlock()
		close(s.done)
	})
}

type backend struct {
	index             *storage.Index
	store             *tgtelegram.Store
	cache             *cache.Manager
	thumbnails        *cache.ThumbnailCache
	mutationMu        sync.Mutex
	lastRemoteRefresh time.Time
}

func (b *backend) Refresh(ctx context.Context) error {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if time.Since(b.lastRemoteRefresh) < remoteIndexRefreshInterval {
		return nil
	}
	return b.refreshLocked(ctx)
}

func (b *backend) refreshLocked(ctx context.Context) error {
	backup, found, err := b.store.RestoreIndex(ctx)
	if err != nil {
		return fmt.Errorf("refresh Telegram index: %w", err)
	}
	if !found {
		return errors.New("Telegram index backup not found")
	}
	if err := b.index.Restore(backup); err != nil {
		return fmt.Errorf("apply Telegram index: %w", err)
	}
	b.lastRemoteRefresh = time.Now()
	return nil
}

func (b *backend) Entries(directory string) []storage.Entry { return b.index.Entries(directory) }
func (b *backend) List() []storage.File                     { return b.index.List() }
func (b *backend) DirectoryPaths() []string                 { return b.index.DirectoryPaths() }
func (b *backend) IsDir(directory string) bool              { return b.index.IsDir(directory) }
func (b *backend) Get(name string) (storage.File, bool)     { return b.index.Get(name) }

func (b *backend) Put(ctx context.Context, name string, input io.Reader, size int64) (storage.File, error) {
	return b.put(ctx, name, input, size, nil)
}

func (b *backend) PutWithProgress(
	ctx context.Context,
	name string,
	input io.Reader,
	size int64,
	onProgress func(uploadedBytes, totalBytes int64) error,
) (storage.File, error) {
	return b.put(ctx, name, input, size, onProgress)
}

func (b *backend) put(
	ctx context.Context,
	name string,
	input io.Reader,
	size int64,
	onProgress func(uploadedBytes, totalBytes int64) error,
) (storage.File, error) {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := b.refreshLocked(ctx); err != nil {
		return storage.File{}, err
	}

	oldFile, hadOldFile := b.index.Get(name)
	hasher := sha256.New()
	hashedInput := io.TeeReader(input, hasher)
	var file storage.File
	var err error
	if onProgress == nil {
		file, err = b.store.Put(ctx, name, hashedInput, size)
	} else {
		file, err = b.store.PutWithProgress(ctx, name, hashedInput, size, onProgress)
	}
	if err != nil {
		return storage.File{}, err
	}
	file.ContentHash = hex.EncodeToString(hasher.Sum(nil))
	if hadOldFile {
		if err := b.store.Delete(ctx, oldFile); err != nil {
			return storage.File{}, fmt.Errorf("replace old Telegram file: %w", err)
		}
		b.invalidateCache(oldFile)
	}
	if err := b.index.Put(file); err != nil {
		return storage.File{}, err
	}
	if err := b.backup(); err != nil {
		return file, err
	}
	return file, nil
}

func (b *backend) OpenRange(ctx context.Context, file storage.File, start int64) (io.ReadCloser, error) {
	return b.cache.Open(ctx, file, start, func(sourceCtx context.Context, offset int64) (io.ReadCloser, error) {
		return b.store.OpenRange(sourceCtx, file, offset)
	})
}

func (b *backend) invalidateCache(file storage.File) {
	b.cache.Invalidate(file)
	b.thumbnails.Invalidate(file)
}

func (b *backend) ClearCache() {
	b.cache.Clear()
	b.thumbnails.Clear()
}

func (b *backend) GetThumbnail(file storage.File) ([]byte, bool) {
	return b.thumbnails.Get(file)
}

func (b *backend) PutThumbnail(file storage.File, content []byte) {
	b.thumbnails.Put(file, content)
}

func (b *backend) InvalidateThumbnail(file storage.File) {
	b.thumbnails.Invalidate(file)
}

func (b *backend) Delete(ctx context.Context, file storage.File) error {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := b.refreshLocked(ctx); err != nil {
		return err
	}
	if err := b.store.Delete(ctx, file); err != nil {
		return err
	}
	if err := b.index.Delete(file.Name); err != nil {
		return err
	}
	b.invalidateCache(file)
	return b.backup()
}

func (b *backend) Mkdir(directory string) error {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := b.refreshLocked(context.Background()); err != nil {
		return err
	}
	if err := b.index.Mkdir(directory); err != nil {
		return err
	}
	return b.backup()
}

func (b *backend) RemoveDir(ctx context.Context, directory string) error {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := b.refreshLocked(ctx); err != nil {
		return err
	}
	if !b.index.IsDir(directory) {
		return errors.New("directory not found")
	}

	prefix := directory + "/"
	files := b.index.List()
	for _, file := range files {
		if file.Name != directory && !strings.HasPrefix(file.Name, prefix) {
			continue
		}
		if err := b.store.Delete(ctx, file); err != nil {
			return fmt.Errorf("delete %s from Telegram: %w", file.Name, err)
		}
		b.invalidateCache(file)
	}
	if err := b.index.RemoveDirRecursive(directory); err != nil {
		return err
	}
	return b.backup()
}

func (b *backend) Rename(oldName, newName string) error {
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := b.refreshLocked(context.Background()); err != nil {
		return err
	}
	oldFile, hadFile := b.index.Get(oldName)
	if err := b.index.Rename(oldName, newName); err != nil {
		return err
	}
	if hadFile {
		b.invalidateCache(oldFile)
	}
	return b.backup()
}

func (b *backend) backup() error {
	snapshot, err := b.index.Snapshot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return b.store.BackupIndex(ctx, snapshot)
}

func thumbnailCacheSize(maxBytes int64) int64 {
	const minimum = int64(64 * 1024)
	size := maxBytes / 8
	if size < minimum {
		size = minimum
	}
	if size >= maxBytes {
		return maxBytes / 2
	}
	return size
}

func streamCacheSize(maxBytes int64) int64 {
	return maxBytes - thumbnailCacheSize(maxBytes)
}

func validateConfig(cfg Config) error {
	if cfg.Token == "" || cfg.Hash == "" || cfg.Key == "" || cfg.APIID <= 0 || cfg.ChatID == 0 {
		return errors.New("Telegram config is incomplete")
	}
	if cfg.Listen == "" || cfg.Index == "" || cfg.Session == "" {
		return errors.New("service paths are incomplete")
	}
	if cfg.MaxCache <= 0 || cfg.CacheTTL < 0 {
		return errors.New("cache config is invalid")
	}
	return nil
}
