package config

import (
	"path/filepath"
	"testing"
)

func TestConfigModTimeMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yml")
	_, ok, err := ConfigModTime(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing file")
	}
}
