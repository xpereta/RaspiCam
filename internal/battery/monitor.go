package battery

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// State is the monitor state shown in the UI.
type State int

// State values.
const (
	StateDisabled State = iota
	StateNotDetected
	StateReadError
	StateStale
	StateOK
)

// String returns the UI name for s.
func (s State) String() string {
	switch s {
	case StateDisabled:
		return "Disabled"
	case StateNotDetected:
		return "Not detected"
	case StateReadError:
		return "Read error"
	case StateStale:
		return "Stale"
	case StateOK:
		return "OK"
	default:
		return "Unknown"
	}
}

const (
	// ReadErrorThreshold is how many consecutive failed polls switch the
	// state to StateReadError.
	ReadErrorThreshold = 3
	// StaleIntervals is how many poll intervals without a completed poll
	// make a running monitor report StateStale.
	StaleIntervals = 3
	// DefaultPollInterval is used when NewMonitor gets a non-positive
	// interval.
	DefaultPollInterval = 30 * time.Second
)

// Status is a snapshot of the monitor for the UI.
type Status struct {
	State     State
	Err       string    // reason for NotDetected / ReadError / Stale
	Reading   Reading   // last good reading
	ReadAt    time.Time // time of last good reading; zero before the first
	Trend     Trend
	Decision  Decision      // decision for the last good reading
	Remaining time.Duration // 0 when hidden
	ResetSeen bool          // STATUS RI bit was set at detection
	Note      string        // non-fatal remark, such as the uptime fallback
}

// StatusProvider is anything that can report a battery Status: a running
// Monitor or a StaticStatus when there is no gauge.
type StatusProvider interface {
	Status() Status
}

// StaticStatus is a StatusProvider for when no monitor runs (battery
// disabled or gauge not detected). No shutdown can trigger from it.
type StaticStatus struct {
	s Status
}

// NewDisabledStatus reports StateDisabled (BATTERY_ENABLED=false).
func NewDisabledStatus() StaticStatus {
	return StaticStatus{s: Status{State: StateDisabled}}
}

// NewNotDetectedStatus reports StateNotDetected with err (the bus open or
// detection error) as the reason.
func NewNotDetectedStatus(err error) StaticStatus {
	s := Status{State: StateNotDetected}
	if err != nil {
		s.Err = err.Error()
	}
	return StaticStatus{s: s}
}

// Status returns the fixed status.
func (s StaticStatus) Status() Status { return s.s }

// Sink receives every valid reading. Record is called synchronously on the
// monitor goroutine, must not block, must use its own lock and must not call
// back into the monitor.
type Sink interface {
	Record(r Reading, s Status)
}

// MonitorClock is injected so tests control time.
type MonitorClock struct {
	Now    func() time.Time              // time.Now in main
	Uptime func() (time.Duration, error) // system.Uptime in main
}

// resetReporter is implemented by gauges that know whether the chip reset
// (MAX17048.ResetSeen).
type resetReporter interface {
	ResetSeen() bool
}

// Monitor polls a gauge, evaluates the shutdown policy, publishes readings
// to sinks and triggers the shutdown coordinator. One goroutine runs Run;
// Status is safe to call from any goroutine.
type Monitor struct {
	gauge    Gauge
	policy   *Policy
	sd       *Shutdown
	interval time.Duration
	now      func() time.Time
	uptime   func() (time.Duration, error)
	sinks    []Sink
	created  time.Time

	// Owned by the Run goroutine.
	trend        TrendTracker
	errCount     int
	shutdownDone bool // Execute completed for the current latch episode
	uptimeNoted  bool
	tick         <-chan time.Time // test hook; nil uses a time.Ticker
	afterPoll    func()           // test hook; called after each poll
	logf         func(format string, args ...any)

	mu       sync.RWMutex
	status   Status
	running  bool
	lastPoll time.Time
}

// NewMonitor returns a monitor polling g every interval. p is the shutdown
// policy and sd the coordinator (nil disables shutdown). If g reports
// ResetSeen() bool (as *MAX17048 does), it is shown in Status.
func NewMonitor(g Gauge, p *Policy, sd *Shutdown, interval time.Duration,
	clock MonitorClock, sinks ...Sink) *Monitor {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	now := clock.Now
	if now == nil {
		now = time.Now
	}
	m := &Monitor{
		gauge:    g,
		policy:   p,
		sd:       sd,
		interval: interval,
		now:      now,
		uptime:   clock.Uptime,
		sinks:    sinks,
		created:  now(),
		logf:     logPrintf,
	}
	m.status.State = StateOK
	if rr, ok := g.(resetReporter); ok {
		m.status.ResetSeen = rr.ResetSeen()
	}
	return m
}

