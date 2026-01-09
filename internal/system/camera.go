package system

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var cameraCodes = []string{"imx708", "imx477", "imx219", "ov5647"}

func cameraModel() string {
	code := findCameraCode([]string{
		"/sys/firmware/devicetree/base",
		"/proc/device-tree",
	})
	if code == "" {
		return "Unknown camera"
	}
	return mapCameraModel(code)
}

func findCameraCode(roots []string) string {
	code := ""
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, err := readFilePrefix(path, 8192)
			if err != nil {
				return nil
			}
			if found := extractCameraCode(data); found != "" {
				code = found
				return io.EOF
			}
			return nil
		})
		if code != "" {
			break
		}
	}
	return code
}

func readFilePrefix(path string, limit int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buf := make([]byte, limit)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	return string(buf[:n]), nil
}

func extractCameraCode(data string) string {
	lower := strings.ToLower(data)
	for _, code := range cameraCodes {
		if strings.Contains(lower, code) {
			return code
		}
	}
	return ""
}

func mapCameraModel(code string) string {
	switch code {
	case "ov5647":
		return "Pi Camera v1 (ov5647)"
	case "imx219":
		return "Pi Camera v2 (imx219)"
	case "imx477":
		return "HQ Camera (imx477)"
	case "imx708":
		return "Pi Camera v3 (imx708)"
	default:
		return "Unknown camera (" + code + ")"
	}
}
