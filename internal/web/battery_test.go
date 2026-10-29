package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xpereta/RaspiCam/internal/battery"
)

var testNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.Local)

type fakeStatus struct{ s battery.Status }

func (f fakeStatus) Status() battery.Status { return f.s }

type fakeShutdown struct {
	mu      sync.Mutex
	pending bool
	err     error
	events  []battery.Event
	called  chan battery.Event
}

func newFakeShutdown() *fakeShutdown {
	return &fakeShutdown{called: make(chan battery.Event, 4)}
}

func (f *fakeShutdown) Execute(ctx context.Context, ev battery.Event) error {
	f.mu.Lock()
	f.events = append(f.events, ev)
	err := f.err
	f.mu.Unlock()
	f.called <- ev
	return err
}

func (f *fakeShutdown) Pending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending
}

func (f *fakeShutdown) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

type fakeLister struct {
	events []battery.Event
	err    error
}

func (f fakeLister) List() ([]battery.Event, error) { return f.events, f.err }

func okStatus() battery.Status {
	return battery.Status{
		State:     battery.StateOK,
		Reading:   battery.Reading{Volts: 3.874, SOC: 64.4, CRate: -10},
		ReadAt:    testNow.Add(-30 * time.Second),
		Trend:     battery.TrendDischarging,
		Decision:  battery.Decision{Level: battery.LevelOK},
		Remaining: 3*time.Hour + 20*time.Minute,
	}
}

func newTestServer(t *testing.T, deps Deps) *Server {
	t.Helper()
	if deps.ConfigPath == "" {
		deps.ConfigPath = filepath.Join(t.TempDir(), "mediamtx.yml")
	}
	if deps.ShutdownSOC == 0 {
		deps.ShutdownSOC = 5
	}
	if deps.ShutdownVolts == 0 {
		deps.ShutdownVolts = 3.3
	}
	deps.now = func() time.Time { return testNow }
	s, err := NewServer(deps)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

// renderCard renders only the battery card, avoiding the slow system
// collectors of the full status page.
func renderCard(t *testing.T, s *Server, power string) (string, []string) {
	t.Helper()
	view, warnings := s.buildBatteryView(power)
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "battery_card", view); err != nil {
		t.Fatalf("render battery_card: %v", err)
	}
	return buf.String(), warnings
}

func assertContains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\n%s", w, body)
		}
	}
}

func assertNotContains(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(body, w) {
			t.Errorf("body unexpectedly contains %q", w)
		}
	}
}

func TestBatteryCardStates(t *testing.T) {
	low := okStatus()
	low.Reading.SOC = 15
	low.Decision.Level = battery.LevelLow
	low.Remaining = 0

	critical := okStatus()
	critical.Reading = battery.Reading{Volts: 3.28, SOC: 4, CRate: -8}
	critical.Decision.Level = battery.LevelCritical
	critical.Remaining = 0

	tests := []struct {
		name    string
		status  battery.Status
		want    []string
		notWant []string
	}{
		{
			name:   "ok",
			status: okStatus(),
			want: []string{"3.87 V", "64%", `style="width: 64%;"`, "discharging", "~3h 20m",
				`<span class="badge ok">OK</span>`, "2026-10-29 11:59:30 (30s ago)",
				"Shutdown at 5% or 3.30 V"},
			notWant: []string{"notice", "(dry run)"},
		},
		{
			name:    "low",
			status:  low,
			want:    []string{"15%", `<span class="badge warn">Low</span>`, "bar-fill warn"},
			notWant: []string{"Remaining"},
		},
		{
			name:   "critical",
			status: critical,
			want:   []string{"3.28 V", "4%", `<span class="badge err">Critical</span>`, "bar-fill err"},
		},
		{
			name:    "read error",
			status:  battery.Status{State: battery.StateReadError, Err: "i2c timeout", ReadAt: testNow.Add(-time.Minute), Reading: battery.Reading{Volts: 3.9, SOC: 70}},
			want:    []string{`<div class="notice err">Read error: i2c timeout</div>`, "Inactive: read error"},
			notWant: []string{"3.90 V", "Last reading"},
		},
		{
			name:    "stale",
			status:  battery.Status{State: battery.StateStale, Err: "no poll completed for 2m0s", ReadAt: testNow.Add(-150 * time.Second), Reading: battery.Reading{Volts: 3.9, SOC: 70}},
			want:    []string{"Stale: last reading 2m 30s ago", "Inactive: readings stale"},
			notWant: []string{"3.90 V"},
		},
		{
			name:    "disabled",
			status:  battery.NewDisabledStatus().Status(),
			want:    []string{`<div class="notice warn">Disabled</div>`, "Inactive: battery monitoring disabled"},
			notWant: []string{"Shutdown at"},
		},
		{
			name:    "not detected",
			status:  battery.NewNotDetectedStatus(errors.New("open i2c bus /dev/i2c-1: no such file")).Status(),
			want:    []string{"Not detected: open i2c bus /dev/i2c-1: no such file", "Inactive: gauge not detected"},
			notWant: []string{"Shutdown at"},
		},
		{
			name:    "waiting",
			status:  battery.Status{State: battery.StateOK},
			want:    []string{"Waiting for first reading", "Shutdown at 5% or 3.30 V"},
			notWant: []string{"Last reading"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, Deps{Battery: fakeStatus{tt.status}, Shutdown: newFakeShutdown(), CanPowerOff: true})
			body, _ := renderCard(t, s, "")
			assertContains(t, body, tt.want...)
			assertNotContains(t, body, tt.notWant...)
		})
	}
}

