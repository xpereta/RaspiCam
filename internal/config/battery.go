package config

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Size units for BATTERY_HISTORY_MAX_SIZE. Units are 1024-based:
// 1KB = 1024 bytes, 1MB = 1024 KB, 1GB = 1024 MB.
const (
	kib int64 = 1 << 10
	mib int64 = 1 << 20
	gib int64 = 1 << 30
)

// Environment variable names for the battery monitor (SPEC 3.8).
const (
	envBatteryEnabled         = "BATTERY_ENABLED"
	envBatteryI2CDevice       = "BATTERY_I2C_DEVICE"
	envBatteryI2CAddr         = "BATTERY_I2C_ADDR"
	envBatteryPollInterval    = "BATTERY_POLL_INTERVAL"
	envBatteryLowSOC          = "BATTERY_LOW_SOC"
	envBatteryShutdownSOC     = "BATTERY_SHUTDOWN_SOC"
	envBatteryShutdownVolts   = "BATTERY_SHUTDOWN_VOLTS"
	envBatteryShutdownSamples = "BATTERY_SHUTDOWN_SAMPLES"
	envBatteryGracePeriod     = "BATTERY_GRACE_PERIOD"
	envBatteryDryRun          = "BATTERY_SHUTDOWN_DRY_RUN"
	envStateDir               = "STATE_DIR"
	envStateDirectory         = "STATE_DIRECTORY"
	envHistoryEnabled         = "BATTERY_HISTORY_ENABLED"
	envHistorySampleInterval  = "BATTERY_HISTORY_SAMPLE_INTERVAL"
	envHistoryFlushInterval   = "BATTERY_HISTORY_FLUSH_INTERVAL"
	envHistoryMaxSize         = "BATTERY_HISTORY_MAX_SIZE"
	envBatteryFullSOC         = "BATTERY_FULL_SOC"
)

const defaultStateDir = "/var/lib/raspicam"

// BatteryConfig holds the battery monitor settings (phase 1 and phase 2).
type BatteryConfig struct {
	Enabled         bool
	I2CDevice       string
	I2CAddr         uint16
	PollInterval    time.Duration
	LowSOC          float64
	ShutdownSOC     float64
	ShutdownVolts   float64
	ShutdownSamples int
	GracePeriod     time.Duration
	DryRun          bool
	StateDir        string

	HistoryEnabled        bool
	HistorySampleInterval time.Duration
	HistoryFlushInterval  time.Duration
	HistoryMaxSize        int64 // bytes
	FullSOC               float64
}

// DefaultBatteryConfig returns the documented defaults.
func DefaultBatteryConfig() BatteryConfig {
	return BatteryConfig{
		Enabled:               true,
		I2CDevice:             "/dev/i2c-1",
		I2CAddr:               0x36,
		PollInterval:          30 * time.Second,
		LowSOC:                20,
		ShutdownSOC:           5,
		ShutdownVolts:         3.30,
		ShutdownSamples:       3,
		GracePeriod:           2 * time.Minute,
		DryRun:                false,
		StateDir:              defaultStateDir,
		HistoryEnabled:        true,
		HistorySampleInterval: 60 * time.Second,
		HistoryFlushInterval:  15 * time.Minute,
		HistoryMaxSize:        10 * mib,
		FullSOC:               95,
	}
}

