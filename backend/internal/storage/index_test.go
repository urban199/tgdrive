package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIndexPersistsAndSorts(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "nested", "index.json")
	index, err := Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	updated := time.Unix(10, 0).UTC()
	if err := index.Put(File{Name: "z.txt", Size: 2, ContentHash: "abc123", UpdatedAt: updated}); err != nil {
		t.Fatal(err)
	}
	if err := index.Put(File{Name: "a.txt", Size: 1, UpdatedAt: updated}); err != nil {
		t.Fatal(err)
	}
	files := index.List()
	if len(files) != 2 || files[0].Name != "a.txt" {
		t.Fatalf("unexpected order: %#v", files)
	}
	reloaded, err := Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := reloaded.Get("z.txt")
	if !ok || file.Size != 2 || file.ContentHash != "abc123" {
		t.Fatalf("reloaded file missing: %#v", file)
	}
	if err := reloaded.Delete("z.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("z.txt"); ok {
		t.Fatal("file was not deleted")
	}
}

func TestMemoryIndexDoesNotPersist(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "should-not-exist.json")
	index := New("")
	if err := index.Put(File{Name: "remote.txt", Size: 7}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("remote.txt"); ok {
		t.Fatal("memory index wrote data to disk")
	}
	if _, ok := index.Get("remote.txt"); !ok {
		t.Fatal("memory index did not keep the file")
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	index, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Put(File{Name: "video.mp4", Size: 42}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := index.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := New(filepath.Join(t.TempDir(), "restored.json"))
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if file, ok := restored.Get("video.mp4"); !ok || file.Size != 42 {
		t.Fatalf("restored file: %#v, %v", file, ok)
	}
}

func TestRemoveDirRecursive(t *testing.T) {
	index := New("")
	if err := index.Mkdir("archive/photos"); err != nil {
		t.Fatal(err)
	}
	if err := index.Put(File{Name: "archive/photos/one.jpg"}); err != nil {
		t.Fatal(err)
	}
	if err := index.Put(File{Name: "archive/two.jpg"}); err != nil {
		t.Fatal(err)
	}
	if err := index.RemoveDirRecursive("archive"); err != nil {
		t.Fatal(err)
	}
	if index.IsDir("archive") || len(index.List()) != 0 {
		t.Fatal("recursive directory removal left entries")
	}
}

func TestIndexRejectsFileDirectoryConflicts(t *testing.T) {
	index := New("")
	if err := index.Put(File{Name: "archive"}); err != nil {
		t.Fatal(err)
	}
	if err := index.Mkdir("archive/photos"); err == nil {
		t.Fatal("file parent should not accept a directory")
	}
	if err := index.Put(File{Name: "archive/photos/image.jpg"}); err == nil {
		t.Fatal("file parent should not accept a child file")
	}
}

func TestRenameRejectsMovingDirectoryInsideItself(t *testing.T) {
	index := New("")
	if err := index.Mkdir("archive/photos"); err != nil {
		t.Fatal(err)
	}
	if err := index.Rename("archive", "archive/photos/nested"); err == nil {
		t.Fatal("directory should not move inside itself")
	}
}

func TestDirectoriesAndRename(t *testing.T) {
	index, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Mkdir("music/live"); err != nil {
		t.Fatal(err)
	}
	if err := index.Put(File{Name: "music/live/song.mp3", Size: 10}); err != nil {
		t.Fatal(err)
	}
	if !index.IsDir("music") || !index.IsDir("music/live") {
		t.Fatal("implicit directory missing")
	}
	entries := index.Entries("music")
	if len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "live" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if err := index.Rename("music", "archive"); err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Get("archive/live/song.mp3"); !ok {
		t.Fatal("file was not moved with directory")
	}
	if err := index.RemoveDir("archive/live"); err == nil {
		t.Fatal("non-empty directory was removed")
	}
}
