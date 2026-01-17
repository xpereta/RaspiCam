package focusassist

import (
	"errors"
	"image"
	"image/color"
	"math"
)

type Region string

const (
	RegionCenter Region = "center"
	RegionThirds Region = "thirds"
	RegionFull   Region = "full"
)

func ScoreImage(img image.Image, region Region) (float64, error) {
	if img == nil {
		return 0, errors.New("image is nil")
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return 0, errors.New("image bounds empty")
	}

	regionRect := regionBounds(bounds, region)
	if regionRect.Empty() {
		return 0, errors.New("region bounds empty")
	}

	if regionRect.Dx() < 3 || regionRect.Dy() < 3 {
		return 0, errors.New("region too small")
	}

	var sum float64
	var sumSq float64
	var count int

	minX := regionRect.Min.X + 1
	maxX := regionRect.Max.X - 1
	minY := regionRect.Min.Y + 1
	maxY := regionRect.Max.Y - 1

	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			center := luma(img.At(x, y))
			left := luma(img.At(x-1, y))
			right := luma(img.At(x+1, y))
			up := luma(img.At(x, y-1))
			down := luma(img.At(x, y+1))
			lap := left + right + up + down - 4*center
			sum += lap
			sumSq += lap * lap
			count++
		}
	}

	if count == 0 {
		return 0, errors.New("region too small")
	}

	mean := sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return variance, nil
}

func regionBounds(bounds image.Rectangle, region Region) image.Rectangle {
	width := bounds.Dx()
	height := bounds.Dy()

	switch region {
	case RegionCenter:
		startX := bounds.Min.X + width/3
		endX := bounds.Min.X + (2 * width / 3)
		startY := bounds.Min.Y + height/3
		endY := bounds.Min.Y + (2 * height / 3)
		return image.Rect(startX, startY, endX, endY)
	case RegionThirds:
		startX := bounds.Min.X + width/6
		endX := bounds.Min.X + (5 * width / 6)
		startY := bounds.Min.Y + height/6
		endY := bounds.Min.Y + (5 * height / 6)
		return image.Rect(startX, startY, endX, endY)
	case RegionFull:
		return bounds
	default:
		return bounds
	}
}

func luma(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
}

func ResizeNearest(src image.Image, targetWidth int) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == 0 || height == 0 || targetWidth <= 0 {
		return src
	}
	if width <= targetWidth {
		return src
	}
	scale := float64(targetWidth) / float64(width)
	newHeight := int(math.Round(float64(height) * scale))
	if newHeight < 1 {
		newHeight = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, newHeight))
	for y := 0; y < newHeight; y++ {
		srcY := int(math.Floor(float64(y) / scale))
		if srcY >= height {
			srcY = height - 1
		}
		for x := 0; x < targetWidth; x++ {
			srcX := int(math.Floor(float64(x) / scale))
			if srcX >= width {
				srcX = width - 1
			}
			dst.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}
