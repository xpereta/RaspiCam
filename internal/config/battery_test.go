package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadBatteryConfigDefaults(t *testing.T) {
	cfg, warnings := LoadBatteryConfig(mapEnv(nil))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	want := BatteryConfig{
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
		StateDir:              "/var/lib/raspicam",
		HistoryEnabled:        true,
		HistorySampleInterval: 60 * time.Second,
		HistoryFlushInterval:  15 * time.Minute,
		HistoryMaxSize:        10 << 20,
		FullSOC:               95,
	}
	if cfg != want {
		t.Fatalf("defaults mismatch:\n got %+v\nwant %+v", cfg, want)
	}
	if cfg != DefaultBatteryConfig() {
		t.Fatalf("LoadBatteryConfig defaults differ from DefaultBatteryConfig")
	}
}

func TestLoadBatteryConfigEmptyValuesUseDefaults(t *testing.T) {
	env := map[string]string{
		"BATTERY_ENABLED":       "",
		"BATTERY_POLL_INTERVAL": "   ",
		"STATE_DIR":             "",
	}
	cfg, warnings := LoadBatteryConfig(mapEnv(env))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if cfg != DefaultBatteryConfig() {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadBatteryConfigAllValid(t *testing.T) {
	env := map[string]string{
		"BATTERY_ENABLED":                 "false",
		"BATTERY_I2C_DEVICE":              "/dev/i2c-3",
		"BATTERY_I2C_ADDR":                "54",
		"BATTERY_POLL_INTERVAL":           "15s",
		"BATTERY_LOW_SOC":                 "25.5",
		"BATTERY_SHUTDOWN_SOC":            "8",
		"BATTERY_SHUTDOWN_VOLTS":          "3.4",
		"BATTERY_SHUTDOWN_SAMPLES":        "5",
		"BATTERY_GRACE_PERIOD":            "0",
		"BATTERY_SHUTDOWN_DRY_RUN":        "1",
		"STATE_DIR":                       "/srv/raspicam/",
		"BATTERY_HISTORY_ENABLED":         "FALSE",
		"BATTERY_HISTORY_SAMPLE_INTERVAL": "45s",
		"BATTERY_HISTORY_FLUSH_INTERVAL":  "1h",
		"BATTERY_HISTORY_MAX_SIZE":        "20mb",
		"BATTERY_FULL_SOC":                "100",
	}

	cfg, warnings := LoadBatteryConfig(mapEnv(env))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	want := BatteryConfig{
		Enabled:               false,
		I2CDevice:             "/dev/i2c-3",
		I2CAddr:               54,
		PollInterval:          15 * time.Second,
		LowSOC:                25.5,
		ShutdownSOC:           8,
		ShutdownVolts:         3.4,
		ShutdownSamples:       5,
		GracePeriod:           0,
		DryRun:                true,
		StateDir:              "/srv/raspicam",
		HistoryEnabled:        false,
		HistorySampleInterval: 45 * time.Second,
		HistoryFlushInterval:  time.Hour,
		HistoryMaxSize:        20 << 20,
		FullSOC:               100,
	}
	if cfg != want {
		t.Fatalf("config mismatch:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadBatteryConfigInvalidValues(t *testing.T) {
	def := DefaultBatteryConfig()
	tests := []struct {
		name  string
		value string
		check func(BatteryConfig) bool
	}{
		{"BATTERY_ENABLED", "maybe", func(c BatteryConfig) bool { return c.Enabled == def.Enabled }},
		{"BATTERY_I2C_DEVICE", "i2c-1", func(c BatteryConfig) bool { return c.I2CDevice == def.I2CDevice }},
		{"BATTERY_I2C_ADDR", "0x78", func(c BatteryConfig) bool { return c.I2CAddr == def.I2CAddr }},
		{"BATTERY_I2C_ADDR", "0x02", func(c BatteryConfig) bool { return c.I2CAddr == def.I2CAddr }},
		{"BATTERY_I2C_ADDR", "abc", func(c BatteryConfig) bool { return c.I2CAddr == def.I2CAddr }},
		{"BATTERY_POLL_INTERVAL", "5s", func(c BatteryConfig) bool { return c.PollInterval == def.PollInterval }},
		{"BATTERY_POLL_INTERVAL", "11m", func(c BatteryConfig) bool { return c.PollInterval == def.PollInterval }},
		{"BATTERY_POLL_INTERVAL", "30", func(c BatteryConfig) bool { return c.PollInterval == def.PollInterval }},
		{"BATTERY_LOW_SOC", "0", func(c BatteryConfig) bool { return c.LowSOC == def.LowSOC }},
		{"BATTERY_LOW_SOC", "91", func(c BatteryConfig) bool { return c.LowSOC == def.LowSOC }},
		{"BATTERY_LOW_SOC", "NaN", func(c BatteryConfig) bool { return c.LowSOC == def.LowSOC }},
		{"BATTERY_SHUTDOWN_SOC", "-1", func(c BatteryConfig) bool { return c.ShutdownSOC == def.ShutdownSOC }},
		{"BATTERY_SHUTDOWN_SOC", "51", func(c BatteryConfig) bool { return c.ShutdownSOC == def.ShutdownSOC }},
		{"BATTERY_SHUTDOWN_VOLTS", "2.99", func(c BatteryConfig) bool { return c.ShutdownVolts == def.ShutdownVolts }},
		{"BATTERY_SHUTDOWN_VOLTS", "3.81", func(c BatteryConfig) bool { return c.ShutdownVolts == def.ShutdownVolts }},
		{"BATTERY_SHUTDOWN_VOLTS", "x", func(c BatteryConfig) bool { return c.ShutdownVolts == def.ShutdownVolts }},
		{"BATTERY_SHUTDOWN_SAMPLES", "0", func(c BatteryConfig) bool { return c.ShutdownSamples == def.ShutdownSamples }},
		{"BATTERY_SHUTDOWN_SAMPLES", "21", func(c BatteryConfig) bool { return c.ShutdownSamples == def.ShutdownSamples }},
		{"BATTERY_SHUTDOWN_SAMPLES", "2.5", func(c BatteryConfig) bool { return c.ShutdownSamples == def.ShutdownSamples }},
		{"BATTERY_GRACE_PERIOD", "-1s", func(c BatteryConfig) bool { return c.GracePeriod == def.GracePeriod }},
		{"BATTERY_GRACE_PERIOD", "31m", func(c BatteryConfig) bool { return c.GracePeriod == def.GracePeriod }},
		{"BATTERY_SHUTDOWN_DRY_RUN", "yes", func(c BatteryConfig) bool { return c.DryRun == def.DryRun }},
		{"STATE_DIR", "relative/dir", func(c BatteryConfig) bool { return c.StateDir == def.StateDir }},
		{"BATTERY_HISTORY_ENABLED", "on", func(c BatteryConfig) bool { return c.HistoryEnabled == def.HistoryEnabled }},
		{"BATTERY_HISTORY_SAMPLE_INTERVAL", "29s", func(c BatteryConfig) bool { return c.HistorySampleInterval == def.HistorySampleInterval }},
		{"BATTERY_HISTORY_SAMPLE_INTERVAL", "11m", func(c BatteryConfig) bool { return c.HistorySampleInterval == def.HistorySampleInterval }},
		{"BATTERY_HISTORY_FLUSH_INTERVAL", "59s", func(c BatteryConfig) bool { return c.HistoryFlushInterval == def.HistoryFlushInterval }},
		{"BATTERY_HISTORY_FLUSH_INTERVAL", "61m", func(c BatteryConfig) bool { return c.HistoryFlushInterval == def.HistoryFlushInterval }},
		{"BATTERY_HISTORY_MAX_SIZE", "500KB", func(c BatteryConfig) bool { return c.HistoryMaxSize == def.HistoryMaxSize }},
		{"BATTERY_HISTORY_MAX_SIZE", "1GB", func(c BatteryConfig) bool { return c.HistoryMaxSize == def.HistoryMaxSize }},
		{"BATTERY_HISTORY_MAX_SIZE", "ten", func(c BatteryConfig) bool { return c.HistoryMaxSize == def.HistoryMaxSize }},
		{"BATTERY_FULL_SOC", "49", func(c BatteryConfig) bool { return c.FullSOC == def.FullSOC }},
		{"BATTERY_FULL_SOC", "101", func(c BatteryConfig) bool { return c.FullSOC == def.FullSOC }},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			cfg, warnings := LoadBatteryConfig(mapEnv(map[string]string{tt.name: tt.value}))
			if !tt.check(cfg) {
				t.Fatalf("expected default for %s, got %+v", tt.name, cfg)
			}
			if len(warnings) != 1 {
				t.Fatalf("expected 1 warning, got %v", warnings)
			}
			w := warnings[0]
			if !strings.Contains(w, tt.name) || !strings.Contains(w, tt.value) || !strings.Contains(w, "default") {
				t.Fatalf("warning %q should name variable, value and default", w)
			}
			if cfg != DefaultBatteryConfig() {
				t.Fatalf("other fields changed: %+v", cfg)
			}
		})
	}
}

func TestLoadBatteryConfigWarningText(t *testing.T) {
	_, warnings := LoadBatteryConfig(mapEnv(map[string]string{"BATTERY_POLL_INTERVAL": "5s"}))
	want := `BATTERY_POLL_INTERVAL="5s" is invalid (out of range 10s-10m0s); using default 30s`
	if len(warnings) != 1 || warnings[0] != want {
		t.Fatalf("got %v, want [%s]", warnings, want)
	}
}

func TestLoadBatteryConfigShutdownSOCNotBelowLow(t *testing.T) {
	tests := []struct{ low, shutdown string }{
		{"10", "10"},
		{"10", "20"},
	}
	def := DefaultBatteryConfig()
	for _, tt := range tests {
		env := map[string]string{"BATTERY_LOW_SOC": tt.low, "BATTERY_SHUTDOWN_SOC": tt.shutdown}
		cfg, warnings := LoadBatteryConfig(mapEnv(env))
		if cfg.LowSOC != def.LowSOC || cfg.ShutdownSOC != def.ShutdownSOC {
			t.Fatalf("low=%s shutdown=%s: expected defaults, got low=%g shutdown=%g",
				tt.low, tt.shutdown, cfg.LowSOC, cfg.ShutdownSOC)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "BATTERY_SHUTDOWN_SOC") ||
			!strings.Contains(warnings[0], "BATTERY_LOW_SOC") {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	}
}

func TestLoadBatteryConfigShutdownSOCAboveDefaultLow(t *testing.T) {
	// SHUTDOWN_SOC=30 is in range but not below the default LOW_SOC=20.
	cfg, warnings := LoadBatteryConfig(mapEnv(map[string]string{"BATTERY_SHUTDOWN_SOC": "30"}))
	if cfg.ShutdownSOC != 5 || cfg.LowSOC != 20 {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
}

func TestLoadBatteryConfigStateDir(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		want     string
		warnings int
	}{
		{"default", nil, "/var/lib/raspicam", 0},
		{"STATE_DIR wins", map[string]string{"STATE_DIR": "/data", "STATE_DIRECTORY": "/var/lib/other"}, "/data", 0},
		{"STATE_DIRECTORY", map[string]string{"STATE_DIRECTORY": "/var/lib/raspicam-ui"}, "/var/lib/raspicam-ui", 0},
		{"STATE_DIRECTORY list", map[string]string{"STATE_DIRECTORY": "/var/lib/a:/var/lib/b"}, "/var/lib/a", 0},
		{"STATE_DIRECTORY relative", map[string]string{"STATE_DIRECTORY": "a:/var/lib/b"}, "/var/lib/raspicam", 1},
		{"STATE_DIR invalid", map[string]string{"STATE_DIR": "data", "STATE_DIRECTORY": "/var/lib/x"}, "/var/lib/raspicam", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, warnings := LoadBatteryConfig(mapEnv(tt.env))
			if cfg.StateDir != tt.want {
				t.Fatalf("StateDir = %q, want %q", cfg.StateDir, tt.want)
			}
			if len(warnings) != tt.warnings {
				t.Fatalf("warnings = %v, want %d", warnings, tt.warnings)
			}
		})
	}
}

func TestLoadBatteryConfigSampleIntervalRounding(t *testing.T) {
	tests := []struct {
		poll, sample string
		want         time.Duration
		warn         bool
	}{
		{"30s", "60s", 60 * time.Second, false},
		{"30s", "45s", 60 * time.Second, true},
		{"20s", "30s", 40 * time.Second, true},
		{"1m", "30s", time.Minute, true},
		{"10m", "30s", 10 * time.Minute, true},
		{"15s", "45s", 45 * time.Second, false},
		{"7m", "10m", 14 * time.Minute, true},
	}
	for _, tt := range tests {
		env := map[string]string{
			"BATTERY_POLL_INTERVAL":           tt.poll,
			"BATTERY_HISTORY_SAMPLE_INTERVAL": tt.sample,
		}
		cfg, warnings := LoadBatteryConfig(mapEnv(env))
		if cfg.HistorySampleInterval != tt.want {
			t.Fatalf("poll=%s sample=%s: got %s, want %s", tt.poll, tt.sample, cfg.HistorySampleInterval, tt.want)
		}
		if got := len(warnings) == 1; got != tt.warn || len(warnings) > 1 {
			t.Fatalf("poll=%s sample=%s: warnings %v", tt.poll, tt.sample, warnings)
		}
		if tt.warn && !strings.Contains(warnings[0], "rounded up") {
			t.Fatalf("unexpected warning %q", warnings[0])
		}
	}
}

func TestRoundUpToMultiple(t *testing.T) {
	tests := []struct{ d, step, want time.Duration }{
		{60, 30, 60},
		{61, 30, 90},
		{10, 30, 30},
		{0, 30, 30},
		{90, 30, 90},
	}
	for _, tt := range tests {
		if got := roundUpToMultiple(tt.d, tt.step); got != tt.want {
			t.Fatalf("roundUpToMultiple(%d, %d) = %d, want %d", tt.d, tt.step, got, tt.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"10MB", 10 << 20, false},
		{"10mb", 10 << 20, false},
		{"10Mb", 10 << 20, false},
		{"500KB", 500 << 10, false},
		{"500kb", 500 << 10, false},
		{"1GB", 1 << 30, false},
		{"1048576", 1 << 20, false},
		{"2048B", 2048, false},
		{"0", 0, false},
		{"", 0, true},
		{"MB", 0, true},
		{"1.5MB", 0, true},
		{"-1MB", 0, true},
		{"+1MB", 0, true},
		{"10 MB", 0, true},
		{"10TB", 0, true},
		{"10MiB", 0, true},
		{"abc", 0, true},
		{"99999999999GB", 0, true},
		{"99999999999999999999", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseSize(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		0:         "0B",
		1000:      "1000B",
		1 << 10:   "1KB",
		10 << 20:  "10MB",
		500 << 20: "500MB",
		2 << 30:   "2GB",
	}
	for in, want := range tests {
		if got := formatSize(in); got != want {
			t.Fatalf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseI2CAddr(t *testing.T) {
	tests := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{"0x36", 0x36, false},
		{"0X36", 0x36, false},
		{"54", 0x36, false},
		{"0x03", 0x03, false},
		{"0x77", 0x77, false},
		{"0x02", 0, true},
		{"0x78", 0, true},
		{"0x10000", 0, true},
		{"-1", 0, true},
		{"0xZZ", 0, true},
	}
	for _, tt := range tests {
		got, err := parseI2CAddr(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseI2CAddr(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseI2CAddr(%q) = %#x, want %#x", tt.in, got, tt.want)
		}
	}
}

func TestParseDurationRange(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"10s", 10 * time.Second, false},
		{"10m", 10 * time.Minute, false},
		{"1m30s", 90 * time.Second, false},
		{"9.999s", 0, true},
		{"10m1s", 0, true},
		{"10", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := parseDurationRange(tt.in, 10*time.Second, 10*time.Minute)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseDurationRange(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseDurationRange(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestParseFloatRange(t *testing.T) {
	tests := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"3.0", 3.0, false},
		{"3.8", 3.8, false},
		{"3.30", 3.30, false},
		{"2.999", 0, true},
		{"Inf", 0, true},
		{"NaN", 0, true},
		{"3,3", 0, true},
	}
	for _, tt := range tests {
		got, err := parseFloatRange(tt.in, 3.0, 3.8)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseFloatRange(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseFloatRange(%q) = %g, want %g", tt.in, got, tt.want)
		}
	}
}

func TestParseIntRange(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"1", 1, false},
		{"20", 20, false},
		{"0", 0, true},
		{"21", 0, true},
		{"3.0", 0, true},
		{"0x3", 0, true},
	}
	for _, tt := range tests {
		got, err := parseIntRange(tt.in, 1, 20)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseIntRange(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseIntRange(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseAbsPath(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"/dev/i2c-1", "/dev/i2c-1", false},
		{"/var/lib/raspicam/", "/var/lib/raspicam", false},
		{"/a/../b", "/b", false},
		{"", "", true},
		{"dev/i2c-1", "", true},
	}
	for _, tt := range tests {
		got, err := parseAbsPath(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseAbsPath(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("parseAbsPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadBatteryConfigMultipleWarnings(t *testing.T) {
	env := map[string]string{
		"BATTERY_ENABLED":       "nope",
		"BATTERY_I2C_ADDR":      "0x99",
		"BATTERY_POLL_INTERVAL": "1s",
	}
	cfg, warnings := LoadBatteryConfig(mapEnv(env))
	if !reflect.DeepEqual(cfg, DefaultBatteryConfig()) {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %v", warnings)
	}
}
