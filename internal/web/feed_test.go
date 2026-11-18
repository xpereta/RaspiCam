package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestCameraFeedURL(t *testing.T) {
	tests := []struct {
		host string
		path string
		want string
	}{
		{"raspicam.local:8080", "cam", "http://raspicam.local:8889/cam/"},
		{"raspicam.local", "cam", "http://raspicam.local:8889/cam/"},
		{"192.168.1.50:8080", "cam", "http://192.168.1.50:8889/cam/"},
		{"[fe80::1%25en0]:8080", "cam", "http://[fe80::1%25en0]:8889/cam/"},
		{"[::1]", "cam", "http://[::1]:8889/cam/"},
		{"", "cam", "http://localhost:8889/cam/"},
		{"raspicam.local:8080", "front door", "http://raspicam.local:8889/front%20door/"},
	}
	for _, tt := range tests {
		if got := cameraFeedURL(tt.host, tt.path); got != tt.want {
			t.Errorf("cameraFeedURL(%q, %q) = %q; want %q", tt.host, tt.path, got, tt.want)
		}
	}
}

func TestTemplateCameraFeedLink(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var buf bytes.Buffer
	view := StatusView{CameraFeed: "http://raspicam.local:8889/cam/"}
	if err := s.tmpl.Execute(&buf, view); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	want := `<a class="btn" href="http://raspicam.local:8889/cam/" target="_blank" rel="noopener">Open camera feed</a>`
	if !strings.Contains(buf.String(), want) {
		t.Errorf("rendered page missing feed link %q", want)
	}

	buf.Reset()
	if err := s.tmpl.Execute(&buf, StatusView{}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if strings.Contains(buf.String(), "Open camera feed") {
		t.Errorf("feed link rendered without a URL")
	}
}

func TestCameraRTSPURL(t *testing.T) {
	tests := []struct {
		host string
		path string
		want string
	}{
		{"raspicam.local:8080", "cam", "rtsp://raspicam.local:8554/cam"},
		{"raspicam.local", "cam", "rtsp://raspicam.local:8554/cam"},
		{"192.168.1.50:8080", "cam", "rtsp://192.168.1.50:8554/cam"},
		{"[fe80::1%25en0]:8080", "cam", "rtsp://[fe80::1%25en0]:8554/cam"},
		{"", "cam", "rtsp://localhost:8554/cam"},
		{"raspicam.local:8080", "front door", "rtsp://raspicam.local:8554/front%20door"},
	}
	for _, tt := range tests {
		if got := cameraRTSPURL(tt.host, tt.path); got != tt.want {
			t.Errorf("cameraRTSPURL(%q, %q) = %q; want %q", tt.host, tt.path, got, tt.want)
		}
	}
}

func TestTemplateVLCSection(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var buf bytes.Buffer
	view := StatusView{CameraRTSP: "rtsp://raspicam.local:8554/cam"}
	if err := s.tmpl.Execute(&buf, view); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	page := buf.String()
	for _, want := range []string{
		`<pre class="code" id="rtsp-url">rtsp://raspicam.local:8554/cam</pre>`,
		`"C:\Program Files\VideoLAN\VLC\vlc.exe" --rtsp-tcp --network-caching=1000 rtsp://raspicam.local:8554/cam`,
		`&amp; "C:\Program Files\VideoLAN\VLC\vlc.exe" --rtsp-tcp --network-caching=1000 rtsp://raspicam.local:8554/cam`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered page missing %q", want)
		}
	}

	buf.Reset()
	if err := s.tmpl.Execute(&buf, StatusView{}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if strings.Contains(buf.String(), "Watch in VLC") {
		t.Errorf("VLC section rendered without an RTSP URL")
	}
}
