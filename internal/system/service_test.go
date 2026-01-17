package system

import "testing"

func TestControlErrorMessage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "auth required",
			input:  "Authentication is required to manage system services.",
			expect: "MediaMTX control requires systemd permissions.",
		},
		{
			name:   "access denied",
			input:  "Access denied",
			expect: "MediaMTX control requires systemd permissions.",
		},
		{
			name:   "permission denied",
			input:  "permission denied",
			expect: "MediaMTX control requires systemd permissions.",
		},
		{
			name:   "passthrough",
			input:  "some other error",
			expect: "some other error",
		},
	}

	for _, tt := range tests {
		if got := controlErrorMessage(tt.input); got != tt.expect {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.expect, got)
		}
	}
}
