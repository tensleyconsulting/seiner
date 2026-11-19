package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoGoogleImportsInAppOrUI guards the boundary described in AGENTS.md:
// internal/app and internal/ui must never import Google Cloud client
// libraries, generated API clients, or OAuth2. Live GCP access belongs under
// internal/gcp.
func TestNoGoogleImportsInAppOrUI(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to go list; skipped in -short")
	}

	modOut, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	root := filepath.Dir(strings.TrimSpace(string(modOut)))

	cmd := exec.Command("go", "list", "-deps", "./internal/app", "./internal/ui/...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./internal/app ./internal/ui/...: %v\n%s", err, out)
	}

	forbidden := []string{"cloud.google.com/", "google.golang.org/", "golang.org/x/oauth2"}
	for _, line := range strings.Split(string(out), "\n") {
		for _, prefix := range forbidden {
			if strings.HasPrefix(line, prefix) {
				t.Errorf("internal/app or internal/ui depends on forbidden import %q", line)
			}
		}
	}
}
