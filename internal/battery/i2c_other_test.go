//go:build !linux

package battery

import (
	"errors"
	"testing"
)

func TestOpenBusUnsupported(t *testing.T) {
	_, err := OpenBus("/dev/i2c-1")
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("OpenBus err = %v, want wrapping errors.ErrUnsupported", err)
	}
}
