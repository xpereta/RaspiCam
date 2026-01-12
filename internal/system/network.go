package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type NetworkSnapshot struct {
	Interface        string
	IPAddress        string
	RxBytesPerSec    *float64
	TxBytesPerSec    *float64
	WiFiSSID         string
	WiFiTxRate       string
	WiFiRxRate       string
	WiFiLinkQuality  string
	wirelessDetected bool
}

func CollectNetwork(ctx context.Context) (NetworkSnapshot, []string) {
	var warnings []string

	iface, err := defaultRouteInterface()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Default route unavailable: %v", err))
	}
	if iface == "" {
		iface = firstActiveInterface()
		if iface == "" {
			warnings = append(warnings, "Network interface unavailable")
		}
	}

	snap := NetworkSnapshot{
		Interface: iface,
	}

	if iface != "" {
		ip, err := interfaceIPv4(iface)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("IP address unavailable: %v", err))
		} else if ip == "" {
			warnings = append(warnings, "IP address unavailable: no IPv4 found")
		} else {
			snap.IPAddress = ip
		}

		rxRate, txRate, err := sampleNetRates(ctx, iface, 200*time.Millisecond)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Network rates unavailable: %v", err))
		} else {
			snap.RxBytesPerSec = &rxRate
			snap.TxBytesPerSec = &txRate
		}

		quality, ok, err := wifiLinkQuality(iface)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("WiFi quality unavailable: %v", err))
		} else if ok {
			snap.WiFiLinkQuality = quality
			snap.wirelessDetected = true
		}

		if snap.wirelessDetected {
			ssid, txRate, rxRate, err := wifiLinkInfo(ctx, iface)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("WiFi details unavailable: %v", err))
			} else {
				snap.WiFiSSID = ssid
				snap.WiFiTxRate = txRate
				snap.WiFiRxRate = rxRate
			}
		}
	}

	return snap, warnings
}

func defaultRouteInterface() (string, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", errors.New("missing /proc/net/route header")
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[1] == "00000000" {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func firstActiveInterface() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			if ip := ipNet.IP.To4(); ip != nil {
				return iface.Name
			}
		}
	}
	return ""
}

func interfaceIPv4(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() {
			continue
		}
		return ip.String(), nil
	}
	return "", nil
}

func sampleNetRates(ctx context.Context, iface string, delay time.Duration) (float64, float64, error) {
	rx1, tx1, err := readNetDevBytes(iface)
	if err != nil {
		return 0, 0, err
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	case <-timer.C:
	}

	rx2, tx2, err := readNetDevBytes(iface)
	if err != nil {
		return 0, 0, err
	}

	if rx2 < rx1 || tx2 < tx1 {
		return 0, 0, errors.New("invalid /proc/net/dev sample")
	}

	seconds := delay.Seconds()
	if seconds <= 0 {
		return 0, 0, errors.New("invalid sample duration")
	}

	rxRate := float64(rx2-rx1) / seconds
	txRate := float64(tx2-tx1) / seconds
	return rxRate, txRate, nil
}

func readNetDevBytes(iface string) (uint64, uint64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}
		rxBytes, txBytes, ok, err := parseNetDevLine(line, iface)
		if err != nil {
			return 0, 0, err
		}
		if ok {
			return rxBytes, txBytes, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return 0, 0, fmt.Errorf("interface not found: %s", iface)
}

func parseNetDevLine(line, iface string) (uint64, uint64, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 17 {
		return 0, 0, false, nil
	}
	name := strings.TrimSuffix(fields[0], ":")
	if name != iface {
		return 0, 0, false, nil
	}
	rxBytes, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse rx bytes: %w", err)
	}
	txBytes, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse tx bytes: %w", err)
	}
	return rxBytes, txBytes, true, nil
}

func wifiLinkQuality(iface string) (string, bool, error) {
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}
		quality, ok, err := parseWirelessLine(line, iface)
		if err != nil {
			return "", false, err
		}
		if ok {
			return quality, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func parseWirelessLine(line, iface string) (string, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "", false, nil
	}
	name := strings.TrimSuffix(fields[0], ":")
	if name != iface {
		return "", false, nil
	}
	link, err := parseWirelessValue(fields[2])
	if err != nil {
		return "", false, fmt.Errorf("parse link quality: %w", err)
	}
	level, err := parseWirelessValue(fields[3])
	if err != nil {
		return "", false, fmt.Errorf("parse signal level: %w", err)
	}
	quality := fmt.Sprintf("%.0f/70", link)
	if level != 0 && level > -200 {
		quality = fmt.Sprintf("%s (%.0f dBm)", quality, level)
	}
	return quality, true, nil
}

func parseWirelessValue(value string) (float64, error) {
	value = strings.TrimSuffix(value, ".")
	return strconv.ParseFloat(value, 64)
}

func wifiLinkInfo(ctx context.Context, iface string) (string, string, string, error) {
	cmd := exec.CommandContext(ctx, "iw", "dev", iface, "link")
	out, err := cmd.Output()
	if err != nil {
		return "", "", "", err
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return "", "", "", errors.New("empty iw output")
	}
	if strings.Contains(output, "Not connected") {
		return "disconnected", "", "", nil
	}

	var ssid string
	var txRate string
	var rxRate string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SSID:"):
			ssid = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		case strings.HasPrefix(line, "tx bitrate:"):
			txRate = strings.TrimSpace(strings.TrimPrefix(line, "tx bitrate:"))
		case strings.HasPrefix(line, "rx bitrate:"):
			rxRate = strings.TrimSpace(strings.TrimPrefix(line, "rx bitrate:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", err
	}
	return ssid, txRate, rxRate, nil
}
