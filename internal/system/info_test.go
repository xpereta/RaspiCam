package system

import (
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	input := `NAME="Debian GNU/Linux"
VERSION_ID="12"
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
# comment
INVALID_LINE
`

	fields := parseOSRelease(strings.NewReader(input))
	if fields["NAME"] != "Debian GNU/Linux" {
		t.Fatalf("unexpected NAME: %q", fields["NAME"])
	}
	if fields["VERSION_ID"] != "12" {
		t.Fatalf("unexpected VERSION_ID: %q", fields["VERSION_ID"])
	}
	if fields["PRETTY_NAME"] != "Debian GNU/Linux 12 (bookworm)" {
		t.Fatalf("unexpected PRETTY_NAME: %q", fields["PRETTY_NAME"])
	}
}

func TestBuildOSLabel(t *testing.T) {
	if got := buildOSLabel("Pretty OS 1.0", "Name", "1.0"); got != "Pretty OS 1.0" {
		t.Fatalf("unexpected pretty label: %q", got)
	}
	if got := buildOSLabel("", "Name", "1.0"); got != "Name 1.0" {
		t.Fatalf("unexpected label: %q", got)
	}
	if got := buildOSLabel("", "unknown", "unknown"); got != "unknown" {
		t.Fatalf("unexpected unknown label: %q", got)
	}
}

func TestMapCameraModel(t *testing.T) {
	if got := mapCameraModel("imx708"); got != "Pi Camera v3 (imx708)" {
		t.Fatalf("unexpected model: %q", got)
	}
	if got := mapCameraModel("ov5647"); got != "Pi Camera v1 (ov5647)" {
		t.Fatalf("unexpected model: %q", got)
	}
	if got := mapCameraModel("unknown"); got != "Unknown camera (unknown)" {
		t.Fatalf("unexpected model: %q", got)
	}
}
