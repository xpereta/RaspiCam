package config

import (
	"os"
	"time"
)

func ConfigModTime(path string) (time.Time, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return info.ModTime(), true, nil
}
