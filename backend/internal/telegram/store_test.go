package telegram

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
	"tgdrive/internal/crypto"
)

func TestEncryptedSize(t *testing.T) {
	if got := encryptedSize(0); got != 9 {
		t.Fatalf("empty encrypted size = %d", got)
	}
	if got := encryptedSize(int64(crypto.ChunkSize())); got != int64(9+crypto.ChunkSize()+32) {
		t.Fatalf("one chunk encrypted size = %d", got)
	}
}

func TestMaxPlainFileSizeAccountsForEncryption(t *testing.T) {
	maximum := maxPlainFileSize()
	if encryptedSize(maximum) > MaxFileSize {
		t.Fatalf("maximum plaintext encrypts to %d bytes", encryptedSize(maximum))
	}
	if encryptedSize(maximum+1) <= MaxFileSize {
		t.Fatalf("plaintext size %d should exceed Telegram limit", maximum+1)
	}
}

func TestCountingReaderAllowsLimitButRejectsOverflow(t *testing.T) {
	reader := &countingReader{reader: strings.NewReader("12345"), max: 5}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "12345" {
		t.Fatalf("exact limit read failed: %q, %v", data, err)
	}
	overflow := &countingReader{reader: strings.NewReader("123456"), max: 5}
	_, err = io.ReadAll(overflow)
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatal("expected overflow error")
	}
}

func TestMessageIDFromChannelUpdate(t *testing.T) {
	update := &tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 42}}
	updates := &tg.Updates{Updates: []tg.UpdateClass{update}}
	id, err := messageIDFromUpdates(updates)
	if err != nil || id != 42 {
		t.Fatalf("message id = %d, err = %v", id, err)
	}
}
