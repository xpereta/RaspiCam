package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndSaveCameraConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "mediamtx.yml")
	input := `paths:
  cam:
    source: rpiCamera
    rpiCameraVFlip: false
    rpiCameraHFlip: true
  other:
    source: rtsp
`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadCameraConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.VFlip {
		t.Fatalf("expected VFlip false")
	}
	if !cfg.HFlip {
		t.Fatalf("expected HFlip true")
	}

	cfg.VFlip = true
	cfg.HFlip = false
	if err := SaveCameraConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	updated, err := LoadCameraConfig(path)
	if err != nil {
		t.Fatalf("load updated: %v", err)
	}
	if !updated.VFlip || updated.HFlip {
		t.Fatalf("unexpected updated values")
	}

	backupMatches, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatalf("glob backup: %v", err)
	}
	if len(backupMatches) == 0 {
		t.Fatalf("expected backup file")
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(out), "rpiCameraVFlip: true") {
		t.Fatalf("expected VFlip in output")
	}
}
