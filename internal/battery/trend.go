package battery

import (
	"math"
	"time"
)

// Trend is the charge direction derived from CRATE.
type Trend int

// Trend values.
const (
	TrendUnknown Trend = iota
	TrendIdle
	TrendCharging
	TrendDischarging
)

// String returns a lowercase name for t.
func (t Trend) String() string {
	switch t {
	case TrendIdle:
		return "idle"
	case TrendCharging:
		return "charging"
	case TrendDischarging:
		return "discharging"
	default:
		return "unknown"
	}
}

const (
	// TrendDeadBand is the |CRATE| (%/h) below which the battery is idle.
	TrendDeadBand = 0.5
	// TrendSwitchSamples is how many consecutive samples in a new band are
	// needed to change state.
	TrendSwitchSamples = 2

	// MinRemainingCRate is the minimum |CRATE| (%/h) for showing remaining time.
	MinRemainingCRate = 1.0
	// MinDischargingForRemaining is how long the trend must have been
	// discharging before remaining time is shown.
	MinDischargingForRemaining = 5 * time.Minute
)

// classifyCRate maps one CRATE sample to its band.
func classifyCRate(crate float64) Trend {
	switch {
	case crate >= TrendDeadBand:
		return TrendCharging
	case crate <= -TrendDeadBand:
		return TrendDischarging
	default:
		return TrendIdle
	}
}

// TrendTracker classifies CRATE with a dead band and requires
// TrendSwitchSamples consecutive samples in a new band to change state.
// The zero value is ready to use and starts as TrendUnknown.
type TrendTracker struct {
	state     Trend
	since     time.Time
	candidate Trend
	count     int
}

// Update feeds one CRATE sample and returns the current trend.
func (t *TrendTracker) Update(crate float64) Trend {
	return t.UpdateAt(crate, time.Time{})
}

// UpdateAt is Update with the sample time, which is recorded as Since when
// the state changes.
func (t *TrendTracker) UpdateAt(crate float64, at time.Time) Trend {
	band := classifyCRate(crate)
	if band == t.state {
		t.count = 0
		return t.state
	}
	if band != t.candidate {
		t.candidate = band
		t.count = 0
	}
	t.count++
	if t.count >= TrendSwitchSamples {
		t.state = band
		t.since = at
		t.count = 0
	}
	return t.state
}

// Trend returns the current trend.
func (t *TrendTracker) Trend() Trend { return t.state }

// Since returns the time passed to UpdateAt when the current state began
// (zero if unknown or set through Update).
func (t *TrendTracker) Since() time.Time { return t.since }

// RemainingTime estimates the time until the shutdown threshold:
// (SOC - shutdownSOC) / |CRATE| hours. ok is false (hide the estimate)
// unless trend is discharging, |CRATE| >= MinRemainingCRate and the trend
// has been discharging for at least MinDischargingForRemaining (from since
// to r.Time).
func RemainingTime(r Reading, trend Trend, since time.Time, shutdownSOC float64) (time.Duration, bool) {
	if trend != TrendDischarging || since.IsZero() {
		return 0, false
	}
	rate := math.Abs(r.CRate)
	if rate < MinRemainingCRate {
		return 0, false
	}
	if r.Time.Sub(since) < MinDischargingForRemaining {
		return 0, false
	}
	left := r.SOC - shutdownSOC
	if left <= 0 {
		return 0, true
	}
	return time.Duration(left / rate * float64(time.Hour)), true
}
