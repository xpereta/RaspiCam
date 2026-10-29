package system

import "os"

// timesyncPath is created by systemd-timesyncd once the clock is synchronized.
const timesyncPath = "/run/systemd/timesync/synchronized"

// TimeSynced reports whether systemd-timesyncd has synchronized the wall
// clock since boot. It returns false where the flag file is absent (for
// example on macOS or without systemd-timesyncd).
func TimeSynced() bool {
	return fileExists(timesyncPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