// LoadBatteryConfig reads the battery settings through getenv. Unset or
// empty variables use the default. Invalid or out-of-range values are
// replaced by the default and reported as warnings. It never fails.
func LoadBatteryConfig(getenv func(string) string) (BatteryConfig, []string) {
	l := envLoader{getenv: getenv}
	d := DefaultBatteryConfig()
	c := d

	c.Enabled = l.bool(envBatteryEnabled, d.Enabled)
	c.I2CDevice = l.path(envBatteryI2CDevice, d.I2CDevice)
	c.I2CAddr = l.i2cAddr(envBatteryI2CAddr, d.I2CAddr)
	c.PollInterval = l.duration(envBatteryPollInterval, d.PollInterval, 10*time.Second, 10*time.Minute)
	c.LowSOC = l.float(envBatteryLowSOC, d.LowSOC, 1, 90)
	c.ShutdownSOC = l.float(envBatteryShutdownSOC, d.ShutdownSOC, 0, 50)
	c.ShutdownVolts = l.float(envBatteryShutdownVolts, d.ShutdownVolts, 3.00, 3.80)
	c.ShutdownSamples = l.int(envBatteryShutdownSamples, d.ShutdownSamples, 1, 20)
	c.GracePeriod = l.duration(envBatteryGracePeriod, d.GracePeriod, 0, 30*time.Minute)
	c.DryRun = l.bool(envBatteryDryRun, d.DryRun)
	c.StateDir = l.stateDir(d.StateDir)
	c.HistoryEnabled = l.bool(envHistoryEnabled, d.HistoryEnabled)
	c.HistorySampleInterval = l.duration(envHistorySampleInterval, d.HistorySampleInterval, 30*time.Second, 10*time.Minute)
	c.HistoryFlushInterval = l.duration(envHistoryFlushInterval, d.HistoryFlushInterval, time.Minute, time.Hour)
	c.HistoryMaxSize = l.size(envHistoryMaxSize, d.HistoryMaxSize, mib, 500*mib)
	c.FullSOC = l.float(envBatteryFullSOC, d.FullSOC, 50, 100)

	if c.ShutdownSOC >= c.LowSOC {
		l.warnf("%s=%g must be below %s=%g; using defaults %g and %g",
			envBatteryShutdownSOC, c.ShutdownSOC, envBatteryLowSOC, c.LowSOC, d.ShutdownSOC, d.LowSOC)
		c.ShutdownSOC = d.ShutdownSOC
		c.LowSOC = d.LowSOC
	}

	if r := roundUpToMultiple(c.HistorySampleInterval, c.PollInterval); r != c.HistorySampleInterval {
		l.warnf("%s=%s is not a multiple of %s=%s; rounded up to %s",
			envHistorySampleInterval, c.HistorySampleInterval, envBatteryPollInterval, c.PollInterval, r)
		c.HistorySampleInterval = r
	}

	return c, l.warnings
}

// roundUpToMultiple returns the smallest multiple of step that is >= d and
// >= step. step must be positive.
func roundUpToMultiple(d, step time.Duration) time.Duration {
	if d <= step {
		return step
	}
	n := (d + step - 1) / step
	return n * step
}

// envLoader reads variables and collects warnings for invalid values.
type envLoader struct {
	getenv   func(string) string
	warnings []string
}

func (l *envLoader) warnf(format string, args ...any) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}

// lookup returns the trimmed value and whether it is set and non-empty.
func (l *envLoader) lookup(name string) (string, bool) {
	v := strings.TrimSpace(l.getenv(name))
	return v, v != ""
}

func (l *envLoader) invalid(name, raw string, err error, def any) {
	l.warnf("%s=%q is invalid (%v); using default %v", name, raw, err, def)
}

func (l *envLoader) bool(name string, def bool) bool {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		l.invalid(name, raw, errors.New("not a boolean"), def)
		return def
	}
	return v
}

func (l *envLoader) path(name, def string) string {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseAbsPath(raw)
	if err != nil {
		l.invalid(name, raw, err, def)
		return def
	}
	return v
}

func (l *envLoader) i2cAddr(name string, def uint16) uint16 {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseI2CAddr(raw)
	if err != nil {
		l.invalid(name, raw, err, fmt.Sprintf("0x%02x", def))
		return def
	}
	return v
}

func (l *envLoader) duration(name string, def, lo, hi time.Duration) time.Duration {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseDurationRange(raw, lo, hi)
	if err != nil {
		l.invalid(name, raw, err, def)
		return def
	}
	return v
}

func (l *envLoader) float(name string, def, lo, hi float64) float64 {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseFloatRange(raw, lo, hi)
	if err != nil {
		l.invalid(name, raw, err, def)
		return def
	}
	return v
}

