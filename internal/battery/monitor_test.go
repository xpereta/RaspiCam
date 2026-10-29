package battery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// gaugeResult is one scripted Read result.
type gaugeResult struct {
	r     Reading
	err   error
	block chan struct{} // when set, Read waits for it (or ctx) first
}

// fakeGauge returns scripted results, then repeats the last one.
type fakeGauge struct {
	mu      sync.Mutex
	results []gaugeResult
	reads   int
	reset   bool
	now     func() time.Time // stamps Reading.Time when set
}

func (g *fakeGauge) Read(ctx context.Context) (Reading, error) {
	g.mu.Lock()
	res := g.results[len(g.results)-1]
	if g.reads < len(g.results) {
		res = g.results[g.reads]
	}
	g.reads++
	g.mu.Unlock()
	if res.block != nil {
		select {
		case <-res.block:
		case <-ctx.Done():
			return Reading{}, ctx.Err()
		}
	}
	if g.now != nil && res.err == nil {
		res.r.Time = g.now()
	}
	return res.r, res.err
}

func (g *fakeGauge) ResetSeen() bool { return g.reset }

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type recordSink struct {
	mu       sync.Mutex
	readings []Reading
	statuses []Status
}

func (s *recordSink) Record(r Reading, st Status) {
	s.mu.Lock()
	s.readings = append(s.readings, r)
	s.statuses = append(s.statuses, st)
	s.mu.Unlock()
}

const testInterval = 30 * time.Second

var testPolicyCfg = PolicyConfig{
	LowSOC:          20,
	ShutdownSOC:     5,
	ShutdownVolts:   3.30,
	RequiredSamples: 3,
	GracePeriod:     2 * time.Minute,
}

