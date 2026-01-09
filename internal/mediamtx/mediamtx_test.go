package mediamtx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetPathStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/paths/get/cam" {
			http.NotFound(w, r)
			return
		}
		resp := pathResponse{
			Name:  "cam",
			Ready: true,
			Source: &pathSource{
				Type: "rpiCameraSource",
			},
			Readers: []pathReader{{Type: "rtspSession"}},
			Tracks:  []string{"video"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	status, err := GetPathStatus(context.Background(), server.URL, "cam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("expected ready true")
	}
	if status.SourceType != "rpiCameraSource" {
		t.Fatalf("unexpected source type: %s", status.SourceType)
	}
	if status.Readers != 1 {
		t.Fatalf("unexpected readers: %d", status.Readers)
	}
	if status.Tracks != 1 {
		t.Fatalf("unexpected tracks: %d", status.Tracks)
	}
}
