package focusassist

import "testing"

func TestBuildPositions(t *testing.T) {
	positions, err := buildPositions(0, 0.3, 0.1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positions) != 4 {
		t.Fatalf("expected 4 positions, got %d", len(positions))
	}
	if !floatEquals(positions[0], 0, 0.0001) || !floatEquals(positions[3], 0.3, 0.0001) {
		t.Fatalf("unexpected positions: %v", positions)
	}
}

func TestBuildPositionsInvalid(t *testing.T) {
	if _, err := buildPositions(1, 0, 0.1, 10); err == nil {
		t.Fatalf("expected error for end before start")
	}
	if _, err := buildPositions(0, 1, 0, 10); err == nil {
		t.Fatalf("expected error for step 0")
	}
}

func floatEquals(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}