func TestBatteryCardDryRun(t *testing.T) {
	s := newTestServer(t, Deps{Battery: fakeStatus{okStatus()}, Shutdown: newFakeShutdown(), DryRun: true})
	body, _ := renderCard(t, s, "")
	assertContains(t, body, "Shutdown at 5% or 3.30 V (dry run)", "Power off (dry run)", "Dry run: the request is logged only.")
	assertNotContains(t, body, "disabled>")
}

func TestBatteryCardPowerOffForm(t *testing.T) {
	t.Run("permitted", func(t *testing.T) {
		s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), CanPowerOff: true})
		body, _ := renderCard(t, s, "")
		assertContains(t, body, `action="/power-off"`, `name="confirm" value="yes"`, "I understand the Pi will power off")
		assertNotContains(t, body, "disabled>", "<script")
	})
	t.Run("not permitted", func(t *testing.T) {
		s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), PowerOffNote: "logind says no"})
		body, _ := renderCard(t, s, "")
		assertContains(t, body, `type="submit" disabled>Power off`, "logind says no")
	})
	t.Run("nil battery defaults to disabled", func(t *testing.T) {
		s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), CanPowerOff: true})
		body, _ := renderCard(t, s, "")
		assertContains(t, body, "Disabled", "Power off")
	})
}

func TestBatteryCardLastShutdown(t *testing.T) {
	events := []battery.Event{
		{Time: testNow.Add(-time.Hour), Reason: battery.ReasonLowBattery, Threshold: battery.ReasonVolts, Volts: 3.284, SOC: 6.2, HasBattery: true, Synced: true},
		{Time: testNow.Add(-2 * time.Hour), Reason: battery.ReasonManual, Client: "10.0.0.2:5000", DryRun: true},
	}
	s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), ShutdownLog: fakeLister{events: events}})
	body, _ := renderCard(t, s, "")
	assertContains(t, body,
		"2026-10-29 11:00:00, low battery (voltage), 3.28 V, 6%",
		"<details",
		"Shutdown log (2)",
		"2026-10-29 10:00:00 (clock not synced), manual, no battery reading",
		`<span class="badge warn">dry run</span>`,
	)
}

func TestBatteryCardShutdownLogError(t *testing.T) {
	s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), ShutdownLog: fakeLister{err: errors.New("permission denied")}})
	body, warnings := renderCard(t, s, "")
	assertContains(t, body, "none recorded")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Shutdown log unavailable: permission denied") {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestBatteryCardPowerMessage(t *testing.T) {
	s := newTestServer(t, Deps{Shutdown: newFakeShutdown(), DryRun: true})
	body, _ := renderCard(t, s, "dry-run")
	assertContains(t, body, `<div class="notice ok">Dry run: the power-off request was logged only.</div>`)
}

func TestStatusPageIncludesBatteryCard(t *testing.T) {
	mtx := httptest.NewServer(http.NotFoundHandler())
	mtx.Close() // unreachable API: collectors degrade quickly
	s := newTestServer(t, Deps{
		MediaMTXURL:  mtx.URL,
		MediaMTXPath: "cam",
		Battery:      fakeStatus{okStatus()},
		Shutdown:     newFakeShutdown(),
		CanPowerOff:  true,
		Warnings:     []string{"BATTERY_POLL_INTERVAL=1s is out of range"},
	})
	req := httptest.NewRequest(http.MethodGet, "/?power=confirm-required", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, `<div class="section-title">Battery</div>`, "3.87 V",
		"Tick the confirmation box to power off.", "BATTERY_POLL_INTERVAL=1s is out of range")
	if strings.Index(body, "Battery</div>") > strings.Index(body, "Camera configuration") {
		t.Errorf("battery card should come before the camera card")
	}
}

