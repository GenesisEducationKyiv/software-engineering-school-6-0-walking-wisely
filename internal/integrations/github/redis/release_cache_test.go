package redis

import "testing"

func TestReleaseKey(t *testing.T) {
	got := releaseKey("owner/repo")
	want := "github:release:owner/repo"
	if got != want {
		t.Fatalf("releaseKey() = %q, want %q", got, want)
	}
}
