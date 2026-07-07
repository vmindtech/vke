package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// ChdirRepoRoot switches the CWD to the repo root (where go.mod is); restored on cleanup.
func ChdirRepoRoot(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("go.mod not found above %s", wd)
		}
		root = parent
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}
