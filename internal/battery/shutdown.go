package battery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Reason is why a shutdown was requested.
type Reason string

// Shutdown reasons.
const (
	ReasonLowBattery Reason = "low_battery"
	ReasonManual     Reason = "manual"
)

// Timeouts used by Shutdown.Execute.
const (
	// ShutdownTimeout bounds a whole Execute call.
	ShutdownTimeout = 15 * time.Second
	// FlushTimeout bounds all finalizers and flushers together.
	FlushTimeout = 5 * time.Second
)

// ErrShutdownInProgress is returned by Execute while another shutdown is
// being executed or has already been handed to the OS.
var ErrShutdownInProgress = errors.New("shutdown in progress")

// Event is one shutdown request, as stored in the shutdown log.
type Event struct {
	Time       time.Time `json:"time"`
	Reason     Reason    `json:"reason"`
	Threshold  string    `json:"threshold"` // ReasonSOC or ReasonVolts for low_battery, "" for manual
	Volts      float64   `json:"volts"`     // 0 when unknown (gauge absent)
	SOC        float64   `json:"soc"`
	HasBattery bool      `json:"has_battery"`
	Synced     bool      `json:"synced"`
	Client     string    `json:"client"` // remote address for manual
	DryRun     bool      `json:"dry_run"`
}

// PowerOff powers the system off.
type PowerOff interface {
	PowerOff(ctx context.Context) error
}

// Flusher writes buffered data to disk. It never changes session state, so
// it is safe in dry run and on every exit.
type Flusher interface {
	Flush(ctx context.Context) error
}

// ShutdownFinalizer closes open state (the phase 2 history session) because
// the Pi is really about to power off. Never called in dry run. It must be
// idempotent: it runs again on a retry after a failed power off.
type ShutdownFinalizer interface {
	FinalizeShutdown(ctx context.Context, ev Event) error
}

// ShutdownOption configures a Shutdown.
type ShutdownOption func(*Shutdown)

// WithShutdownLogf sets the logger used for journal messages (default
// log.Printf). A nil f discards them.
func WithShutdownLogf(f func(format string, args ...any)) ShutdownOption {
	return func(s *Shutdown) { s.logf = f }
}

// WithTimeSynced sets the function that fills Event.Synced (for example
// system.TimeSynced). Without it, Event.Synced is kept as passed in.
func WithTimeSynced(f func() bool) ShutdownOption {
	return func(s *Shutdown) { s.synced = f }
}

// WithShutdownClock sets the function that fills a zero Event.Time
// (default time.Now).
func WithShutdownClock(now func() time.Time) ShutdownOption {
	return func(s *Shutdown) { s.now = now }
}

// Shutdown coordinates both shutdown triggers (low battery and the UI
// button): journal message, shutdown log, finalize, flush and power off.
// It is safe for concurrent use.
type Shutdown struct {
	log        *ShutdownLog
	power      PowerOff
	dryRun     bool
	flushers   []Flusher
	finalizers []ShutdownFinalizer

	logf   func(format string, args ...any)
	synced func() bool
	now    func() time.Time

	mu         sync.Mutex
	inProgress bool
	// lastAppended is the last event written to the shutdown log while the
	// following power off failed; a retry of the same request is not
	// appended again.
	lastAppended *Event
}

// NewShutdown returns a coordinator. log may be nil (nothing is recorded);
// power may be nil only in dry run.
func NewShutdown(log *ShutdownLog, power PowerOff, dryRun bool,
	flushers []Flusher, finalizers []ShutdownFinalizer, opts ...ShutdownOption) *Shutdown {
	s := &Shutdown{
		log:        log,
		power:      power,
		dryRun:     dryRun,
		flushers:   flushers,
		finalizers: finalizers,
		logf:       logPrintf,
		now:        time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func logPrintf(format string, args ...any) { log.Printf(format, args...) }

// DryRun reports whether the coordinator only logs and flushes.
func (s *Shutdown) DryRun() bool { return s.dryRun }

// Pending reports whether a shutdown is currently executing or has been
// handed to the OS. It is false again after a failed power off.
func (s *Shutdown) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inProgress
}

// Execute runs the shutdown sequence for ev:
//  1. reject with ErrShutdownInProgress while another call is running or a
//     real shutdown was handed to the OS;
//  2. log a journal message;
//  3. append ev to the shutdown log (not again on a retry of the same
//     request after a failed power off);
//  4. dry run: run the flushers and return nil;
//  5. real: run the finalizers, then the flushers (5s total budget, errors
//     logged, not fatal);
//  6. real: power off. On failure the guard is released and the error is
//     returned so the caller can retry.
//
// Execute uses its own 15s deadline and deliberately ignores the
// cancellation of ctx (only its values are kept): a shutdown must not be cut
// short because an HTTP request finished or the monitor is stopping.
//
// A failure to append to the shutdown log is logged and does not stop the
// shutdown: powering off safely matters more than the record.
func (s *Shutdown) Execute(ctx context.Context, ev Event) error {
	ev = s.prepare(ev)

	s.mu.Lock()
	if s.inProgress {
		s.mu.Unlock()
		return ErrShutdownInProgress
	}
	s.inProgress = true
	retry := s.lastAppended != nil && sameRequest(*s.lastAppended, ev)
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()

	s.printf("battery: shutdown requested: reason=%s threshold=%q volts=%.3f soc=%.1f has_battery=%t client=%q dry_run=%t retry=%t",
		ev.Reason, ev.Threshold, ev.Volts, ev.SOC, ev.HasBattery, ev.Client, ev.DryRun, retry)

	appended := false
	if s.log != nil && !retry {
		if err := s.log.Append(ev); err != nil {
			s.printf("battery: shutdown log: %v", err)
		} else {
			appended = true
		}
	}

	if s.dryRun {
		s.runFlush(ctx, ev, false)
		s.printf("battery: dry run, not powering off")
		s.mu.Lock()
		s.inProgress = false
		s.mu.Unlock()
		return nil
	}

	s.runFlush(ctx, ev, true)

	var err error
	if s.power == nil {
		err = errors.New("no power off implementation")
	} else {
		err = s.power.PowerOff(ctx)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.inProgress = false
		if appended {
			e := ev
			s.lastAppended = &e
		}
		s.printf("battery: power off failed: %v", err)
		return fmt.Errorf("power off: %w", err)
	}
	s.lastAppended = nil
	return nil
}

// prepare fills the fields Shutdown owns.
func (s *Shutdown) prepare(ev Event) Event {
	if ev.Time.IsZero() {
		ev.Time = s.now()
	}
	if s.synced != nil {
		ev.Synced = s.synced()
	}
	ev.DryRun = s.dryRun
	return ev
}

// sameRequest reports whether b retries the request a (same trigger).
func sameRequest(a, b Event) bool {
	return a.Reason == b.Reason && a.Threshold == b.Threshold && a.Client == b.Client
}

// runFlush runs the finalizers (when finalize is true) and then the
// flushers within FlushTimeout. Errors are logged only.
func (s *Shutdown) runFlush(ctx context.Context, ev Event, finalize bool) {
	ctx, cancel := context.WithTimeout(ctx, FlushTimeout)
	defer cancel()
	if finalize {
		for _, f := range s.finalizers {
			if err := f.FinalizeShutdown(ctx, ev); err != nil {
				s.printf("battery: finalize before shutdown: %v", err)
			}
		}
	}
	for _, f := range s.flushers {
		if err := f.Flush(ctx); err != nil {
			s.printf("battery: flush before shutdown: %v", err)
		}
	}
}

func (s *Shutdown) printf(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}
