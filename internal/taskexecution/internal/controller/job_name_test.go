package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedJobName(t *testing.T) {
	// short name: unchanged join
	require.Equal(t, "short-abcd1234", boundedJobName("short", "abcd1234"))

	// 63-char taskExecution name: result must stay within the 63-char label cap
	longName := "candidates-verify-db-ready-c4b2f59d-baad-4a19-aea2-49afe5ca0ff9"
	require.Len(t, longName, 63)
	got := boundedJobName(longName, "abcd1234")
	require.LessOrEqual(t, len(got), 63)
	require.NotContains(t, got, "--") // no double dash from trimming
}