func postPowerOff(s *Server, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/power-off", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.10:41000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

var confirmed = url.Values{"confirm": {"yes"}}

func TestPowerOffMethodNotAllowed(t *testing.T) {
	sd := newFakeShutdown()
	s := newTestServer(t, Deps{Shutdown: sd, CanPowerOff: true})
	req := httptest.NewRequest(http.MethodGet, "/power-off?confirm=yes", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if sd.count() != 0 {
		t.Fatalf("Execute called on GET")
	}
}

func TestPowerOffRedirects(t *testing.T) {
	tests := []struct {
		name string
		deps Deps
		form url.Values
		want string
	}{
		{"missing confirm", Deps{CanPowerOff: true}, url.Values{}, "/?power=confirm-required"},
		{"wrong confirm", Deps{CanPowerOff: true}, url.Values{"confirm": {"on"}}, "/?power=confirm-required"},
		{"not permitted", Deps{CanPowerOff: false}, confirmed, "/?power=not-permitted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd := newFakeShutdown()
			tt.deps.Shutdown = sd
			s := newTestServer(t, tt.deps)
			rec := postPowerOff(s, tt.form, nil)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != tt.want {
				t.Fatalf("Location = %q, want %q", loc, tt.want)
			}
			if sd.count() != 0 {
				t.Fatalf("Execute called")
			}
		})
	}
}

func TestPowerOffDryRun(t *testing.T) {
	sd := newFakeShutdown()
	s := newTestServer(t, Deps{Battery: fakeStatus{okStatus()}, Shutdown: sd, DryRun: true})
	rec := postPowerOff(s, confirmed, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?power=dry-run" {
		t.Fatalf("got %d %q, want 303 /?power=dry-run", rec.Code, rec.Header().Get("Location"))
	}
	// Execute runs synchronously in dry run, so it is done already.
	if sd.count() != 1 {
		t.Fatalf("Execute calls = %d, want 1", sd.count())
	}
	ev := <-sd.called
	want := battery.Event{Reason: battery.ReasonManual, Client: "192.0.2.10:41000", HasBattery: true, Volts: 3.874, SOC: 64.4}
	if ev != want {
		t.Fatalf("event = %+v, want %+v", ev, want)
	}
}

func TestPowerOffDryRunWithoutReading(t *testing.T) {
	sd := newFakeShutdown()
	s := newTestServer(t, Deps{Battery: fakeStatus{battery.Status{State: battery.StateOK}}, Shutdown: sd, DryRun: true})
	postPowerOff(s, confirmed, nil)
	ev := <-sd.called
	if ev.HasBattery || ev.Volts != 0 || ev.SOC != 0 {
		t.Fatalf("event = %+v, want no battery values", ev)
	}
}

func TestPowerOffDryRunError(t *testing.T) {
	sd := newFakeShutdown()
	sd.err = errors.New("boom")
	s := newTestServer(t, Deps{Shutdown: sd, DryRun: true})
	rec := postPowerOff(s, confirmed, nil)
	if loc := rec.Header().Get("Location"); loc != "/?power=error" {
		t.Fatalf("Location = %q, want /?power=error", loc)
	}
}

func TestPowerOffReal(t *testing.T) {
	sd := newFakeShutdown()
	release := make(chan struct{})
	deps := Deps{Battery: battery.NewNotDetectedStatus(errors.New("no bus")), Shutdown: sd, CanPowerOff: true}
	deps.powerOffDelay = func() { <-release }
	s := newTestServer(t, deps)

	rec := postPowerOff(s, confirmed, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Shutting down. Wait for the green activity LED to stop blinking before cutting power.")
	if !rec.Flushed {
		t.Errorf("response not flushed")
	}
	if sd.count() != 0 {
		t.Fatalf("Execute called before the delay")
	}

	close(release)
	select {
	case ev := <-sd.called:
		want := battery.Event{Reason: battery.ReasonManual, Client: "192.0.2.10:41000"}
		if ev != want {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Execute not called")
	}
}

func TestPowerOffPending(t *testing.T) {
	sd := newFakeShutdown()
	sd.pending = true
	deps := Deps{Shutdown: sd, CanPowerOff: true}
	deps.powerOffDelay = func() { t.Error("delay hook called while pending") }
	s := newTestServer(t, deps)

	rec := postPowerOff(s, confirmed, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Shutting down.")
	if sd.count() != 0 {
		t.Fatalf("Execute called while pending")
	}
}

func TestPowerOffRejectsCrossSite(t *testing.T) {
	sd := newFakeShutdown()
	s := newTestServer(t, Deps{Shutdown: sd, CanPowerOff: true, DryRun: true})
	rec := postPowerOff(s, confirmed, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if sd.count() != 0 {
		t.Fatalf("Execute called on cross-site request")
	}
}

func TestPowerOffUnavailable(t *testing.T) {
	s := newTestServer(t, Deps{CanPowerOff: true})
	rec := postPowerOff(s, confirmed, nil)
	if loc := rec.Header().Get("Location"); loc != "/?power=unavailable" {
		t.Fatalf("Location = %q, want /?power=unavailable", loc)
	}
}

func TestFormatRemaining(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, ""},
		{-time.Minute, ""},
		{20 * time.Second, "<1m"},
		{45 * time.Minute, "~45m"},
		{3*time.Hour + 20*time.Minute, "~3h 20m"},
		{3*time.Hour + 19*time.Minute + 40*time.Second, "~3h 20m"},
		{time.Hour, "~1h 0m"},
	}
	for _, tt := range tests {
		if got := formatRemaining(tt.in); got != tt.want {
			t.Errorf("formatRemaining(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{45 * time.Second, "45s"},
		{190 * time.Second, "3m 10s"},
		{2*time.Hour + 5*time.Minute, "2h 5m"},
	}
	for _, tt := range tests {
		if got := formatAge(tt.in); got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClampSOC(t *testing.T) {
	tests := []struct {
		in   float64
		want int
	}{
		{-3, 0}, {0, 0}, {64.4, 64}, {64.5, 65}, {100, 100}, {104.7, 100},
	}
	for _, tt := range tests {
		if got := clampSOC(tt.in); got != tt.want {
			t.Errorf("clampSOC(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatAutoShutdown(t *testing.T) {
	cfg := batterySettings{ShutdownSOC: 5, ShutdownVolts: 3.3}
	dry := batterySettings{ShutdownSOC: 2.5, ShutdownVolts: 3.25, DryRun: true}
	tests := []struct {
		state battery.State
		cfg   batterySettings
		want  string
	}{
		{battery.StateOK, cfg, "Shutdown at 5% or 3.30 V"},
		{battery.StateOK, dry, "Shutdown at 2.5% or 3.25 V (dry run)"},
		{battery.StateDisabled, cfg, "Inactive: battery monitoring disabled"},
		{battery.StateNotDetected, cfg, "Inactive: gauge not detected"},
		{battery.StateReadError, cfg, "Inactive: read error"},
		{battery.StateStale, cfg, "Inactive: readings stale"},
	}
	for _, tt := range tests {
		if got := formatAutoShutdown(tt.state, tt.cfg); got != tt.want {
			t.Errorf("formatAutoShutdown(%v) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestFormatShutdowns(t *testing.T) {
	if last, list := formatShutdowns(nil); last != nil || list != nil {
		t.Fatalf("expected nothing for no events")
	}
	events := make([]battery.Event, 12)
	for i := range events {
		events[i] = battery.Event{Time: testNow.Add(-time.Duration(i) * time.Hour), Reason: battery.ReasonManual, Synced: true}
	}
	last, list := formatShutdowns(events)
	if len(list) != 10 {
		t.Fatalf("len = %d, want 10", len(list))
	}
	if last == nil || last.Time != "2026-10-29 12:00:00" || last.Reason != "manual" {
		t.Fatalf("last = %+v", last)
	}

	ev := battery.Event{Time: testNow, Reason: battery.ReasonLowBattery, Threshold: battery.ReasonSOC, Volts: 3.512, SOC: 4.6, HasBattery: true}
	got := formatShutdown(ev)
	want := ShutdownView{Time: "2026-10-29 12:00:00 (clock not synced)", Reason: "low battery", Threshold: "SOC", Reading: "3.51 V, 5%"}
	if got != want {
		t.Fatalf("formatShutdown = %+v, want %+v", got, want)
	}
}

func TestFormatPowerOff(t *testing.T) {
	tests := []struct {
		name                        string
		available, canPowerOff, dry bool
		note                        string
		want                        PowerOffView
	}{
		{"permitted", true, true, false, "", PowerOffView{Enabled: true}},
		{"not permitted", true, false, false, "polkit", PowerOffView{Note: "polkit"}},
		{"not permitted no note", true, false, false, "", PowerOffView{Note: "Power off is not permitted."}},
		{"dry run not permitted", true, false, true, "polkit", PowerOffView{Enabled: true, DryRun: true, Note: "Dry run: the request is logged only."}},
		{"unavailable", false, true, false, "", PowerOffView{Note: "Power off is not available."}},
	}
	for _, tt := range tests {
		if got := formatPowerOff(tt.available, tt.canPowerOff, tt.note, tt.dry); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestPowerMessageFromStatus(t *testing.T) {
	for _, status := range []string{"confirm-required", "not-permitted", "dry-run", "error", "unavailable"} {
		if msg, class := powerMessageFromStatus(status, "note"); msg == "" || class == "" {
			t.Errorf("%s: empty message", status)
		}
	}
	if msg, _ := powerMessageFromStatus("not-permitted", "polkit"); msg != "Power off is not permitted: polkit" {
		t.Errorf("not-permitted message = %q", msg)
	}
	if msg, class := powerMessageFromStatus("nope", ""); msg != "" || class != "" {
		t.Errorf("unknown status gave %q %q", msg, class)
	}
}
