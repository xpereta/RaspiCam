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
    rpiCameraWidth: 1280
    rpiCameraHeight: 720
    rpiCameraAWB: indoor
    rpiCameraMode: "2304:1296:10:P"
    rpiCameraAfMode: continuous
    rpiCameraLensPosition: 1.25
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
	if cfg.Width != 1280 || cfg.Height != 720 {
		t.Fatalf("unexpected resolution")
	}
	if cfg.AWB != "indoor" {
		t.Fatalf("unexpected awb")
	}
	if cfg.Mode != "2304:1296:10:P" {
		t.Fatalf("unexpected mode")
	}
	if cfg.AfMode != "continuous" {
		t.Fatalf("unexpected af mode")
	}
	if cfg.LensPosition == nil || *cfg.LensPosition != 1.25 {
		t.Fatalf("unexpected lens position")
	}

	cfg.VFlip = true
	cfg.HFlip = false
	cfg.Width = 1920
	cfg.Height = 1080
	cfg.AWB = "daylight"
	cfg.Mode = "1536:864:10:P"
	cfg.AfMode = "manual"
	lensPosition := 2.5
	cfg.LensPosition = &lensPosition
	cfg.LensPositionSet = true
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
	if updated.Width != 1920 || updated.Height != 1080 {
		t.Fatalf("unexpected updated resolution")
	}
	if updated.AWB != "daylight" {
		t.Fatalf("unexpected updated awb")
	}
	if updated.Mode != "1536:864:10:P" {
		t.Fatalf("unexpected updated mode")
	}
	if updated.AfMode != "manual" {
		t.Fatalf("unexpected updated af mode")
	}
	if updated.LensPosition == nil || *updated.LensPosition != 2.5 {
		t.Fatalf("unexpected updated lens position")
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
	if !strings.Contains(string(out), "rpiCameraWidth: 1920") {
		t.Fatalf("expected width in output")
	}
	if !strings.Contains(string(out), "rpiCameraAWB: daylight") {
		t.Fatalf("expected awb in output")
	}
	if !strings.Contains(string(out), "rpiCameraMode: \"1536:864:10:P\"") &&
		!strings.Contains(string(out), "rpiCameraMode: 1536:864:10:P") {
		t.Fatalf("expected mode in output")
	}
	if !strings.Contains(string(out), "rpiCameraAfMode: manual") {
		t.Fatalf("expected af mode in output")
	}
	if !strings.Contains(string(out), "rpiCameraLensPosition: 2.5") {
		t.Fatalf("expected lens position in output")
	}
	if !strings.Contains(string(out), "other:") {
		t.Fatalf("expected other path preserved")
	}

	cfg.Mode = ""
	if err := SaveCameraConfig(path, cfg); err != nil {
		t.Fatalf("save config without mode: %v", err)
	}

	updated, err = LoadCameraConfig(path)
	if err != nil {
		t.Fatalf("load without mode: %v", err)
	}
	if updated.Mode != "" {
		t.Fatalf("expected mode cleared")
	}

	out, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output without mode: %v", err)
	}
	if strings.Contains(string(out), "rpiCameraMode:") {
		t.Fatalf("expected mode key removed")
	}

	cfg.LensPosition = nil
	cfg.LensPositionSet = true
	if err := SaveCameraConfig(path, cfg); err != nil {
		t.Fatalf("save config without lens position: %v", err)
	}

	updated, err = LoadCameraConfig(path)
	if err != nil {
		t.Fatalf("load without lens position: %v", err)
	}
	if updated.LensPosition != nil {
		t.Fatalf("expected lens position cleared")
	}

	out, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output without lens position: %v", err)
	}
	if strings.Contains(string(out), "rpiCameraLensPosition:") {
		t.Fatalf("expected lens position key removed")
	}
}

func TestLoadCameraConfigMissingPath(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "mediamtx.yml")
	input := `paths:
  other:
    source: rtsp
`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadCameraConfig(path); err == nil {
		t.Fatalf("expected error for missing cam path")
	}
}

func TestLoadCameraConfigInvalidValues(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "mediamtx.yml")
	input := `paths:
  cam:
    rpiCameraVFlip: notabool
    rpiCameraWidth: notanint
`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadCameraConfig(path); err == nil {
		t.Fatalf("expected error for invalid values")
	}
}
