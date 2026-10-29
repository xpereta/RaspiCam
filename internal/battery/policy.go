package battery

import "time"

// Shutdown reasons reported in Decision.Reason.
const (
	ReasonSOC   = "soc"
	ReasonVolts = "volts"
)

// PolicyConfig holds the shutdown thresholds.
type PolicyConfig struct {
	LowSOC          float64       // 20
	ShutdownSOC     float64       // 5
	ShutdownVolts   float64       // 3.30
	RequiredSamples int           // 3
	GracePeriod     time.Duration // 2m
}

// Level is the battery warning level shown in the UI.
type Level int

// Level values.
const (
	LevelOK Level = iota
	LevelLow
	LevelCritical
)

// String returns the UI name for l.
func (l Level) String() string {
	switch l {
	case LevelLow:
		return "Low"
	case LevelCritical:
		return "Critical"
	default:
		return "OK"
	}
}

// Decision is the outcome of one policy evaluation.
type Decision struct {
	Level    Level
	Shutdown bool
	Reason   string // ReasonSOC or ReasonVolts when Shutdown
}

// Policy decides when to shut down. It is pure (no I/O, no clock) and not
// safe for concurrent use.
type Policy struct {
	cfg      PolicyConfig
	lowCount int
	latched  bool
	reason   string
}

// NewPolicy returns a policy using cfg. cfg is expected to be validated.
func NewPolicy(cfg PolicyConfig) *Policy {
	return &Policy{cfg: cfg}
}

// Evaluate applies one sample. uptime is the system uptime (from
// /proc/uptime), not the process run time. Rules, in order:
//  1. read error or implausible reading: reset the low count, keep the latch;
//  2. uptime below the grace period: reset the low count;
//  3. charging: reset the low count and clear the latch;
//  4. low condition (SOC or volts at or below threshold): count it, else reset;
//  5. RequiredSamples consecutive low readings: shut down and latch.
//
// While latched, every call returns Shutdown true with the latched reason.
func (p *Policy) Evaluate(r Reading, readErr error, trend Trend, uptime time.Duration) Decision {
	if readErr != nil || plausible(r) != nil {
		p.lowCount = 0
		return p.latchedDecision(LevelOK)
	}

	low, reason := p.lowCondition(r)
	level := p.level(r, low)

	if uptime < p.cfg.GracePeriod {
		p.lowCount = 0
		return p.latchedDecision(level)
	}

	if trend == TrendCharging {
		p.lowCount = 0
		p.latched = false
		p.reason = ""
		return Decision{Level: level}
	}

	if low {
		p.lowCount++
	} else {
		p.lowCount = 0
	}

	if !p.latched && p.lowCount >= p.cfg.RequiredSamples {
		p.latched = true
		p.reason = reason
	}
	return p.latchedDecision(level)
}

// latchedDecision returns a decision at level that carries the latch.
func (p *Policy) latchedDecision(level Level) Decision {
	if !p.latched {
		return Decision{Level: level}
	}
	return Decision{Level: level, Shutdown: true, Reason: p.reason}
}

// lowCondition reports whether r is at or below a shutdown threshold and
// which one. SOC takes precedence when both hold.
func (p *Policy) lowCondition(r Reading) (bool, string) {
	switch {
	case r.SOC <= p.cfg.ShutdownSOC:
		return true, ReasonSOC
	case r.Volts <= p.cfg.ShutdownVolts:
		return true, ReasonVolts
	default:
		return false, ""
	}
}

func (p *Policy) level(r Reading, low bool) Level {
	switch {
	case low:
		return LevelCritical
	case r.SOC <= p.cfg.LowSOC:
		return LevelLow
	default:
		return LevelOK
	}
}
