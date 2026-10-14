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

	// the leading ASCII byte puts the limit in the middle of a two-byte "é"
	long := "a" + strings.Repeat("é", maxConditionMessageLength)
	got := truncateMessage(long)
	if len(got) != maxConditionMessageLength-1 {
		t.Errorf("got %d bytes, want %d", len(got), maxConditionMessageLength-1)
	}
	if !utf8.ValidString(got) {
		t.Error("truncation split a multi-byte character")
	}
}
