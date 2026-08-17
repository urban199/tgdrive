package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tgdrive/internal/storage"
)

func TestParseMaxSize(t *testing.T) {
	cases := map[string]int64{"500M": 500 * 1024 * 1024, "1G": 1024 * 1024 * 1024}
	for input, want := range cases {
		got, err := ParseMaxSize(input)
		if err != nil || got != want {
			t.Errorf("ParseMaxSize(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "1", "1MB", "0M", "1.5G", "65G"} {
		if _, err := ParseMaxSize(input); err == nil {
			t.Errorf("ParseMaxSize(%q) should fail", input)
		}
	}
}

func TestCachePersistsWhenReaderStopsAtExactFileSize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	file := storage.File{Name: "exact.bin", Size: 5, MessageID: 9}
	manager, err := New(root, 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("12345")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(reader, buffer); err != nil || string(buffer) != "12345" {
		t.Fatalf("exact read = %q, %v", buffer, err)
	}
	_ = reader.Close()

	manager, err = New(root, 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err = manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		t.Fatal("exact-size cache was not persisted")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(content) != "12345" {
		t.Fatalf("persisted exact read = %q, %v", content, err)
	}
}

func TestCachePersistsAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	file := storage.File{Name: "movie.mp4", Size: 11, MessageID: 7, UpdatedAt: time.Unix(2, 0)}
	manager, err := New(root, 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("hello-cache")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if content, err := io.ReadAll(reader); err != nil || string(content) != "hello-cache" {
		t.Fatalf("initial cache read = %q, %v", content, err)
	}
	_ = reader.Close()

	manager, err = New(root, 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err = manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		t.Fatal("cache should survive restart")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(content) != "hello-cache" {
		t.Fatalf("persistent cache read = %q, %v", content, err)
	}
}

func TestPartialStreamContinuesCachingAfterClose(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "cache"), 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	file := storage.File{Name: "movie.mp4", Size: 11, MessageID: 7, UpdatedAt: time.Unix(2, 0)}
	reader, err := manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("hello-cache")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	partial := make([]byte, 5)
	if _, err := io.ReadFull(reader, partial); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	manager, err = New(filepath.Join(filepath.Dir(manager.root), "cache"), 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := manager.Open(context.Background(), file, 0, func(_ context.Context, offset int64) (io.ReadCloser, error) {
		if offset != 5 {
			return nil, errors.New("unexpected continuation offset")
		}
		return io.NopCloser(strings.NewReader("-cache")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(cached)
	_ = cached.Close()
	if err != nil || string(content) != "hello-cache" {
		t.Fatalf("partial cache read = %q, %v", content, err)
	}
}

func TestLargeFileKeepsCachedWindowWithinLimit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	file := storage.File{Name: "large.bin", Size: 9, MessageID: 10}
	manager, err := New(root, 5, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("123456789")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, 3)
	if _, err := io.ReadFull(reader, first); err != nil || string(first) != "123" {
		t.Fatalf("large-file prefix = %q, %v", first, err)
	}
	_ = reader.Close()

	manager, err = New(root, 5, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err = manager.Open(context.Background(), file, 0, func(_ context.Context, offset int64) (io.ReadCloser, error) {
		if offset != 3 {
			return nil, errors.New("large-file cache did not resume at cached boundary")
		}
		return io.NopCloser(strings.NewReader("456789")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(content) != "123456789" {
		t.Fatalf("large-file content = %q, %v", content, err)
	}
}

func TestActiveSegmentIsNotEvicted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	manager, err := New(root, 5, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	firstFile := storage.File{Name: "first", Size: 5, MessageID: 1}
	firstReader, err := manager.Open(context.Background(), firstFile, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("first")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if content, err := io.ReadAll(firstReader); err != nil || string(content) != "first" {
		t.Fatalf("first read = %q, %v", content, err)
	}

	secondFile := storage.File{Name: "second", Size: 5, MessageID: 2}
	secondReader, err := manager.Open(context.Background(), secondFile, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("secon")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(secondReader); err != nil {
		t.Fatal(err)
	}
	_ = secondReader.Close()
	_ = firstReader.Close()

	matches, err := filepath.Glob(filepath.Join(root, cacheKey(firstFile)+"-0.cache"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("active segment was evicted: %v", matches)
	}
}

func TestClearRemovesCompletedCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	manager, err := New(root, 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	file := storage.File{Name: "video.mp4", Size: 5, MessageID: 42, UpdatedAt: time.Unix(1, 0)}
	reader, err := manager.Open(context.Background(), file, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("video")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()

	manager.Clear()
	matches, err := filepath.Glob(filepath.Join(root, "*.cache"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("cache files remain after clear: %v", matches)
	}
}

func TestSameNameReplacementUsesNewCacheIdentity(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "cache"), 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	oldFile := storage.File{Name: "same.txt", Size: 3, MessageID: 1}
	newFile := storage.File{Name: "same.txt", Size: 3, MessageID: 2}
	oldReader, err := manager.Open(context.Background(), oldFile, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("old")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(oldReader)
	_ = oldReader.Close()
	newReader, err := manager.Open(context.Background(), newFile, 0, func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("new")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(newReader)
	_ = newReader.Close()
	if readErr != nil || string(content) != "new" {
		t.Fatalf("replacement content = %q, %v", content, readErr)
	}
	if _, err := os.Stat(filepath.Join(manager.root, cacheKey(oldFile)+"-0.cache")); !os.IsNotExist(err) {
		t.Fatalf("old cache identity remains: %v", err)
	}
}

func TestThumbnailCachePersistsAndInvalidatesReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "thumbnails")
	oldFile := storage.File{Name: "same.jpg", Size: 3, MessageID: 1}
	newFile := storage.File{Name: "same.jpg", Size: 3, MessageID: 2}
	thumbnail, err := NewThumbnailCache(root, 1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	thumbnail.Put(oldFile, []byte("old-thumb"))
	thumbnail, err = NewThumbnailCache(root, 1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	content, ok := thumbnail.Get(oldFile)
	if !ok || string(content) != "old-thumb" {
		t.Fatalf("persistent thumbnail = %q, %v", content, ok)
	}
	thumbnail.Put(newFile, []byte("new-thumb"))
	thumbnail.Invalidate(oldFile)
	if _, ok := thumbnail.Get(oldFile); ok {
		t.Fatal("old thumbnail identity was not invalidated")
	}
	content, ok = thumbnail.Get(newFile)
	if !ok || string(content) != "new-thumb" {
		t.Fatalf("replacement thumbnail = %q, %v", content, ok)
	}
}

func TestCacheHitAndInvalidation(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "cache"), 10*1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	file := storage.File{Name: "video.mp4", Size: 11, MessageID: 42, UpdatedAt: time.Unix(1, 0)}
	sourceCalls := 0
	source := func(context.Context, int64) (io.ReadCloser, error) {
		sourceCalls++
		return io.NopCloser(strings.NewReader("hello-cache")), nil
	}
	reader, err := manager.Open(context.Background(), file, 0, source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(first) != "hello-cache" {
		t.Fatalf("first read = %q, %v", first, err)
	}
	reader, err = manager.Open(context.Background(), file, 6, func(context.Context, int64) (io.ReadCloser, error) { t.Fatal("cache miss"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(second) != "cache" {
		t.Fatalf("cached seek = %q, %v", second, err)
	}
	if sourceCalls != 1 {
		t.Fatalf("source called %d times, want 1", sourceCalls)
	}
	manager.Invalidate(file)
	if _, err := manager.Open(context.Background(), file, 0, source); err != nil {
		t.Fatal(err)
	}
	if sourceCalls != 2 {
		t.Fatalf("source calls after invalidation = %d, want 2", sourceCalls)
	}
}
