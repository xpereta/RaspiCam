package system

import (
	"bufio"
	"io"
	"os"
	"strings"
)

type Info struct {
	Model     string
	OSName    string
	OSVersion string
	OSLabel   string
}

func Collect() Info {
	name := osName()
	version := osVersion()
	return Info{
		Model:     deviceModel(),
		OSName:    name,
		OSVersion: version,
		OSLabel:   buildOSLabel(name, version),
	}
}

func deviceModel() string {
	paths := []string{
		"/proc/device-tree/model",
		"/sys/firmware/devicetree/base/model",
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(b))
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func osRelease() map[string]string {
	result := map[string]string{}
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return result
	}
	defer file.Close()
	return parseOSRelease(file)
}

func osName() string {
	fields := osRelease()
	if name := fields["PRETTY_NAME"]; name != "" {
		return name
	}
	if name := fields["NAME"]; name != "" {
		return name
	}
	return "unknown"
}

func osVersion() string {
	fields := osRelease()
	if version := fields["VERSION"]; version != "" {
		return version
	}
	if version := fields["VERSION_ID"]; version != "" {
		return version
	}
	return "unknown"
}

func buildOSLabel(name, version string) string {
	if name == "unknown" && version == "unknown" {
		return "unknown"
	}
	if version == "unknown" {
		return name
	}
	if name == "unknown" {
		return version
	}
	return name + " " + version
}

func parseOSRelease(r io.Reader) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.Trim(parts[1], "\"")
		if key != "" {
			result[key] = value
		}
	}
	return result
}
