package battery

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// callRecorder records the order of shutdown steps.
type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (c *callRecorder) add(s string) {
	c.mu.Lock()
	c.calls = append(c.calls, s)
	c.mu.Unlock()
}

func (c *callRecorder) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

type fakeFlusher struct {
	rec    *callRecorder
	ctxErr error
	err    error
}

func (f *fakeFlusher) Flush(ctx context.Context) error {
	f.ctxErr = ctx.Err()
	f.rec.add("flush")
	return f.err
}

type fakeFinalizer struct {
	rec     *callRecorder
	log     *ShutdownLog
	logLen  int
	gotEv   Event
	err     error
	release chan struct{} // when set, blocks until closed
}

func (f *fakeFinalizer) FinalizeShutdown(ctx context.Context, ev Event) error {
	if f.log != nil {
		evs, _ := f.log.List()
		f.logLen = len(evs)
	}
	f.gotEv = ev
	f.rec.add("finalize")
	if f.release != nil {
		<-f.release
	}
	return f.err
}

type fakePower struct {
	rec    *callRecorder
	mu     sync.Mutex
	errs   []error // returned in order; nil after exhausted
	calls  int
	ctxErr error
}

func (p *fakePower) PowerOff(ctx context.Context) error {
	p.rec.add("power")
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ctxErr = ctx.Err()
	p.calls++
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		return err
	}
	return nil
}

func (p *fakePower) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func quietLogf(string, ...any) {}

func newTestShutdown(t *testing.T, dryRun bool, power *fakePower, fl *fakeFlusher, fin *fakeFinalizer) (*Shutdown, *ShutdownLog) {
	t.Helper()
	l := NewShutdownLog(filepath.Join(t.TempDir(), "shutdowns.jsonl"))
	l.Logf = quietLogf
	if fin != nil {
		fin.log = l
	}
	var flushers []Flusher
	if fl != nil {
		flushers = append(flushers, fl)
	}
	var finalizers []ShutdownFinalizer
	if fin != nil {
		finalizers = append(finalizers, fin)
	}
	var p PowerOff
	if power != nil {
		p = power
	}
	sd := NewShutdown(l, p, dryRun, flushers, finalizers,
		WithShutdownLogf(quietLogf),
		WithTimeSynced(func() bool { return true }),
		WithShutdownClock(func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }))
	return sd, l
}

