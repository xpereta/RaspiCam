package battery

import (
	"errors"
	"testing"
	"time"
)

var defaultPolicy = PolicyConfig{
	LowSOC:          20,
	ShutdownSOC:     5,
	ShutdownVolts:   3.30,
	RequiredSamples: 3,
	GracePeriod:     2 * time.Minute,
}

type step struct {
	r      Reading
	err    error
	trend  Trend
	uptime time.Duration
	want   Decision
}

var (
	errRead = errors.New("read failed")

	okR       = Reading{Volts: 3.9, SOC: 70}
	lowWarnR  = Reading{Volts: 3.6, SOC: 15}
	lowSOCR   = Reading{Volts: 3.5, SOC: 4}
	lowVoltsR = Reading{Volts: 3.25, SOC: 30}
	bothLowR  = Reading{Volts: 3.2, SOC: 3}
	badVoltsR = Reading{Volts: 1.0, SOC: 3}
	badSOCR   = Reading{Volts: 3.2, SOC: 200}

	up = 10 * time.Minute

	dOK       = Decision{Level: LevelOK}
	dLow      = Decision{Level: LevelLow}
	dCrit     = Decision{Level: LevelCritical}
	dShutSOC  = Decision{Level: LevelCritical, Shutdown: true, Reason: ReasonSOC}
	dShutVolt = Decision{Level: LevelCritical, Shutdown: true, Reason: ReasonVolts}
)

func dis(r Reading, want Decision) step {
	return step{r: r, trend: TrendDischarging, uptime: up, want: want}
}

func fail(want Decision) step {
	return step{err: errRead, trend: TrendDischarging, uptime: up, want: want}
}

func TestPolicyEvaluate(t *testing.T) {
	cases := []struct {
		name  string
		steps []step
	}{
		{"ok and low levels", []step{dis(okR, dOK), dis(lowWarnR, dLow)}},
		{"low threshold inclusive", []step{dis(Reading{Volts: 3.7, SOC: 20}, dLow)}},
		{"three consecutive SOC lows trigger", []step{
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(lowSOCR, dShutSOC)}},
		{"SOC threshold inclusive", []step{
			dis(Reading{Volts: 3.5, SOC: 5}, dCrit), dis(Reading{Volts: 3.5, SOC: 5}, dCrit),
			dis(Reading{Volts: 3.5, SOC: 5}, dShutSOC)}},
		{"volts-only trigger", []step{
			dis(lowVoltsR, dCrit), dis(lowVoltsR, dCrit), dis(lowVoltsR, dShutVolt)}},
		{"volts threshold inclusive", []step{
			dis(Reading{Volts: 3.30, SOC: 30}, dCrit), dis(Reading{Volts: 3.30, SOC: 30}, dCrit),
			dis(Reading{Volts: 3.30, SOC: 30}, dShutVolt)}},
		{"both low reports soc", []step{
			dis(bothLowR, dCrit), dis(bothLowR, dCrit), dis(bothLowR, dShutSOC)}},
		{"mixed soc and volts lows count together", []step{
			dis(lowVoltsR, dCrit), dis(lowSOCR, dCrit), dis(lowVoltsR, dShutVolt)}},
		{"recovery resets count", []step{
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(okR, dOK),
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(lowSOCR, dShutSOC)}},
		{"low, error, low, error, low does not trigger", []step{
			dis(lowSOCR, dCrit), fail(dOK), dis(lowSOCR, dCrit), fail(dOK), dis(lowSOCR, dCrit)}},
		{"implausible readings reset count", []step{
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(badVoltsR, dOK),
			dis(lowSOCR, dCrit), dis(badSOCR, dOK), dis(lowSOCR, dCrit)}},
		{"latch kept across errors and recovery", []step{
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(lowSOCR, dShutSOC),
			fail(Decision{Level: LevelOK, Shutdown: true, Reason: ReasonSOC}),
			dis(badVoltsR, Decision{Level: LevelOK, Shutdown: true, Reason: ReasonSOC}),
			dis(okR, Decision{Level: LevelOK, Shutdown: true, Reason: ReasonSOC}),
			dis(lowVoltsR, Decision{Level: LevelCritical, Shutdown: true, Reason: ReasonSOC})}},
		{"charging suppresses and clears latch", []step{
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit),
			{r: lowSOCR, trend: TrendCharging, uptime: up, want: dCrit},
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(lowSOCR, dShutSOC),
			{r: lowSOCR, trend: TrendCharging, uptime: up, want: dCrit},
			{r: okR, trend: TrendCharging, uptime: up, want: dOK},
			dis(lowSOCR, dCrit), dis(lowSOCR, dCrit), dis(lowSOCR, dShutSOC)}},
		{"idle and unknown trend still trigger", []step{
			{r: lowSOCR, trend: TrendIdle, uptime: up, want: dCrit},
			{r: lowSOCR, trend: TrendUnknown, uptime: up, want: dCrit},
			{r: lowSOCR, trend: TrendIdle, uptime: up, want: dShutSOC}}},
		{"grace period by uptime", []step{
			{r: lowSOCR, trend: TrendDischarging, uptime: 0, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: time.Minute, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 2*time.Minute - time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 2 * time.Minute, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 2*time.Minute + 30*time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 3 * time.Minute, want: dShutSOC}}},
		{"grace low readings do not count", []step{
			{r: lowSOCR, trend: TrendDischarging, uptime: 60 * time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 90 * time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 119 * time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 150 * time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 180 * time.Second, want: dCrit},
			{r: lowSOCR, trend: TrendDischarging, uptime: 210 * time.Second, want: dShutSOC}}},
	}
	for _, tc := range cases {
		p := NewPolicy(defaultPolicy)
		for i, s := range tc.steps {
			got := p.Evaluate(s.r, s.err, s.trend, s.uptime)
			if got != s.want {
				t.Errorf("%s: step %d: got %+v want %+v", tc.name, i, got, s.want)
			}
		}
	}
}

// A UI restart after the grace period gets a fresh Policy but no new grace,
// since grace is measured from system boot.
func TestPolicyRestartAfterGrace(t *testing.T) {
	p := NewPolicy(defaultPolicy)
	uptime := 30 * time.Minute
	want := []Decision{dCrit, dCrit, dShutSOC}
	for i, w := range want {
		got := p.Evaluate(lowSOCR, nil, TrendDischarging, uptime+time.Duration(i)*30*time.Second)
		if got != w {
			t.Errorf("step %d: got %+v want %+v", i, got, w)
		}
	}
}

func TestLevelString(t *testing.T) {
	for l, want := range map[Level]string{LevelOK: "OK", LevelLow: "Low", LevelCritical: "Critical"} {
		if got := l.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", l, got, want)
		}
	}
}
