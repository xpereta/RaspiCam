package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Snapshot struct {
	CPUUsagePercent *float64
	TemperatureC    *float64
	VoltageV        *float64
	Throttled       *ThrottledStatus
}

type ThrottledStatus struct {
	Raw         uint32
	IsThrottled bool
	Flags       []string
}

func Collect(ctx context.Context) (Snapshot, []string) {
	var snap Snapshot
	var warnings []string

	if v, err := CPUUsagePercent(ctx, 150*time.Millisecond); err != nil {
		warnings = append(warnings, fmt.Sprintf("CPU usage unavailable: %v", err))
	} else {
		snap.CPUUsagePercent = &v
	}

	if v, err := MeasureTempC(ctx); err != nil {
		warnings = append(warnings, fmt.Sprintf("Temperature unavailable: %v", err))
	} else {
		snap.TemperatureC = &v
	}

	if v, err := MeasureVolts(ctx); err != nil {
		warnings = append(warnings, fmt.Sprintf("Voltage unavailable: %v", err))
	} else {
		snap.VoltageV = &v
	}

	if v, err := GetThrottled(ctx); err != nil {
		warnings = append(warnings, fmt.Sprintf("Throttling status unavailable: %v", err))
	} else {
		snap.Throttled = &v
	}

	return snap, warnings
}

func CPUUsagePercent(ctx context.Context, sampleDelay time.Duration) (float64, error) {
	idle1, total1, err := readProcStat(ctx)
	if err != nil {
		return 0, err
	}

	select {
	case <-time.After(sampleDelay):
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	idle2, total2, err := readProcStat(ctx)
	if err != nil {
		return 0, err
	}

	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta <= 0 {
		return 0, errors.New("invalid CPU sample")
	}

	usage := (1.0 - (idleDelta / totalDelta)) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}

	return usage, nil
}

func readProcStat(ctx context.Context) (uint64, uint64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, 0, err
		}
		return 0, 0, errors.New("missing /proc/stat line")
	}

	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	default:
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, errors.New("unexpected /proc/stat format")
	}

	var total uint64
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse /proc/stat: %w", err)
		}
		total += v
	}

	idle, err := strconv.ParseUint(fields[4], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse /proc/stat idle: %w", err)
	}

	return idle, total, nil
}

func MeasureTempC(ctx context.Context) (float64, error) {
	out, err := runVcgencmd(ctx, "measure_temp")
	if err != nil {
		return 0, err
	}
	return parseVcgencmdFloat(out, "temp=", "'C")
}

func MeasureVolts(ctx context.Context) (float64, error) {
	out, err := runVcgencmd(ctx, "measure_volts")
	if err != nil {
		return 0, err
	}
	return parseVcgencmdFloat(out, "volt=", "V")
}

func GetThrottled(ctx context.Context) (ThrottledStatus, error) {
	out, err := runVcgencmd(ctx, "get_throttled")
	if err != nil {
		return ThrottledStatus{}, err
	}

	value, err := parseHexValue(out, "throttled=")
	if err != nil {
		return ThrottledStatus{}, err
	}

	active := []string{}
	if value&0x1 != 0 {
		active = append(active, "under-voltage")
	}
	if value&0x2 != 0 {
		active = append(active, "arm frequency capped")
	}
	if value&0x4 != 0 {
		active = append(active, "currently throttled")
	}
	if value&0x8 != 0 {
		active = append(active, "soft temperature limit")
	}

	return ThrottledStatus{
		Raw:         value,
		IsThrottled: len(active) > 0,
		Flags:       active,
	}, nil
}

func runVcgencmd(ctx context.Context, arg string) (string, error) {
	cmd := exec.CommandContext(ctx, "vcgencmd", arg)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseVcgencmdFloat(out, prefix, suffix string) (float64, error) {
	out = strings.TrimSpace(out)
	if !strings.HasPrefix(out, prefix) {
		return 0, fmt.Errorf("unexpected output: %q", out)
	}
	value := strings.TrimPrefix(out, prefix)
	if suffix != "" {
		if !strings.HasSuffix(value, suffix) {
			return 0, fmt.Errorf("unexpected output: %q", out)
		}
		value = strings.TrimSuffix(value, suffix)
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse value: %w", err)
	}
	return parsed, nil
}

func parseHexValue(out, prefix string) (uint32, error) {
	out = strings.TrimSpace(out)
	if !strings.HasPrefix(out, prefix) {
		return 0, fmt.Errorf("unexpected output: %q", out)
	}
	value := strings.TrimPrefix(out, prefix)
	if strings.HasPrefix(value, "0x") {
		value = strings.TrimPrefix(value, "0x")
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("parse hex value: %w", err)
	}
	return uint32(parsed), nil
}
