package battery

import (
	"testing"
	"time"
)

func TestClassifyCRate(t *testing.T) {
	cases := []struct {
		crate float64
		want  Trend
	}{
		{0, TrendIdle},
		{0.49, TrendIdle},
		{-0.49, TrendIdle},
		{0.5, TrendCharging},
		{12, TrendCharging},
		{-0.5, TrendDischarging},
		{-12, TrendDischarging},
	}
	for _, tc := range cases {
		if got := classifyCRate(tc.crate); got != tc.want {
			t.Errorf("classifyCRate(%v) = %v, want %v", tc.crate, got, tc.want)
		}
	}
}

func TestTrendTracker(t *testing.T) {
	cases := []struct {
		name    string
		samples []float64
		want    []Trend
	}{
		{"starts unknown, needs 2", []float64{-5, -5, -5},
			[]Trend{TrendUnknown, TrendDischarging, TrendDischarging}},
		{"idle from unknown", []float64{0.1, -0.2},
			[]Trend{TrendUnknown, TrendIdle}},
		{"single outlier ignored", []float64{-5, -5, 5, -5, -5},
			[]Trend{TrendUnknown, TrendDischarging, TrendDischarging, TrendDischarging, TrendDischarging}},
		{"2-sample switch", []float64{-5, -5, 5, 5},
			[]Trend{TrendUnknown, TrendDischarging, TrendDischarging, TrendCharging}},
		{"noise around dead band stays idle", []float64{0, 0, 0.6, 0.4, 0.6, 0.4, -0.6, 0.3},
			[]Trend{TrendUnknown, TrendIdle, TrendIdle, TrendIdle, TrendIdle, TrendIdle, TrendIdle, TrendIdle}},
		{"alternating new bands never switch", []float64{0, 0, 5, -5, 5, -5},
			[]Trend{TrendUnknown, TrendIdle, TrendIdle, TrendIdle, TrendIdle, TrendIdle}},
		{"candidate reset by current band", []float64{0, 0, 5, 0, 5, 5},
			[]Trend{TrendUnknown, TrendIdle, TrendIdle, TrendIdle, TrendIdle, TrendCharging}},
	}
	for _, tc := range cases {
		var tr TrendTracker
		for i, s := range tc.samples {
			if got := tr.Update(s); got != tc.want[i] {
				t.Errorf("%s: sample %d (%v): got %v want %v", tc.name, i, s, got, tc.want[i])
			}
		}
	}
}

func TestTrendTrackerSince(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tr TrendTracker
	tr.UpdateAt(-5, base)
	tr.UpdateAt(-5, base.Add(30*time.Second))
	tr.UpdateAt(-5, base.Add(60*time.Second))
	if !tr.Since().Equal(base.Add(30*time.Second)) || tr.Trend() != TrendDischarging {
		t.Fatalf("Since = %v, Trend = %v", tr.Since(), tr.Trend())
	}
}

func TestRemainingTime(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return since.Add(d) }
	cases := []struct {
		name   string
		r      Reading
		trend  Trend
		since  time.Time
		want   time.Duration
		wantOK bool
	}{
		{"normal", Reading{Time: at(10 * time.Minute), SOC: 55, CRate: -10}, TrendDischarging, since, 5 * time.Hour, true},
		{"fractional", Reading{Time: at(5 * time.Minute), SOC: 20, CRate: -6}, TrendDischarging, since, 150 * time.Minute, true},
		{"exactly 1%/h", Reading{Time: at(time.Hour), SOC: 6, CRate: -1}, TrendDischarging, since, time.Hour, true},
		{"below threshold clamps to 0", Reading{Time: at(time.Hour), SOC: 4, CRate: -2}, TrendDischarging, since, 0, true},
		{"rate too small", Reading{Time: at(time.Hour), SOC: 50, CRate: -0.9}, TrendDischarging, since, 0, false},
		{"not long enough", Reading{Time: at(4*time.Minute + 59*time.Second), SOC: 50, CRate: -10}, TrendDischarging, since, 0, false},
		{"charging", Reading{Time: at(time.Hour), SOC: 50, CRate: 10}, TrendCharging, since, 0, false},
		{"idle", Reading{Time: at(time.Hour), SOC: 50, CRate: -0.2}, TrendIdle, since, 0, false},
		{"unknown since", Reading{Time: at(time.Hour), SOC: 50, CRate: -10}, TrendDischarging, time.Time{}, 0, false},
	}
	for _, tc := range cases {
		got, ok := RemainingTime(tc.r, tc.trend, tc.since, 5)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}