// Status returns a copy of the latest status. It never waits for the
// monitor goroutine to finish a poll: while Run is active and no poll has
// completed for StaleIntervals intervals, it reports StateStale.
func (m *Monitor) Status() Status {
	m.mu.RLock()
	s := m.status
	running := m.running
	lastPoll := m.lastPoll
	m.mu.RUnlock()

	if running {
		if age := m.now().Sub(lastPoll); age > StaleIntervals*m.interval {
			s.State = StateStale
			s.Err = fmt.Sprintf("no poll completed for %v", age.Round(time.Second))
		}
	}
	return s
}

// Run polls once immediately and then on every tick until ctx is done.
func (m *Monitor) Run(ctx context.Context) {
	tick := m.tick
	if tick == nil {
		t := time.NewTicker(m.interval)
		defer t.Stop()
		tick = t.C
	}

	m.mu.Lock()
	m.running = true
	m.lastPoll = m.now()
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	m.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			m.poll(ctx)
		}
	}
}

// poll performs one read and acts on it.
func (m *Monitor) poll(ctx context.Context) {
	if m.afterPoll != nil {
		defer m.afterPoll()
	}
	r, err := m.gauge.Read(ctx)
	if ctx.Err() != nil {
		return // stopping; do not count the aborted read
	}
	if err == nil {
		err = plausible(r)
	}
	uptime := m.systemUptime()

	if err != nil {
		m.pollFailed(ctx, err, uptime)
		return
	}
	m.pollOK(ctx, r, uptime)
}

func (m *Monitor) pollFailed(ctx context.Context, err error, uptime time.Duration) {
	m.errCount++
	// Log the start of a failure streak and the switch to StateReadError,
	// not every poll, to keep SD writes low while the gauge is unplugged.
	switch m.errCount {
	case 1:
		m.logf("battery: read failed: %v", err)
	case ReadErrorThreshold:
		m.logf("battery: %d consecutive reads failed, state %s: %v", m.errCount, StateReadError, err)
	}
	d := m.policy.Evaluate(Reading{}, err, m.trend.Trend(), uptime)

	m.mu.Lock()
	m.lastPoll = m.now()
	if m.errCount >= ReadErrorThreshold {
		m.status.State = StateReadError
		m.status.Err = err.Error()
	}
	last := m.status.Reading
	m.mu.Unlock()

	m.handleDecision(ctx, d, last)
}

func (m *Monitor) pollOK(ctx context.Context, r Reading, uptime time.Duration) {
	if m.errCount > 0 {
		m.logf("battery: reads recovered after %d failed", m.errCount)
	}
	m.errCount = 0
	trend := m.trend.UpdateAt(r.CRate, r.Time)
	d := m.policy.Evaluate(r, nil, trend, uptime)
	remaining, ok := RemainingTime(r, trend, m.trend.Since(), m.policy.cfg.ShutdownSOC)
	if !ok {
		remaining = 0
	}

	m.mu.Lock()
	m.lastPoll = m.now()
	m.status.State = StateOK
	m.status.Err = ""
	m.status.Reading = r
	m.status.ReadAt = r.Time
	m.status.Trend = trend
	m.status.Decision = d
	m.status.Remaining = remaining
	s := m.status
	m.mu.Unlock()

	for _, sink := range m.sinks {
		sink.Record(r, s)
	}
	m.handleDecision(ctx, d, r)
}

// handleDecision calls the shutdown coordinator while the decision is
// latched: once per latch episode on success or dry run, and again on the
// next poll after a power-off failure or while another shutdown (such as a
// manual one) is in progress, since that attempt may still fail. Rejected
// calls return before logging, so the retries are cheap and silent.
func (m *Monitor) handleDecision(ctx context.Context, d Decision, last Reading) {
	if !d.Shutdown {
		m.shutdownDone = false
		return
	}
	if m.sd == nil || m.shutdownDone {
		return
	}
	err := m.sd.Execute(ctx, Event{
		Reason:     ReasonLowBattery,
		Threshold:  d.Reason,
		Volts:      last.Volts,
		SOC:        last.SOC,
		HasBattery: true,
	})
	if err == nil {
		m.shutdownDone = true
	}
	// ErrShutdownInProgress or a failed power off: retry on the next poll.
}

// systemUptime returns the system uptime, falling back to the time since
// the monitor was created when it is unavailable (for example on macOS).
func (m *Monitor) systemUptime() time.Duration {
	if m.uptime != nil {
		if d, err := m.uptime(); err == nil {
			return d
		} else if !m.uptimeNoted {
			m.noteUptime("system uptime unavailable, grace period uses monitor run time: " + err.Error())
		}
	} else if !m.uptimeNoted {
		m.noteUptime("system uptime unavailable, grace period uses monitor run time")
	}
	return m.now().Sub(m.created)
}

func (m *Monitor) noteUptime(note string) {
	m.uptimeNoted = true
	m.mu.Lock()
	m.status.Note = note
	m.mu.Unlock()
}
