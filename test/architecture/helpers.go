package architecture

import (
	"os/exec"
	"strings"
	"testing"
)

func moduleRoot(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func modulePath(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}
