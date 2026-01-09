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
