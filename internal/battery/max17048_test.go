package battery

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestDecodeVCell(t *testing.T) {
	cases := []struct {
		name string
		in   [2]byte
		want float64
	}{
		{"zero", [2]byte{0x00, 0x00}, 0},
		{"one lsb", [2]byte{0x00, 0x01}, 78.125e-6},
		{"4.16V", [2]byte{0xD0, 0x00}, 4.16},
		{"3.30V", [2]byte{0xA5, 0x00}, 3.3},
		{"3.7V", [2]byte{0xB9, 0x00}, 3.7},
		{"max", [2]byte{0xFF, 0xFF}, 65535 * 78.125e-6},
	}
	for _, tc := range cases {
		if got := decodeVCell(tc.in); !approx(got, tc.want) {
			t.Errorf("%s: decodeVCell(%x) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestDecodeSOC(t *testing.T) {
	cases := []struct {
		name string
		in   [2]byte
		want float64
	}{
		{"zero", [2]byte{0x00, 0x00}, 0},
		{"one lsb", [2]byte{0x00, 0x01}, 1.0 / 256},
		{"50.5%", [2]byte{0x32, 0x80}, 50.5},
		{"100%", [2]byte{0x64, 0x00}, 100},
		{"above 100", [2]byte{0x65, 0x40}, 101.25},
		{"max", [2]byte{0xFF, 0xFF}, 65535.0 / 256},
	}
	for _, tc := range cases {
		if got := decodeSOC(tc.in); !approx(got, tc.want) {
			t.Errorf("%s: decodeSOC(%x) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestDecodeCRate(t *testing.T) {
	cases := []struct {
		name string
		in   [2]byte
		want float64
	}{
		{"zero", [2]byte{0x00, 0x00}, 0},
		{"plus one lsb", [2]byte{0x00, 0x01}, 0.208},
		{"minus one lsb", [2]byte{0xFF, 0xFF}, -0.208},
		{"charging 10.4%/h", [2]byte{0x00, 0x32}, 10.4},
		{"discharging -10.4%/h", [2]byte{0xFF, 0xCE}, -10.4},
		{"max positive", [2]byte{0x7F, 0xFF}, 32767 * 0.208},
		{"max negative", [2]byte{0x80, 0x00}, -32768 * 0.208},
	}
	for _, tc := range cases {
		if got := decodeCRate(tc.in); !approx(got, tc.want) {
			t.Errorf("%s: decodeCRate(%x) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestPlausible(t *testing.T) {
	cases := []struct {
		name  string
		volts float64
		soc   float64
		ok    bool
	}{
		{"typical", 3.8, 60, true},
		{"min bounds", 2.5, 0, true},
		{"max bounds", 4.5, 110, true},
		{"volts low", 2.49, 50, false},
		{"volts high", 4.51, 50, false},
		{"volts zero", 0, 50, false},
		{"soc negative", 3.8, -0.1, false},
		{"soc high", 3.8, 110.1, false},
		{"volts NaN", math.NaN(), 50, false},
		{"soc NaN", 3.8, math.NaN(), false},
	}
	for _, tc := range cases {
		err := plausible(Reading{Volts: tc.volts, SOC: tc.soc})
		if (err == nil) != tc.ok {
			t.Errorf("%s: plausible err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// fakeBus serves fixed register contents and records reads.
type fakeBus struct {
	regs   map[byte][]byte
	errs   map[byte]error
	reads  []byte
	onRead func(reg byte)
	closed bool
}

func (f *fakeBus) ReadRegs(addr uint16, reg byte, buf []byte) error {
	f.reads = append(f.reads, reg)
	if f.onRead != nil {
		f.onRead(reg)
	}
	if err := f.errs[reg]; err != nil {
		return err
	}
	src, ok := f.regs[reg]
	if !ok || len(src) < len(buf) {
		return errors.New("fake: no data")
	}
	copy(buf, src)
	return nil
}

func (f *fakeBus) Close() error { f.closed = true; return nil }

func newFakeBus() *fakeBus {
	return &fakeBus{
		regs: map[byte][]byte{
			RegVersion: {0x00, 0x12},
			RegStatus:  {0x01, 0x00},
			RegVCell:   {0xB9, 0x00, 0x32, 0x80}, // 3.7V, 50.5%
			RegCRate:   {0xFF, 0xCE},             // -10.4 %/h
		},
		errs: map[byte]error{},
	}
}

func TestNewMAX17048Detection(t *testing.T) {
	errBus := errors.New("nak")
	cases := []struct {
		name      string
		version   []byte
		status    []byte
		errReg    byte
		wantErr   bool
		wantWarn  bool
		wantReset bool
	}{
		{"expected version, RI set", []byte{0x00, 0x12}, []byte{0x01, 0x00}, 0, false, false, true},
		{"expected version, RI clear", []byte{0x00, 0x11}, []byte{0x00, 0x00}, 0, false, false, false},
		{"unexpected version", []byte{0x00, 0x21}, []byte{0x00, 0x00}, 0, false, true, false},
		{"version read fails", nil, nil, RegVersion, true, false, false},
		{"status read fails", []byte{0x00, 0x12}, nil, RegStatus, true, false, false},
	}
	for _, tc := range cases {
		bus := newFakeBus()
		bus.regs[RegVersion] = tc.version
		bus.regs[RegStatus] = tc.status
		if tc.errReg != 0 {
			bus.errs[tc.errReg] = errBus
		}
		m, err := NewMAX17048(bus, DefaultAddr)
		if tc.wantErr {
			if !errors.Is(err, errBus) {
				t.Errorf("%s: err = %v, want wrapping %v", tc.name, err, errBus)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got := m.Warning() != ""; got != tc.wantWarn {
			t.Errorf("%s: warning = %q, want warn=%v", tc.name, m.Warning(), tc.wantWarn)
		}
		if m.ResetSeen() != tc.wantReset {
			t.Errorf("%s: ResetSeen = %v, want %v", tc.name, m.ResetSeen(), tc.wantReset)
		}
		if m.Version() != uint16(tc.version[0])<<8|uint16(tc.version[1]) {
			t.Errorf("%s: Version = 0x%04x", tc.name, m.Version())
		}
	}

	if _, err := NewMAX17048(nil, DefaultAddr); err == nil {
		t.Error("nil bus: want error")
	}
}

func TestMAX17048Read(t *testing.T) {
	bus := newFakeBus()
	m, err := NewMAX17048(bus, DefaultAddr)
	if err != nil {
		t.Fatalf("NewMAX17048: %v", err)
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }
	bus.reads = nil

	r, err := m.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !approx(r.Volts, 3.7) || !approx(r.SOC, 50.5) || !approx(r.CRate, -10.4) || !r.Time.Equal(base) {
		t.Errorf("Read = %+v", r)
	}
	if len(bus.reads) != 2 || bus.reads[0] != RegVCell || bus.reads[1] != RegCRate {
		t.Errorf("register reads = %x, want [02 16]", bus.reads)
	}
	if err := m.Close(); err != nil || !bus.closed {
		t.Errorf("Close: err=%v closed=%v", err, bus.closed)
	}
}

func TestMAX17048ReadErrors(t *testing.T) {
	errBus := errors.New("eremoteio")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name      string
		ctx       context.Context
		setup     func(bus *fakeBus, ctxCancel context.CancelFunc)
		delay     time.Duration // fake time spent in each bus read
		wantIs    error
		wantReads int
	}{
		{"ctx cancelled before first read", cancelled, nil, 0, context.Canceled, 0},
		{"cell read fails", nil, func(b *fakeBus, _ context.CancelFunc) { b.errs[RegVCell] = errBus }, 0, errBus, 1},
		{"crate read fails", nil, func(b *fakeBus, _ context.CancelFunc) { b.errs[RegCRate] = errBus }, 0, errBus, 2},
		{"ctx cancelled between reads", nil, func(b *fakeBus, c context.CancelFunc) {
			b.onRead = func(reg byte) {
				if reg == RegVCell {
					c()
				}
			}
		}, 0, context.Canceled, 1},
		{"slow read is an error", nil, nil, 251 * time.Millisecond, ErrSlowRead, 2},
		{"exactly 500ms is fine", nil, nil, 250 * time.Millisecond, nil, 2},
	}
	for _, tc := range cases {
		bus := newFakeBus()
		m, err := NewMAX17048(bus, DefaultAddr)
		if err != nil {
			t.Fatalf("%s: NewMAX17048: %v", tc.name, err)
		}
		bus.reads = nil

		ctx := tc.ctx
		ctxCancel := context.CancelFunc(func() {})
		if ctx == nil {
			ctx, ctxCancel = context.WithCancel(context.Background())
		}
		if tc.setup != nil {
			tc.setup(bus, ctxCancel)
		}
		clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		m.now = func() time.Time { return clock }
		if tc.delay > 0 {
			bus.onRead = func(byte) { clock = clock.Add(tc.delay) }
		}

		_, err = m.Read(ctx)
		ctxCancel()
		if tc.wantIs == nil {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", tc.name, err)
			}
		} else if !errors.Is(err, tc.wantIs) {
			t.Errorf("%s: err = %v, want wrapping %v", tc.name, err, tc.wantIs)
		}
		if len(bus.reads) != tc.wantReads {
			t.Errorf("%s: %d bus reads, want %d", tc.name, len(bus.reads), tc.wantReads)
		}
	}
}

func TestOpenBusMissingDevice(t *testing.T) {
	if _, err := OpenBus("/dev/i2c-does-not-exist"); err == nil {
		t.Fatal("OpenBus: want error for missing device")
	}
}
