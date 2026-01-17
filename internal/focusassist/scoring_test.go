package focusassist

import (
	"image"
	"image/color"
	"testing"
)

func TestScoreImageDetectsEdges(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if x < 10 {
				img.Set(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}

	edgeScore, err := ScoreImage(img, RegionFull)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	flat := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			flat.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}

	flatScore, err := ScoreImage(flat, RegionFull)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if edgeScore <= flatScore {
		t.Fatalf("expected edge score > flat score, got %.2f vs %.2f", edgeScore, flatScore)
	}
}

func TestScoreImageRegionTooSmall(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	_, err := ScoreImage(img, RegionCenter)
	if err == nil {
		t.Fatalf("expected error for small image")
	}
}
