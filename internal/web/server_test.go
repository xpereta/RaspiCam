package web

import "testing"

func TestParseResolution(t *testing.T) {
	w, h, ok := parseResolution("1280x720")
	if !ok || w != 1280 || h != 720 {
		t.Fatalf("unexpected 1280x720 parse")
	}
	if _, _, ok := parseResolution("bad"); ok {
		t.Fatalf("expected invalid resolution")
	}
}

func TestIsValidAWB(t *testing.T) {
	if !isValidAWB("daylight") {
		t.Fatalf("expected daylight valid")
	}
	if isValidAWB("nope") {
		t.Fatalf("expected invalid awb")
	}
}

func TestIsValidCameraMode(t *testing.T) {
	if !isValidCameraMode("2304:1296:10:P") {
		t.Fatalf("expected mode valid")
	}
	if isValidCameraMode("bad") {
		t.Fatalf("expected invalid mode")
	}
}

func TestIsValidAFMode(t *testing.T) {
	if !isValidAFMode("manual") {
		t.Fatalf("expected manual valid")
	}
	if isValidAFMode("bad") {
		t.Fatalf("expected invalid af mode")
	}
}

func TestParseLensPosition(t *testing.T) {
	value, ok := parseLensPosition("1.25")
	if !ok || value != 1.25 {
		t.Fatalf("expected dot decimal")
	}
	value, ok = parseLensPosition("1,5")
	if !ok || value != 1.5 {
		t.Fatalf("expected comma decimal")
	}
	if _, ok := parseLensPosition("1.2.3"); ok {
		t.Fatalf("expected multiple dots invalid")
	}
	if _, ok := parseLensPosition("1,2,3"); ok {
		t.Fatalf("expected multiple commas invalid")
	}
	if _, ok := parseLensPosition("1,2.3"); ok {
		t.Fatalf("expected mixed separators invalid")
	}
	if _, ok := parseLensPosition(""); ok {
		t.Fatalf("expected empty invalid")
	}
}

func TestCameraMessageFromStatus(t *testing.T) {
	message, class := cameraMessageFromStatus("saved")
	if message == "" || class == "" {
		t.Fatalf("expected saved message")
	}
	message, class = cameraMessageFromStatus("invalid-mode")
	if message == "" || class == "" {
		t.Fatalf("expected invalid mode message")
	}
	message, class = cameraMessageFromStatus("nope")
	if message != "" || class != "" {
		t.Fatalf("expected empty message for unknown status")
	}
}

func TestParseFlickerPeriod(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"", 0, true},
		{"0", 0, true},
		{"10000", 10000, true},
		{"8333", 8333, true},
		{"20000", 20000, true},
		{"-1", 0, false},
		{"1000001", 0, false},
		{"10ms", 0, false},
		{"83.33", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseFlickerPeriod(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseFlickerPeriod(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFormatFlickerPeriod(t *testing.T) {
	if got := formatFlickerPeriod(0); got != "" {
		t.Errorf("formatFlickerPeriod(0) = %q; want empty", got)
	}
	if got := formatFlickerPeriod(10000); got != "10000" {
		t.Errorf("formatFlickerPeriod(10000) = %q; want 10000", got)
	}
}
