package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetSessionIfAuthChanged(t *testing.T) {
	dataDir := t.TempDir()
	sessionPath := filepath.Join(dataDir, "tgdrive.session")
	config := telegramConfig{BotToken: "first-token", APIID: 1, APIHash: "hash"}

	if err := os.WriteFile(sessionPath, []byte("old session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetSessionIfAuthChanged(sessionPath, config); err != nil {
		t.Fatalf("reset legacy session: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session exists after reset: %v", err)
	}

	if err := os.WriteFile(sessionPath, []byte("current session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetSessionIfAuthChanged(sessionPath, config); err != nil {
		t.Fatalf("keep current session: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session was removed for unchanged token: %v", err)
	}

	config.BotToken = "replacement-token"
	if err := resetSessionIfAuthChanged(sessionPath, config); err != nil {
		t.Fatalf("reset changed token session: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session exists after token change: %v", err)
	}
}
