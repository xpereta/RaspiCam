// Package battery reads a MAX17048 fuel gauge and decides when the system
// must shut down because the battery is low.
package battery

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MAX17048 register addresses (16-bit, big-endian, MSB first).
const (
	RegVCell   byte = 0x02
	RegSOC     byte = 0x04
	RegVersion byte = 0x08
	RegCRate   byte = 0x16
	RegStatus  byte = 0x1A

	// StatusRI is the reset indicator bit in the STATUS register.
	StatusRI uint16 = 0x0100

	// DefaultAddr is the 7-bit I2C address of the MAX17048.
	DefaultAddr uint16 = 0x36
)

// MaxReadDuration bounds a successful Read. A slower read is reported as an
// error because a slow bus is not trustworthy.
const MaxReadDuration = 500 * time.Millisecond

// ErrSlowRead is returned (wrapped) by Read when the bus transactions
// succeeded but took longer than MaxReadDuration.
var ErrSlowRead = errors.New("slow read")

// Plausibility bounds for a reading.
const (
	minPlausibleVolts = 2.5
	maxPlausibleVolts = 4.5
	minPlausibleSOC   = 0
	maxPlausibleSOC   = 110
)

// Reading is one decoded sample from the gauge.
type Reading struct {
	Time  time.Time // wall clock with monotonic component
	Volts float64
	SOC   float64 // raw, may exceed 100; clamp only for display
	CRate float64 // %/hour, negative while discharging
}

// Gauge is a source of battery readings.
type Gauge interface {
	Read(ctx context.Context) (Reading, error)
}

// Bus is an I2C bus that supports combined write-register/read transactions.
type Bus interface {
	ReadRegs(addr uint16, reg byte, buf []byte) error
	Close() error
}

func be16(b [2]byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

// decodeVCell converts the VCELL register to volts (78.125uV per LSB).
func decodeVCell(b [2]byte) float64 {
	return float64(be16(b)) * 78.125e-6
}

// decodeSOC converts the SOC register to percent (1/256 % per LSB).
func decodeSOC(b [2]byte) float64 {
	return float64(be16(b)) / 256.0
}

// decodeCRate converts the signed CRATE register to %/hour (0.208 per LSB).
func decodeCRate(b [2]byte) float64 {
	return float64(int16(be16(b))) * 0.208
}

// plausible reports whether r is within physically sensible bounds.
func plausible(r Reading) error {
	// Written as !(in range) so NaN is rejected too.
	if !(r.Volts >= minPlausibleVolts && r.Volts <= maxPlausibleVolts) {
		return fmt.Errorf("implausible voltage %.3fV (want %.1f-%.1fV)", r.Volts, minPlausibleVolts, maxPlausibleVolts)
	}
	if !(r.SOC >= minPlausibleSOC && r.SOC <= maxPlausibleSOC) {
		return fmt.Errorf("implausible SOC %.2f%% (want %d-%d%%)", r.SOC, minPlausibleSOC, maxPlausibleSOC)
	}
	return nil
}

// versionExpected reports whether v matches the datasheet's 0x001x pattern.
func versionExpected(v uint16) bool {
	return v&0xFFF0 == 0x0010
}

// MAX17048 is a Gauge backed by a MAX17048 on an I2C bus. It never writes
// to the chip. Read is not safe for concurrent use; a single goroutine
// should own the gauge.
type MAX17048 struct {
	bus       Bus
	addr      uint16
	now       func() time.Time
	version   uint16
	resetSeen bool
	warning   string

	cellBuf [4]byte
	rateBuf [2]byte
}

// NewMAX17048 detects the gauge by reading VERSION and STATUS once. An
// unexpected VERSION does not fail; it is reported by Warning.
func NewMAX17048(bus Bus, addr uint16) (*MAX17048, error) {
	if bus == nil {
		return nil, errors.New("max17048: nil bus")
	}
	m := &MAX17048{bus: bus, addr: addr, now: time.Now}

	var b [2]byte
	if err := bus.ReadRegs(addr, RegVersion, b[:]); err != nil {
		return nil, fmt.Errorf("max17048: read VERSION at 0x%02x: %w", addr, err)
	}
	m.version = be16(b)
	if !versionExpected(m.version) {
		m.warning = fmt.Sprintf("max17048: unexpected VERSION 0x%04x (want 0x001x)", m.version)
	}

	if err := bus.ReadRegs(addr, RegStatus, b[:]); err != nil {
		return nil, fmt.Errorf("max17048: read STATUS at 0x%02x: %w", addr, err)
	}
	m.resetSeen = be16(b)&StatusRI != 0
	return m, nil
}

// Version returns the VERSION register read at detection.
func (m *MAX17048) Version() uint16 { return m.version }

// ResetSeen reports whether the STATUS RI bit was set at detection.
func (m *MAX17048) ResetSeen() bool { return m.resetSeen }

// Warning returns a non-empty message when detection found something
// unexpected (such as an unknown VERSION) that did not prevent use.
func (m *MAX17048) Warning() string { return m.warning }

// Close closes the underlying bus.
func (m *MAX17048) Close() error { return m.bus.Close() }

// Read performs one 4-byte read at VCELL (VCELL + SOC) and one 2-byte read
// at CRATE. ctx is checked before each transaction; the transactions
// themselves are bounded by the kernel adapter timeout. A read taking longer
// than MaxReadDuration is returned as an error.
func (m *MAX17048) Read(ctx context.Context) (Reading, error) {
	start := m.now()

	if err := ctx.Err(); err != nil {
		return Reading{}, fmt.Errorf("max17048: read VCELL/SOC: %w", err)
	}
	if err := m.bus.ReadRegs(m.addr, RegVCell, m.cellBuf[:]); err != nil {
		return Reading{}, fmt.Errorf("max17048: read VCELL/SOC: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return Reading{}, fmt.Errorf("max17048: read CRATE: %w", err)
	}
	if err := m.bus.ReadRegs(m.addr, RegCRate, m.rateBuf[:]); err != nil {
		return Reading{}, fmt.Errorf("max17048: read CRATE: %w", err)
	}

	if d := m.now().Sub(start); d > MaxReadDuration {
		return Reading{}, fmt.Errorf("max17048: read took %v (max %v): %w", d, MaxReadDuration, ErrSlowRead)
	}

	return Reading{
		Time:  start,
		Volts: decodeVCell([2]byte{m.cellBuf[0], m.cellBuf[1]}),
		SOC:   decodeSOC([2]byte{m.cellBuf[2], m.cellBuf[3]}),
		CRate: decodeCRate(m.rateBuf),
	}, nil
}
