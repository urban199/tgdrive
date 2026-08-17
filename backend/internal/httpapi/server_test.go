package httpapi

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		size    int64
		start   int64
		end     int64
		partial bool
		failed  bool
	}{
		{name: "full", size: 100, start: 0, end: 99},
		{name: "open ended", header: "bytes=10-", size: 100, start: 10, end: 99, partial: true},
		{name: "bounded", header: "bytes=10-19", size: 100, start: 10, end: 19, partial: true},
		{name: "suffix", header: "bytes=-10", size: 100, start: 90, end: 99, partial: true},
		{name: "clamped", header: "bytes=90-120", size: 100, start: 90, end: 99, partial: true},
		{name: "invalid start", header: "bytes=100-", size: 100, failed: true},
		{name: "invalid unit", header: "items=0-1", size: 100, failed: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, partial, err := parseRange(test.header, test.size)
			if test.failed {
				if err == nil {
					t.Fatal("expected range error")
				}
				return
			}
			if err != nil || start != test.start || end != test.end || partial != test.partial {
				t.Fatalf("got start=%d end=%d partial=%v err=%v", start, end, partial, err)
			}
		})
	}
}

func TestImageThumbnail(t *testing.T) {
	thumbnail, err := imageThumbnail(bytes.NewBufferString("not-an-image"), 512)
	if err == nil || thumbnail != nil {
		t.Fatal("invalid image should fail thumbnail generation")
	}
}

func TestJoinPath(t *testing.T) {
	if got := joinPath("/", "photo.jpg"); got != "photo.jpg" {
		t.Fatalf("root join = %q", got)
	}
	if got := joinPath("backup/2026", "photo.jpg"); got != "backup/2026/photo.jpg" {
		t.Fatalf("nested join = %q", got)
	}
}

func TestVideoExtensionsSupportThumbnails(t *testing.T) {
	for _, name := range []string{"clip.mp4", "clip.mkv", "clip.webm", "clip.mov"} {
		if !supportsThumbnail(name) {
			t.Fatalf("thumbnail is not supported for %s", name)
		}
	}
}

func TestUploadControlPausesAndResumes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control := newUploadControl(cancel)
	control.pause()

	waitDone := make(chan error, 1)
	go func() { waitDone <- control.wait(ctx) }()
	select {
	case err := <-waitDone:
		t.Fatalf("paused upload continued: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	control.resume()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("resume failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("paused upload did not resume")
	}
}

func TestUploadProgressIsMonotonic(t *testing.T) {
	server := NewServer(nil, "", nil, false)
	server.setUploadProgress("upload-1", 0, 1_000, false, "")
	server.setUploadProgress("upload-1", 700, 1_000, false, "")
	server.setUploadProgress("upload-1", 400, 1_000, false, "")

	progress, ok := server.getUploadProgress("upload-1")
	if !ok {
		t.Fatal("upload progress not found")
	}
	if progress.UploadedBytes != 700 || progress.TotalBytes != 1_000 || progress.Complete {
		t.Fatalf("unexpected progress: %+v", progress)
	}

	server.finishUploadProgress("upload-1", nil)
	progress, ok = server.getUploadProgress("upload-1")
	if !ok || !progress.Complete {
		t.Fatalf("upload should be complete: %+v", progress)
	}
}