func (l *envLoader) int(name string, def, lo, hi int) int {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseIntRange(raw, lo, hi)
	if err != nil {
		l.invalid(name, raw, err, def)
		return def
	}
	return v
}

func (l *envLoader) size(name string, def, lo, hi int64) int64 {
	raw, ok := l.lookup(name)
	if !ok {
		return def
	}
	v, err := parseSize(raw)
	if err == nil && (v < lo || v > hi) {
		err = fmt.Errorf("out of range %s-%s", formatSize(lo), formatSize(hi))
	}
	if err != nil {
		l.invalid(name, raw, err, formatSize(def))
		return def
	}
	return v
}

// stateDir resolves STATE_DIR, then STATE_DIRECTORY (set by systemd; the
// first entry of a colon-separated list), then def.
func (l *envLoader) stateDir(def string) string {
	if _, ok := l.lookup(envStateDir); ok {
		return l.path(envStateDir, def)
	}
	raw, ok := l.lookup(envStateDirectory)
	if !ok {
		return def
	}
	first, _, _ := strings.Cut(raw, ":")
	v, err := parseAbsPath(first)
	if err != nil {
		l.invalid(envStateDirectory, raw, err, def)
		return def
	}
	return v
}

func parseAbsPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(s) {
		return "", errors.New("not an absolute path")
	}
	return filepath.Clean(s), nil
}

// parseI2CAddr accepts hex (0x36), octal (0o66) or decimal (54) 7-bit
// addresses in the non-reserved range 0x03-0x77.
func parseI2CAddr(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 0, 16)
	if err != nil {
		return 0, errors.New("not an integer")
	}
	if v < 0x03 || v > 0x77 {
		return 0, errors.New("out of range 0x03-0x77")
	}
	return uint16(v), nil
}

func parseDurationRange(s string, lo, hi time.Duration) (time.Duration, error) {
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("not a duration")
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("out of range %s-%s", lo, hi)
	}
	return v, nil
}

func parseFloatRange(s string, lo, hi float64) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("not a number")
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("out of range %g-%g", lo, hi)
	}
	return v, nil
}

func parseIntRange(s string, lo, hi int) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New("not an integer")
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("out of range %d-%d", lo, hi)
	}
	return v, nil
}

// parseSize parses a byte size: plain bytes ("1048576") or an integer with a
// case-insensitive unit suffix B, KB, MB or GB ("500KB", "10mb", "1GB").
// Units are 1024-based (1KB = 1024 bytes). Fractions and spaces between the
// number and unit are rejected.
func parseSize(s string) (int64, error) {
	u := strings.ToUpper(s)
	mult := int64(1)
	switch {
	case strings.HasSuffix(u, "GB"):
		mult, u = gib, strings.TrimSuffix(u, "GB")
	case strings.HasSuffix(u, "MB"):
		mult, u = mib, strings.TrimSuffix(u, "MB")
	case strings.HasSuffix(u, "KB"):
		mult, u = kib, strings.TrimSuffix(u, "KB")
	case strings.HasSuffix(u, "B"):
		u = strings.TrimSuffix(u, "B")
	}
	if u == "" || strings.TrimLeft(u, "0123456789") != "" {
		return 0, errors.New("not a size")
	}
	n, err := strconv.ParseInt(u, 10, 64)
	if err != nil || n > math.MaxInt64/mult {
		return 0, errors.New("size too large")
	}
	return n * mult, nil
}

// formatSize renders a byte count with the largest exact unit.
func formatSize(n int64) string {
	switch {
	case n != 0 && n%gib == 0:
		return strconv.FormatInt(n/gib, 10) + "GB"
	case n != 0 && n%mib == 0:
		return strconv.FormatInt(n/mib, 10) + "MB"
	case n != 0 && n%kib == 0:
		return strconv.FormatInt(n/kib, 10) + "KB"
	default:
		return strconv.FormatInt(n, 10) + "B"
	}
}
