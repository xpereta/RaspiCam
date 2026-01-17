package focusassist

import (
	"context"
	"testing"
	"time"
)

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

func TestWaitForCompletion(t *testing.T) {
	service := &Service{
		status: Status{State: StateRunning},
	}
	done := make(chan struct{})
	go func() {
		service.updateStatus(func(status *Status) {
			status.State = StateDone
		})
		close(done)
	}()
	<-done

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	status, ok := service.WaitForCompletion(ctx)
	if !ok {
		t.Fatalf("expected wait to succeed")
	}
	if status.State != StateDone {
		t.Fatalf("expected done state, got %s", status.State)
	}
}

func TestScanTimeout(t *testing.T) {
	service := &Service{
		captureTimeout: 3 * time.Second,
		scanOverhead:   time.Second,
		maxSteps:       10,
	}
	timeout, err := service.ScanTimeout(ScanParams{
		Start:  0,
		End:    0.2,
		Step:   0.1,
		Region: RegionCenter,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := 3*time.Second + time.Second
	minimum := expected*3 + 10*time.Second
	if timeout != minimum {
		t.Fatalf("expected %v, got %v", minimum, timeout)
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

func TestImageKinds(t *testing.T) {
	service := &Service{
		images: map[string]imageSet{
			"sample": {
				full:     []byte("full"),
				thumb:    []byte("thumb"),
				original: []byte("original"),
			},
		},
	}

	if data, ok := service.Image("sample", ImageFull); !ok || string(data) != "full" {
		t.Fatalf("expected full image, got %v (%v)", string(data), ok)
	}
	if data, ok := service.Image("sample", ImageThumb); !ok || string(data) != "thumb" {
		t.Fatalf("expected thumb image, got %v (%v)", string(data), ok)
	}
	if data, ok := service.Image("sample", ImageFullRes); !ok || string(data) != "original" {
		t.Fatalf("expected fullres image, got %v (%v)", string(data), ok)
	}
	if _, ok := service.Image("missing", ImageFull); ok {
		t.Fatalf("expected missing image to return ok=false")
	}
}

func floatEquals(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}