func TestShutdownOrder(t *testing.T) {
	rec := &callRecorder{}
	power := &fakePower{rec: rec}
	fl := &fakeFlusher{rec: rec, err: errors.New("flush failed")}
	fin := &fakeFinalizer{rec: rec, err: errors.New("finalize failed")}
	sd, l := newTestShutdown(t, false, power, fl, fin)

	ev := Event{Reason: ReasonManual, Client: "10.0.0.2:1234"}
	if err := sd.Execute(context.Background(), ev); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := rec.get(), []string{"finalize", "flush", "power"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if fin.logLen != 1 {
		t.Errorf("log had %d entries when finalizing, want 1 (log first)", fin.logLen)
	}
	if !fin.gotEv.Synced || fin.gotEv.DryRun || fin.gotEv.Time.IsZero() {
		t.Errorf("finalizer event = %+v, want synced, not dry run, time set", fin.gotEv)
	}
	if !sd.Pending() {
		t.Error("Pending = false after successful power off")
	}
	if err := sd.Execute(context.Background(), ev); !errors.Is(err, ErrShutdownInProgress) {
		t.Errorf("second Execute = %v, want ErrShutdownInProgress", err)
	}
	evs, err := l.List()
	if err != nil || len(evs) != 1 || evs[0].Client != ev.Client || evs[0].Reason != ReasonManual {
		t.Errorf("log = %+v, %v", evs, err)
	}
}

func TestShutdownDryRun(t *testing.T) {
	rec := &callRecorder{}
	power := &fakePower{rec: rec}
	fl := &fakeFlusher{rec: rec}
	fin := &fakeFinalizer{rec: rec}
	sd, l := newTestShutdown(t, true, power, fl, fin)
	if !sd.DryRun() {
		t.Fatal("DryRun = false")
	}

	for i := 0; i < 2; i++ {
		if err := sd.Execute(context.Background(), Event{Reason: ReasonLowBattery, Threshold: ReasonSOC}); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if got, want := rec.get(), []string{"flush", "flush"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if sd.Pending() {
		t.Error("Pending = true after dry run")
	}
	evs, err := l.List()
	if err != nil || len(evs) != 2 || !evs[0].DryRun {
		t.Errorf("log = %+v, %v; want 2 dry-run events", evs, err)
	}
}

func TestShutdownRejectsWhileInProgress(t *testing.T) {
	rec := &callRecorder{}
	power := &fakePower{rec: rec}
	fin := &fakeFinalizer{rec: rec, release: make(chan struct{})}
	sd, _ := newTestShutdown(t, false, power, nil, fin)

	done := make(chan error, 1)
	go func() { done <- sd.Execute(context.Background(), Event{Reason: ReasonManual}) }()

	// Wait until the first call is inside the finalizer.
	deadline := time.Now().Add(5 * time.Second)
	for len(rec.get()) == 0 {
		if time.Now().After(deadline) {
			close(fin.release)
			t.Fatal("first Execute never reached the finalizer")
		}
		time.Sleep(time.Millisecond)
	}
	if !sd.Pending() {
		t.Error("Pending = false while executing")
	}
	err := sd.Execute(context.Background(), Event{Reason: ReasonLowBattery})
	close(fin.release)
	if !errors.Is(err, ErrShutdownInProgress) {
		t.Errorf("concurrent Execute = %v, want ErrShutdownInProgress", err)
	}
	if err := <-done; err != nil {
		t.Errorf("first Execute = %v", err)
	}
}

func TestShutdownPowerOffFailureReleasesGuardNoDuplicateLog(t *testing.T) {
	rec := &callRecorder{}
	errPower := errors.New("systemctl failed")
	power := &fakePower{rec: rec, errs: []error{errPower, errPower}}
	fl := &fakeFlusher{rec: rec}
	fin := &fakeFinalizer{rec: rec}
	sd, l := newTestShutdown(t, false, power, fl, fin)

	ev := Event{Reason: ReasonLowBattery, Threshold: ReasonVolts, Volts: 3.2, HasBattery: true}
	for i := 0; i < 2; i++ {
		if err := sd.Execute(context.Background(), ev); !errors.Is(err, errPower) {
			t.Fatalf("Execute %d = %v, want %v", i, err, errPower)
		}
		if sd.Pending() {
			t.Fatalf("Pending = true after failure %d", i)
		}
	}
	if err := sd.Execute(context.Background(), ev); err != nil {
		t.Fatalf("third Execute = %v", err)
	}
	if power.count() != 3 {
		t.Errorf("power calls = %d, want 3", power.count())
	}
	want := []string{"finalize", "flush", "power", "finalize", "flush", "power", "finalize", "flush", "power"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	evs, err := l.List()
	if err != nil || len(evs) != 1 {
		t.Fatalf("log = %+v, %v; want exactly one entry", evs, err)
	}

	// A different request (manual button) after a failure is logged.
	power.errs = []error{errPower}
	sd2, l2 := newTestShutdown(t, false, power, nil, nil)
	_ = sd2.Execute(context.Background(), ev)
	if err := sd2.Execute(context.Background(), Event{Reason: ReasonManual, Client: "x"}); err != nil {
		t.Fatalf("manual Execute = %v", err)
	}
	if evs, _ := l2.List(); len(evs) != 2 {
		t.Errorf("log after manual retry has %d entries, want 2", len(evs))
	}
}

func TestShutdownIgnoresCancelledContext(t *testing.T) {
	rec := &callRecorder{}
	power := &fakePower{rec: rec}
	fl := &fakeFlusher{rec: rec}
	sd, _ := newTestShutdown(t, false, power, fl, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sd.Execute(ctx, Event{Reason: ReasonManual}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fl.ctxErr != nil || power.ctxErr != nil {
		t.Errorf("ctx errors: flush %v, power %v; want nil", fl.ctxErr, power.ctxErr)
	}
}

func TestShutdownNilPowerFails(t *testing.T) {
	sd, _ := newTestShutdown(t, false, nil, nil, nil)
	if err := sd.Execute(context.Background(), Event{Reason: ReasonManual}); err == nil {
		t.Fatal("Execute with nil power succeeded")
	}
	if sd.Pending() {
		t.Error("Pending = true after failure")
	}
}
