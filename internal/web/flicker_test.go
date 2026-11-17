package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCameraUpdateFlickerPeriod(t *testing.T) {
	tests := []struct {
		name     string
		form     string
		wantLoc  string
		wantYAML string // substring expected in the saved config; empty means key absent
	}{
		{"50 Hz", "rpiCameraAfMode=manual&rpiCameraFlickerPeriod=10000", "/?camera=saved", "rpiCameraFlickerPeriod: 10000"},
		{"off removes key", "rpiCameraAfMode=manual&rpiCameraFlickerPeriod=", "/?camera=saved", ""},
		{"field absent keeps key", "rpiCameraAfMode=manual", "/?camera=saved", "rpiCameraFlickerPeriod: 8333"},
		{"invalid", "rpiCameraAfMode=manual&rpiCameraFlickerPeriod=abc", "/?camera=invalid-flicker-period", "rpiCameraFlickerPeriod: 8333"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mediamtx.yml")
			input := "paths:\n  cam:\n    source: rpiCamera\n    rpiCameraFlickerPeriod: 8333\n"
			if err := os.WriteFile(configPath, []byte(input), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			t.Setenv("MEDIAMTX_CONFIG_PATH", configPath)
			s, err := NewServer()
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/camera-config", strings.NewReader(tt.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if loc := rec.Header().Get("Location"); loc != tt.wantLoc {
				t.Fatalf("Location = %q, want %q", loc, tt.wantLoc)
			}
			out, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if tt.wantYAML == "" {
				if strings.Contains(string(out), "rpiCameraFlickerPeriod") {
					t.Fatalf("expected flicker key removed, got:\n%s", out)
				}
			} else if !strings.Contains(string(out), tt.wantYAML) {
				t.Fatalf("expected %q in config, got:\n%s", tt.wantYAML, out)
			}
		})
	}
}

func TestTemplateFlickerOptions(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	tests := []struct {
		period string
		want   string
	}{
		{"", `<option value="" selected>Off</option>`},
		{"10000", `<option value="10000" selected>`},
		{"20000", `<option value="20000" selected>Custom (20000`},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		view := StatusView{Camera: CameraView{FlickerPeriod: tt.period}}
		if err := s.tmpl.Execute(&buf, view); err != nil {
			t.Fatalf("execute template: %v", err)
		}
		if !strings.Contains(buf.String(), tt.want) {
			t.Errorf("period %q: rendered page missing %q", tt.period, tt.want)
		}
		if tt.period != "20000" && strings.Contains(buf.String(), "Custom (") {
			t.Errorf("period %q: unexpected custom option", tt.period)
		}
	}
}
