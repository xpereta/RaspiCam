package focusassist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"sync"
	"time"
)

const (
	StateIdle    = "idle"
	StateRunning = "running"
	StateFailed  = "failed"
	StateDone    = "done"
)

var (
	ErrScanRunning        = errors.New("scan already running")
	ErrCaptureUnavailable = errors.New("capture unavailable")
)

type Capturer interface {
	Capture(ctx context.Context, lensPosition float64) ([]byte, error)
}

type Status struct {
	State       string
	Message     string
	Total       int
	Completed   int
	CurrentLens float64
	StartedAt   time.Time
	FinishedAt  time.Time
	UpdatedAt   time.Time
}

type Result struct {
	ID           string
	LensPosition float64
	Score        float64
}

type Snapshot struct {
	Status    Status
	Results   []Result
	BestID    string
	UpdatedAt time.Time
}

type ImageKind string

const (
	ImageThumb ImageKind = "thumb"
	ImageFull  ImageKind = "full"
)

type ScanParams struct {
	Start  float64
	End    float64
	Step   float64
	Region Region
}

type Service struct {
	mu             sync.Mutex
	capturer       Capturer
	status         Status
	results        []Result
	bestID         string
	images         map[string]imageSet
	captureTimeout time.Duration
	maxSteps       int
}

type imageSet struct {
	full  []byte
	thumb []byte
}

func NewService(capturer Capturer) *Service {
	return &Service{
		capturer:       capturer,
		status:         Status{State: StateIdle},
		images:         make(map[string]imageSet),
		captureTimeout: 5 * time.Second,
		maxSteps:       80,
	}
}

func (s *Service) StartScan(ctx context.Context, params ScanParams) error {
	if s.capturer == nil {
		return ErrCaptureUnavailable
	}
	positions, err := buildPositions(params.Start, params.End, params.Step, s.maxSteps)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.status.State == StateRunning {
		s.mu.Unlock()
		return ErrScanRunning
	}

	s.status = Status{
		State:     StateRunning,
		Total:     len(positions),
		Completed: 0,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.results = nil
	s.bestID = ""
	s.images = make(map[string]imageSet)
	s.mu.Unlock()

	go s.runScan(ctx, params.Region, positions)
	return nil
}

func (s *Service) runScan(ctx context.Context, region Region, positions []float64) {
	for i, position := range positions {
		s.updateStatus(func(status *Status) {
			status.CurrentLens = position
			status.UpdatedAt = time.Now()
			status.Message = fmt.Sprintf("Capturing %.2f", position)
		})

		stepCtx, cancel := context.WithTimeout(ctx, s.captureTimeout)
		data, err := s.capturer.Capture(stepCtx, position)
		cancel()
		if err != nil {
			s.failScan(fmt.Sprintf("capture failed at %.2f: %v", position, err))
			return
		}

		img, err := decodeImage(data)
		if err != nil {
			s.failScan(fmt.Sprintf("decode failed at %.2f: %v", position, err))
			return
		}

		score, err := ScoreImage(img, region)
		if err != nil {
			s.failScan(fmt.Sprintf("score failed at %.2f: %v", position, err))
			return
		}

		fullImage := ResizeNearest(img, 640)
		thumbImage := ResizeNearest(img, 160)

		fullBytes, err := encodeJPEG(fullImage)
		if err != nil {
			s.failScan(fmt.Sprintf("encode failed at %.2f: %v", position, err))
			return
		}
		thumbBytes, err := encodeJPEG(thumbImage)
		if err != nil {
			s.failScan(fmt.Sprintf("encode failed at %.2f: %v", position, err))
			return
		}

		id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), i)

		s.mu.Lock()
		s.results = append(s.results, Result{ID: id, LensPosition: position, Score: score})
		s.images[id] = imageSet{full: fullBytes, thumb: thumbBytes}
		if s.bestID == "" || score > s.bestScoreLocked() {
			s.bestID = id
		}
		s.status.Completed = i + 1
		s.status.Message = fmt.Sprintf("Captured %.2f", position)
		s.status.UpdatedAt = time.Now()
		s.mu.Unlock()
	}

	s.updateStatus(func(status *Status) {
		status.State = StateDone
		status.Message = "Scan complete"
		status.FinishedAt = time.Now()
		status.UpdatedAt = time.Now()
	})
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make([]Result, len(s.results))
	copy(results, s.results)
	status := s.status

	return Snapshot{
		Status:    status,
		Results:   results,
		BestID:    s.bestID,
		UpdatedAt: status.UpdatedAt,
	}
}

func (s *Service) Image(id string, kind ImageKind) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	img, ok := s.images[id]
	if !ok {
		return nil, false
	}

	switch kind {
	case ImageFull:
		return img.full, true
	case ImageThumb:
		return img.thumb, true
	default:
		return nil, false
	}
}

func (s *Service) updateStatus(fn func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.status)
}

func (s *Service) failScan(message string) {
	s.updateStatus(func(status *Status) {
		status.State = StateFailed
		status.Message = message
		status.FinishedAt = time.Now()
		status.UpdatedAt = time.Now()
	})
}

func (s *Service) bestScoreLocked() float64 {
	for _, result := range s.results {
		if result.ID == s.bestID {
			return result.Score
		}
	}
	return math.SmallestNonzeroFloat64
}

func buildPositions(start, end, step float64, maxSteps int) ([]float64, error) {
	if step <= 0 {
		return nil, errors.New("step must be greater than zero")
	}
	if end < start {
		return nil, errors.New("end must be greater than or equal to start")
	}
	positions := make([]float64, 0, maxSteps)
	epsilon := step * 0.001
	for i := 0; ; i++ {
		pos := start + float64(i)*step
		if pos > end+epsilon {
			break
		}
		positions = append(positions, pos)
		if len(positions) > maxSteps {
			return nil, fmt.Errorf("step count exceeds limit (%d)", maxSteps)
		}
	}
	if len(positions) == 0 {
		return nil, errors.New("no positions to scan")
	}
	return positions, nil
}

func decodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("empty image data")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func encodeJPEG(img image.Image) ([]byte, error) {
	buf := &bytes.Buffer{}
	options := &jpeg.Options{Quality: 85}
	if err := jpeg.Encode(buf, img, options); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
