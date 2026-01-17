package focusassist

import "testing"

func TestCaptureSettingsArgs(t *testing.T) {
	args := captureSettingsArgs(CaptureSettings{
		Mode:  "2304:1296:10:P",
		VFlip: true,
		HFlip: true,
	})
	expected := []string{"--vflip", "--hflip", "--mode", "2304:1296:10:P"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, arg := range expected {
		if args[i] != arg {
			t.Fatalf("expected arg %q at %d, got %q", arg, i, args[i])
		}
	}
}
