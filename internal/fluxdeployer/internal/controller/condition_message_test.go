package controller

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateMessage(t *testing.T) {
	if got := truncateMessage("short"); got != "short" {
		t.Errorf("short message changed: %q", got)
	}

	long := strings.Repeat("é", maxConditionMessageLength)
	got := truncateMessage(long)
	if len(got) > maxConditionMessageLength {
		t.Errorf("got %d bytes, want at most %d", len(got), maxConditionMessageLength)
	}
	if !utf8.ValidString(got) {
		t.Error("truncation split a multi-byte character")
	}
}
