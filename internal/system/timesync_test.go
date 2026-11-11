package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synchronized")
	if fileExists(path) {
		t.Fatalf("fileExists(%q) = true before creation", path)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path) {
		t.Fatalf("fileExists(%q) = false after creation", path)
	}
}
