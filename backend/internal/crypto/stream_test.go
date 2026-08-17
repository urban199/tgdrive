package crypto

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type shortWriter struct {
	buffer bytes.Buffer
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > 3 {
		data = data[:3]
	}
	return w.buffer.Write(data)
}

func TestEncryptorHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{}
	encryptor, err := NewEncryptor(writer, "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encryptor.Write([]byte("short writer test")); err != nil {
		t.Fatal(err)
	}
	if err := encryptor.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := NewDecryptor(bytes.NewReader(writer.buffer.Bytes()), "key")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || string(decoded) != "short writer test" {
		t.Fatalf("decoded = %q, err = %v", decoded, err)
	}
}

func TestRoundTrip(t *testing.T) {
	plain := bytes.Repeat([]byte("telegram-drive-data-"), 100000)
	var encrypted bytes.Buffer
	writer, err := NewEncryptor(&encrypted, "api-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), plain) {
		t.Fatal("plaintext is visible in encrypted stream")
	}
	reader, err := NewDecryptor(bytes.NewReader(encrypted.Bytes()), "api-hash")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Fatal("round-trip mismatch")
	}
}

func TestEncryptorKeepsFixedChunkBoundaries(t *testing.T) {
	var encrypted bytes.Buffer
	writer, _ := NewEncryptor(&encrypted, "hash")
	for i := 0; i < 100; i++ {
		if _, err := writer.Write([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if encrypted.Bytes()[8] != 2 {
		t.Fatalf("expected version 2, got %d", encrypted.Bytes()[8])
	}
	firstLength := binary.BigEndian.Uint32(encrypted.Bytes()[9:13])
	if firstLength != 100 {
		t.Fatalf("first chunk length = %d, want 100", firstLength)
	}
}

func TestWrongKeyFails(t *testing.T) {
	var encrypted bytes.Buffer
	writer, _ := NewEncryptor(&encrypted, "correct")
	_, _ = writer.Write([]byte("secret"))
	_ = writer.Close()
	reader, _ := NewDecryptor(bytes.NewReader(encrypted.Bytes()), "wrong")
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected authentication failure")
	}
}

func TestEncryptStringHidesValue(t *testing.T) {
	encoded, err := EncryptString("private/video name.mp4", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" || bytes.Contains([]byte(encoded), []byte("video")) {
		t.Fatalf("plaintext leaked in encrypted name: %q", encoded)
	}
	other, err := EncryptString("private/video name.mp4", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == other {
		t.Fatal("encrypted names should use random nonces")
	}
}

func TestTruncatedChunkFails(t *testing.T) {
	var encrypted bytes.Buffer
	writer, _ := NewEncryptor(&encrypted, "hash")
	_, _ = writer.Write([]byte("content"))
	_ = writer.Close()
	truncated := encrypted.Bytes()[:encrypted.Len()-1]
	reader, _ := NewDecryptor(bytes.NewReader(truncated), "hash")
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected truncated stream error")
	}
}

func TestEmptyFile(t *testing.T) {
	var encrypted bytes.Buffer
	writer, _ := NewEncryptor(&encrypted, "hash")
	_ = writer.Close()
	reader, _ := NewDecryptor(bytes.NewReader(encrypted.Bytes()), "hash")
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 0 {
		t.Fatalf("got %d bytes", len(decoded))
	}
}
