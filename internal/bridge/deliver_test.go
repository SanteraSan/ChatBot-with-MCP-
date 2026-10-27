package bridge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPrepareDeliveryShort(t *testing.T) {
	got := PrepareDelivery("нет критичных багов", false)
	if len(got.Messages) != 1 || got.File != "" {
		t.Fatalf("%+v", got)
	}
}

func TestPrepareDeliveryFile(t *testing.T) {
	paragraph := strings.Repeat("а", 3000)
	text := strings.Join([]string{paragraph, paragraph, paragraph, paragraph}, "\n\n")
	got := PrepareDelivery(text, true)
	if got.File == "" || len(got.Messages) != 0 {
		t.Fatalf("expected a file, got %+v", got)
	}
	if !strings.Contains(got.Caption, "после напоминания") {
		t.Fatal(got.Caption)
	}
}

func TestSplitStaysUnderLimit(t *testing.T) {
	text := strings.Repeat("абзац с текстом. ", 400)
	for _, part := range splitRunes(text, messageLimit) {
		if utf8.RuneCountInString(part) > messageLimit {
			t.Fatalf("chunk length %d", utf8.RuneCountInString(part))
		}
	}
}
