package telegram

import "testing"

func TestParseChatID(t *testing.T) {
	for _, input := range []string{"123", "-123", "-1001234567890"} {
		if _, err := ParseChatID(input); err != nil {
			t.Fatalf("ParseChatID(%q): %v", input, err)
		}
	}
	if _, err := ParseChatID("0"); err == nil {
		t.Fatal("zero chat ID should fail")
	}
	if got := channelChatID(123); got != -1000000000123 {
		t.Fatalf("channel chat ID = %d", got)
	}
}
