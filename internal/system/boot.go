package system

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	uptimePath = "/proc/uptime"
	bootIDPath = "/proc/sys/kernel/random/boot_id"
)

// maxUptimeSeconds keeps the conversion to time.Duration from overflowing.
const maxUptimeSeconds = float64(math.MaxInt64) / float64(time.Second)

// Uptime returns the time since boot from /proc/uptime. It returns an error
// where /proc is unavailable (for example on macOS).
func Uptime() (time.Duration, error) {
	b, err := os.ReadFile(uptimePath)
	if err != nil {
		return 0, fmt.Errorf("read uptime: %w", err)
	}
	d, err := parseUptime(string(b))
	if err != nil {
		return 0, fmt.Errorf("read uptime: %w", err)
	}
	return d, nil
}

// parseUptime parses the first field of /proc/uptime (seconds with fraction).
func parseUptime(s string) (time.Duration, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, errors.New("empty uptime")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime %q: %w", fields[0], err)
	}
	if math.IsNaN(secs) || secs < 0 || secs >= maxUptimeSeconds {
		return 0, fmt.Errorf("invalid uptime %q", fields[0])
	}
	return time.Duration(math.Round(secs * float64(time.Second))), nil
}

// BootID returns the kernel boot id, which changes on every boot. It returns
// an error where /proc is unavailable (for example on macOS).
func BootID() (string, error) {
	b, err := os.ReadFile(bootIDPath)
	if err != nil {
		return "", fmt.Errorf("read boot id: %w", err)
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", errors.New("read boot id: empty")
	}
	return id, nil
}
