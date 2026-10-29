//go:build !linux

package battery

import (
	"errors"
	"fmt"
)

// LinuxBus is unavailable on this platform; OpenBus always fails.
type LinuxBus struct{}

// OpenBus returns an error wrapping errors.ErrUnsupported on non-Linux
// systems, so the UI runs in degraded mode.
func OpenBus(path string) (*LinuxBus, error) {
	return nil, fmt.Errorf("open i2c bus %s: %w", path, errors.ErrUnsupported)
}

// ReadRegs always fails on non-Linux systems.
func (b *LinuxBus) ReadRegs(addr uint16, reg byte, buf []byte) error {
	return fmt.Errorf("i2c read 0x%02x reg 0x%02x: %w", addr, reg, errors.ErrUnsupported)
}

// Close always fails on non-Linux systems.
func (b *LinuxBus) Close() error {
	return fmt.Errorf("close i2c bus: %w", errors.ErrUnsupported)
}
