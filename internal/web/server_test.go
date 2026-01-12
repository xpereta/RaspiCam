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
