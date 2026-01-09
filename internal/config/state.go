package config

import (
	"os"
	"path/filepath"
	"time"
)

const timeLayout = "2006-01-02 15:04:05"

func LoadLastUpdate(path string) (time.Time, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}

	value := string(b)
	parsed, err := time.Parse(timeLayout, value)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

func SaveLastUpdate(path string, t time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(t.Format(timeLayout)), 0o644)
}
