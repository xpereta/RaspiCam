package system

import "testing"

func TestExtractCameraCode(t *testing.T) {
	input := "random imx219 stuff"
	if got := extractCameraCode(input); got != "imx219" {
		t.Fatalf("unexpected code: %q", got)
	}
	if got := extractCameraCode("no match here"); got != "" {
		t.Fatalf("expected empty code")
	}
}
