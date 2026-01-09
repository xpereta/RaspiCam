package mediamtx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type Status struct {
	ServiceStatus string
	APIStatus     string
	PathName      string
	PathReady     *bool
	SourceType    string
	Readers       *int
	Tracks        *int
}

type pathResponse struct {
	Name    string       `json:"name"`
	Ready   bool         `json:"ready"`
	Source  *pathSource  `json:"source"`
	Readers []pathReader `json:"readers"`
	Tracks  []string     `json:"tracks"`
}

type pathSource struct {
	Type string `json:"type"`
}

type pathReader struct {
	Type string `json:"type"`
}

func Collect(ctx context.Context, baseURL, pathName string) (Status, []string) {
	status := Status{
		ServiceStatus: "unknown",
		APIStatus:     "unknown",
		PathName:      pathName,
		SourceType:    "unknown",
	}
	var warnings []string

	if svc, err := ServiceStatus(ctx); err != nil {
		warnings = append(warnings, fmt.Sprintf("MediaMTX service status unavailable: %v", err))
	} else {
		status.ServiceStatus = svc
	}

	if pathName != "" {
		path, err := GetPathStatus(ctx, baseURL, pathName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("MediaMTX API unavailable: %v", err))
			status.APIStatus = "unavailable"
		} else {
			status.APIStatus = "ok"
			status.PathReady = &path.Ready
			status.SourceType = path.SourceType
			status.Readers = &path.Readers
			status.Tracks = &path.Tracks
		}
	}

	return status, warnings
}

func ServiceStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "mediamtx")
	out, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if err != nil {
		if status != "" {
			return status, nil
		}
		return "", err
	}
	if status == "" {
		return "", errors.New("empty status")
	}
	return status, nil
}

type PathStatus struct {
	Ready      bool
	SourceType string
	Readers    int
	Tracks     int
}

func GetPathStatus(ctx context.Context, baseURL, pathName string) (PathStatus, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	escapedPathName := url.PathEscape(pathName)
	url := strings.TrimRight(baseURL, "/") + "/v3/paths/get/" + escapedPathName

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return PathStatus{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return PathStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return PathStatus{}, fmt.Errorf("path %q not found", pathName)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PathStatus{}, fmt.Errorf("unexpected status %s", resp.Status)
	}

	var pr pathResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return PathStatus{}, err
	}

	status := PathStatus{
		Ready:   pr.Ready,
		Readers: len(pr.Readers),
		Tracks:  len(pr.Tracks),
	}
	if pr.Source != nil && pr.Source.Type != "" {
		status.SourceType = pr.Source.Type
	} else {
		status.SourceType = "none"
	}

	return status, nil
}
