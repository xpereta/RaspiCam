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