// monitorHarness runs a Monitor with an injected tick channel and waits
// for each poll to finish.
type monitorHarness struct {
	t      *testing.T
	m      *Monitor
	clock  *fakeClock
	tick   chan time.Time
	polled chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

func good(soc, volts, crate float64) gaugeResult {
	return gaugeResult{r: Reading{SOC: soc, Volts: volts, CRate: crate}}
}

func bad(err error) gaugeResult { return gaugeResult{err: err} }

func newHarness(t *testing.T, g *fakeGauge, sd *Shutdown, uptime func() (time.Duration, error), sinks ...Sink) *monitorHarness {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	if uptime == nil {
		uptime = func() (time.Duration, error) { return time.Hour, nil }
	}
	g.now = clock.Now
	m := NewMonitor(g, NewPolicy(testPolicyCfg), sd, testInterval,
		MonitorClock{Now: clock.Now, Uptime: uptime}, sinks...)
	h := &monitorHarness{
		t:      t,
		m:      m,
		clock:  clock,
		tick:   make(chan time.Time),
		polled: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	m.tick = h.tick
	m.afterPoll = func() { h.polled <- struct{}{} }
	return h
}

// start runs the monitor and waits for the immediate first poll.
func (h *monitorHarness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		h.m.Run(ctx)
		close(h.done)
	}()
	h.t.Cleanup(h.stop)
	h.wait()
}

// step advances the clock by one interval, ticks and waits for the poll.
func (h *monitorHarness) step() {
	h.clock.Advance(testInterval)
	h.tick <- h.clock.Now()
	h.wait()
}

func (h *monitorHarness) wait() {
	select {
	case <-h.polled:
	case <-time.After(5 * time.Second):
		h.t.Fatal("poll did not finish")
	}
}

func (h *monitorHarness) stop() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	// Drain a poll that may finish during cancellation.
	for {
		select {
		case <-h.done:
			return
		case <-h.polled:
		case <-time.After(5 * time.Second):
			h.t.Error("Run did not exit after cancel")
			return
		}
	}
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{
		StateDisabled:    "Disabled",
		StateNotDetected: "Not detected",
		StateReadError:   "Read error",
		StateStale:       "Stale",
		StateOK:          "OK",
		State(99):        "Unknown",
	} {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestStaticStatus(t *testing.T) {
	var p StatusProvider = NewDisabledStatus()
	if s := p.Status(); s.State != StateDisabled || s.Err != "" {
		t.Errorf("disabled status = %+v", s)
	}
	p = NewNotDetectedStatus(errors.New("open /dev/i2c-1: no such file"))
	if s := p.Status(); s.State != StateNotDetected || s.Err != "open /dev/i2c-1: no such file" {
		t.Errorf("not detected status = %+v", s)
	}
	if s := NewNotDetectedStatus(nil).Status(); s.State != StateNotDetected {
		t.Errorf("not detected (nil err) status = %+v", s)
	}
	var _ StatusProvider = (*Monitor)(nil)
}

func TestMonitorReadErrorTransitions(t *testing.T) {
	errBus := errors.New("bus error")
	slow := fmt.Errorf("max17048: read took 600ms: %w", ErrSlowRead)
	g := &fakeGauge{reset: true, results: []gaugeResult{
		good(50, 3.8, -2),
		bad(errBus),
		bad(slow),
		good(1, 9.9, 0), // implausible voltage counts as an error
		good(49, 3.8, -2),
	}}
	h := newHarness(t, g, nil, nil)
	var logMu sync.Mutex
	var logs []string
	h.m.logf = func(format string, args ...any) {
		logMu.Lock()
		logs = append(logs, fmt.Sprintf(format, args...))
		logMu.Unlock()
	}
	h.start()

	s := h.m.Status()
	if s.State != StateOK || s.Reading.SOC != 50 || !s.ResetSeen {
		t.Fatalf("after good read: %+v", s)
	}

	h.step()
	h.step()
	if s := h.m.Status(); s.State != StateOK || s.Reading.SOC != 50 {
		t.Fatalf("after 2 errors: %+v, want OK with last reading", s)
	}
	h.step()
	s = h.m.Status()
	if s.State != StateReadError || !strings.Contains(s.Err, "implausible voltage") {
		t.Fatalf("after 3 errors: %+v, want ReadError", s)
	}
	if s.Reading.SOC != 50 {
		t.Errorf("last good reading lost: %+v", s.Reading)
	}

	h.step()
	if s := h.m.Status(); s.State != StateOK || s.Err != "" || s.Reading.SOC != 49 {
		t.Fatalf("after recovery: %+v", s)
	}

	// One line when the streak starts, one at ReadError, one on recovery;
	// the second failure is not logged.
	logMu.Lock()
	defer logMu.Unlock()
	want := []string{
		"battery: read failed: bus error",
		"battery: 3 consecutive reads failed, state Read error: implausible voltage",
		"battery: reads recovered after 3 failed",
	}
	if len(logs) != len(want) {
		t.Fatalf("logs = %q, want %d lines", logs, len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(logs[i], w) {
			t.Errorf("log %d = %q, want prefix %q", i, logs[i], w)
		}
	}
}

func TestMonitorSlowReadCountsAsError(t *testing.T) {
	slow := fmt.Errorf("max17048: read took 600ms: %w", ErrSlowRead)
	g := &fakeGauge{results: []gaugeResult{bad(slow)}}
	h := newHarness(t, g, nil, nil)
	h.start()
	h.step()
	h.step()
	s := h.m.Status()
	if s.State != StateReadError || !strings.Contains(s.Err, "slow read") {
		t.Fatalf("status = %+v, want ReadError with slow read", s)
	}
}

func TestMonitorStaleWhileGaugeBlocked(t *testing.T) {
	block := make(chan struct{})
	g := &fakeGauge{results: []gaugeResult{
		good(50, 3.8, -2),
		{r: Reading{SOC: 49, Volts: 3.8}, block: block},
	}}
	h := newHarness(t, g, nil, nil)
	h.start()
	defer close(block) // runs before the Cleanup stop, unblocking the read

	h.clock.Advance(testInterval)
	h.tick <- h.clock.Now() // the loop accepted the tick; Read now blocks

	h.clock.Advance(2 * testInterval)
	if s := h.m.Status(); s.State != StateOK {
		t.Fatalf("after 3 intervals: %+v, want OK", s)
	}
	h.clock.Advance(time.Second)
	s := h.m.Status()
	if s.State != StateStale || !strings.Contains(s.Err, "no poll completed") {
		t.Fatalf("status = %+v, want Stale", s)
	}
	if s.Reading.SOC != 50 {
		t.Errorf("stale status lost reading: %+v", s.Reading)
	}
}

func TestMonitorNotStaleAfterStop(t *testing.T) {
	g := &fakeGauge{results: []gaugeResult{good(50, 3.8, -2)}}
	h := newHarness(t, g, nil, nil)
	h.start()
	h.stop()
	h.clock.Advance(10 * testInterval)
	if s := h.m.Status(); s.State != StateOK {
		t.Fatalf("stopped monitor status = %+v, want OK", s)
	}
}

func lowShutdown(t *testing.T, dryRun bool, power *fakePower) (*Shutdown, *ShutdownLog) {
	t.Helper()
	l := NewShutdownLog(filepath.Join(t.TempDir(), "shutdowns.jsonl"))
	l.Logf = quietLogf
	var p PowerOff
	if power != nil {
		p = power
	}
	return NewShutdown(l, p, dryRun, nil, nil, WithShutdownLogf(quietLogf)), l
}

// A low-battery shutdown rejected because a manual one is running must be
// retried, in case the manual attempt fails and releases the guard.
func TestMonitorRetriesWhileOtherShutdownInProgress(t *testing.T) {
	rec := &callRecorder{}
	power := &fakePower{rec: rec}
	sd, _ := lowShutdown(t, false, power)
	sd.inProgress = true // a manual shutdown is running
	g := &fakeGauge{results: []gaugeResult{good(4, 3.6, -5)}}
	h := newHarness(t, g, sd, nil)
	h.start()
	h.step()
	h.step() // third low sample: latched, Execute rejected
	h.step() // still rejected
	if power.count() != 0 {
		t.Fatalf("power called while another shutdown was in progress")
	}

	// The manual attempt failed and released the guard.
	sd.mu.Lock()
	sd.inProgress = false
	sd.mu.Unlock()
	h.step()
	if power.count() != 1 {
		t.Fatalf("power calls = %d after guard released, want 1", power.count())
	}
}

func TestMonitorRetriesPowerOffFailure(t *testing.T) {
	rec := &callRecorder{}
	errPower := errors.New("systemctl failed")
	power := &fakePower{rec: rec, errs: []error{errPower}}
	sd, l := lowShutdown(t, false, power)
	g := &fakeGauge{results: []gaugeResult{good(4, 3.6, -5)}}
	h := newHarness(t, g, sd, nil)
	h.start()
	h.step()
	if power.count() != 0 {
		t.Fatalf("power called after 2 low samples")
	}
	h.step() // 3rd low sample: shutdown, power fails
	if power.count() != 1 {
		t.Fatalf("power calls = %d, want 1", power.count())
	}
	h.step() // retry succeeds
	if power.count() != 2 {
		t.Fatalf("power calls = %d, want 2 (retry)", power.count())
	}
	h.step()
	h.step()
	if power.count() != 2 {
		t.Fatalf("power calls = %d, want 2 (no retry after success)", power.count())
	}
	evs, err := l.List()
	if err != nil || len(evs) != 1 {
		t.Fatalf("log = %+v, %v; want one entry", evs, err)
	}
	ev := evs[0]
	if ev.Reason != ReasonLowBattery || ev.Threshold != ReasonSOC || ev.SOC != 4 || ev.Volts != 3.6 || !ev.HasBattery {
		t.Errorf("event = %+v", ev)
	}
}

func TestMonitorRetriesAcrossReadErrorsWhileLatched(t *testing.T) {
	rec := &callRecorder{}
	errPower := errors.New("systemctl failed")
	power := &fakePower{rec: rec, errs: []error{errPower}}
	sd, _ := lowShutdown(t, false, power)
	g := &fakeGauge{results: []gaugeResult{
		good(4, 3.6, -5), good(4, 3.6, -5), good(4, 3.6, -5),
		bad(errors.New("bus error")),
	}}
	h := newHarness(t, g, sd, nil)
	h.start()
	h.step()
	h.step()
	h.step() // read error, still latched: retry
	if power.count() != 2 {
		t.Fatalf("power calls = %d, want 2", power.count())
	}
}

func TestMonitorDryRunOncePerLatch(t *testing.T) {
	sd, l := lowShutdown(t, true, nil)
	low := good(4, 3.6, -5)
	charge := good(4, 3.6, 5)
	g := &fakeGauge{results: []gaugeResult{
		low, low, low, low, low, // latched at 3rd, once only
		charge, charge, // trend switches to charging on the 2nd: latch clears
		low, low, low, low, low, // discharging again after 1 sample; latch at 3rd low after that
	}}
	h := newHarness(t, g, sd, nil)
	h.start()
	for i := 1; i < len(g.results); i++ {
		h.step()
	}
	evs, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("dry-run events = %d, want 2 (one per latch episode)", len(evs))
	}
	for _, ev := range evs {
		if !ev.DryRun {
			t.Errorf("event not marked dry run: %+v", ev)
		}
	}
}

func TestMonitorSinksAndStatusFields(t *testing.T) {
	sink := &recordSink{}
	g := &fakeGauge{results: []gaugeResult{
		good(50, 3.8, -2),
		bad(errors.New("bus error")),
		good(48, 3.8, -2),
	}}
	h := newHarness(t, g, nil, nil, sink)
	h.start()
	h.step()
	h.step()
	// Feed a long discharge so remaining time becomes visible.
	for i := 0; i < 12; i++ {
		h.step()
	}

	sink.mu.Lock()
	n := len(sink.readings)
	last := sink.statuses[n-1]
	first := sink.statuses[0]
	sink.mu.Unlock()
	if n != 14 {
		t.Fatalf("sink calls = %d, want 14 (errors skipped)", n)
	}
	if first.Reading.SOC != 50 || first.State != StateOK {
		t.Errorf("first status = %+v", first)
	}
	if last.Trend != TrendDischarging || last.Remaining <= 0 {
		t.Errorf("last status = %+v, want discharging with remaining time", last)
	}
	want := time.Duration((48 - 5) / 2.0 * float64(time.Hour))
	if last.Remaining != want {
		t.Errorf("Remaining = %v, want %v", last.Remaining, want)
	}
	if got := h.m.Status(); got != last {
		t.Errorf("Status() = %+v, want sink status %+v", got, last)
	}
}

func TestMonitorUptimeFallback(t *testing.T) {
	sd, l := lowShutdown(t, true, nil)
	g := &fakeGauge{results: []gaugeResult{good(4, 3.6, -5)}}
	noUptime := func() (time.Duration, error) { return 0, errors.New("no /proc") }
	h := newHarness(t, g, sd, noUptime)
	h.start()
	if s := h.m.Status(); !strings.Contains(s.Note, "uptime unavailable") {
		t.Errorf("Note = %q", s.Note)
	}
	// Inside the 2 minute grace period (measured from monitor creation).
	h.step()
	h.step()
	h.step()
	if evs, _ := l.List(); len(evs) != 0 {
		t.Fatalf("shutdown during grace period: %+v", evs)
	}
	// After grace: 3 consecutive low readings trigger.
	h.step()
	h.step()
	if evs, _ := l.List(); len(evs) != 0 {
		t.Fatalf("shutdown after 2 post-grace samples: %+v", evs)
	}
	h.step()
	if evs, _ := l.List(); len(evs) != 1 {
		t.Fatalf("events after grace = %d, want 1", len(evs))
	}
}

func TestMonitorRunExitsOnCancel(t *testing.T) {
	g := &fakeGauge{results: []gaugeResult{good(50, 3.8, -2)}}
	m := NewMonitor(g, NewPolicy(testPolicyCfg), nil, testInterval, MonitorClock{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

func TestMonitorNonPositiveIntervalDefaults(t *testing.T) {
	m := NewMonitor(&fakeGauge{results: []gaugeResult{good(50, 3.8, 0)}}, NewPolicy(testPolicyCfg), nil, 0, MonitorClock{})
	if m.interval != DefaultPollInterval {
		t.Errorf("interval = %v, want %v", m.interval, DefaultPollInterval)
	}
}

func TestMonitorPollAllocations(t *testing.T) {
	g := &fakeGauge{results: []gaugeResult{good(50, 3.8, -2)}}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewMonitor(g, NewPolicy(testPolicyCfg), nil, testInterval,
		MonitorClock{Now: clock.Now, Uptime: func() (time.Duration, error) { return time.Hour, nil }},
		&nopSink{})
	ctx := context.Background()
	m.poll(ctx)
	allocs := testing.AllocsPerRun(100, func() { m.poll(ctx) })
	if allocs != 0 {
		t.Errorf("allocations per poll = %v, want 0", allocs)
	}
}

type nopSink struct{}

func (nopSink) Record(Reading, Status) {}
