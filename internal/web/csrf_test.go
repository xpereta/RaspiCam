package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		host    string
		headers map[string]string
		want    int
	}{
		{"get cross-site", http.MethodGet, "cam.local", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"}, http.StatusOK},
		{"post same-origin", http.MethodPost, "cam.local", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"post none", http.MethodPost, "cam.local", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"post cross-site", http.MethodPost, "cam.local", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"post same-site", http.MethodPost, "cam.local", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"post matching origin", http.MethodPost, "cam.local:8080", map[string]string{"Origin": "http://cam.local:8080"}, http.StatusOK},
		{"post mismatching origin", http.MethodPost, "cam.local:8080", map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"post null origin", http.MethodPost, "cam.local", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"post no headers", http.MethodPost, "cam.local", nil, http.StatusOK},
		{"post mixed-case host", http.MethodPost, "Cam.Local:8080", map[string]string{"Origin": "http://cam.LOCAL:8080"}, http.StatusOK},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := sameOrigin(next)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/camera-config", nil)
			req.Host = tt.host
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestHandlerRejectsCrossSiteCameraConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mediamtx.yml")
	t.Setenv("MEDIAMTX_CONFIG_PATH", configPath)
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body := strings.NewReader("rpiCameraVFlip=on&rpiCameraAfMode=auto")
	req := httptest.NewRequest(http.MethodPost, "/camera-config", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("handler reached: redirected to %q", loc)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config file touched: %v", err)
	}
}
