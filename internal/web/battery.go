package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/xpereta/RaspiCam/internal/battery"
)

// defaultPowerOffDelay gives the shutdown page time to reach the browser
// before the shutdown starts.
const defaultPowerOffDelay = time.Second

// maxShutdownsShown is how many shutdown log entries the card lists.
const maxShutdownsShown = battery.MaxShutdownLogEntries

const timeLayout = "2006-01-02 15:04:05"

// BatteryStatusProvider reports the current battery status. It never reads
// the gauge; it returns the latest snapshot.
type BatteryStatusProvider interface {
	Status() battery.Status
}

// ShutdownRequester runs the shutdown sequence (battery.Shutdown).
type ShutdownRequester interface {
	Execute(ctx context.Context, ev battery.Event) error
	Pending() bool
}

// ShutdownLister lists logged shutdowns, newest first (battery.ShutdownLog).
type ShutdownLister interface {
	List() ([]battery.Event, error)
}

// BatteryView is the battery card view model.
type BatteryView struct {
	State       string
	HasValues   bool
	Notice      string
	NoticeClass string

	Volts       string
	SOC         string
	SOCPercent  int
	BarClass    string
	Trend       string
	Remaining   string
	Level       string
	LevelClass  string
	LastReading string

	AutoShutdown string

	LastShutdown *ShutdownView
	Shutdowns    []ShutdownView

	PowerOff PowerOffView

	Message      string
	MessageClass string
}

// ShutdownView is one shutdown log entry for display.
type ShutdownView struct {
	Time      string
	Reason    string
	Threshold string
	Reading   string
	DryRun    bool
}

// PowerOffView describes the power-off form.
type PowerOffView struct {
	Enabled bool
	Note    string
	DryRun  bool
}

// batterySettings are the configured values shown on the card.
type batterySettings struct {
	ShutdownSOC   float64
	ShutdownVolts float64
	DryRun        bool
}

// formatBattery builds the card values from st. now is used for ages.
func formatBattery(st battery.Status, now time.Time, cfg batterySettings) BatteryView {
	view := BatteryView{
		State:        st.State.String(),
		AutoShutdown: formatAutoShutdown(st.State, cfg),
	}

	switch st.State {
	case battery.StateDisabled:
		view.Notice, view.NoticeClass = "Disabled", "notice warn"
		return view
	case battery.StateNotDetected:
		view.Notice, view.NoticeClass = withReason("Not detected", st.Err), "notice err"
		return view
	case battery.StateReadError:
		view.Notice, view.NoticeClass = withReason("Read error", st.Err), "notice err"
		return view
	case battery.StateStale:
		view.Notice, view.NoticeClass = staleNotice(st.ReadAt, now), "notice warn"
		return view
	case battery.StateOK:
	default:
		view.Notice, view.NoticeClass = "Unknown battery state", "notice err"
		return view
	}

	if st.ReadAt.IsZero() {
		view.Notice, view.NoticeClass = "Waiting for first reading", "notice warn"
		return view
	}

	soc := clampSOC(st.Reading.SOC)
	view.HasValues = true
	view.Volts = formatFloat(st.Reading.Volts, 2) + " V"
	view.SOC = strconv.Itoa(soc) + "%"
	view.SOCPercent = soc
	view.Trend = st.Trend.String()
	view.Remaining = formatRemaining(st.Remaining)
	view.Level = st.Decision.Level.String()
	view.LevelClass, view.BarClass = levelClasses(st.Decision.Level)
	view.LastReading = formatReadingTime(st.ReadAt, now)
	return view
}

func withReason(label, reason string) string {
	if reason == "" {
		return label
	}
	return label + ": " + reason
}

func staleNotice(readAt, now time.Time) string {
	if readAt.IsZero() {
		return "Stale: no reading yet"
	}
	return "Stale: last reading " + formatAge(now.Sub(readAt)) + " ago"
}

// clampSOC rounds soc to an integer percentage within 0-100.
func clampSOC(soc float64) int {
	if math.IsNaN(soc) {
		return 0
	}
	return int(math.Round(math.Max(0, math.Min(100, soc))))
}

// levelClasses returns the badge class and the bar fill class for l.
func levelClasses(l battery.Level) (string, string) {
	switch l {
	case battery.LevelCritical:
		return "badge err", "err"
	case battery.LevelLow:
		return "badge warn", "warn"
	default:
		return "badge ok", "ok"
	}
}

// formatRemaining returns "~3h 20m", "~45m", or "" when d is not positive.
func formatRemaining(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		return "<1m"
	}
	h, m := minutes/60, minutes%60
	if h == 0 {
		return fmt.Sprintf("~%dm", m)
	}
	return fmt.Sprintf("~%dh %dm", h, m)
}

// formatAge returns a short age such as "45s", "3m 10s" or "2h 5m".
func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	default:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
}

func formatReadingTime(at, now time.Time) string {
	return at.Format(timeLayout) + " (" + formatAge(now.Sub(at)) + " ago)"
}

// formatAutoShutdown describes the auto-shutdown thresholds, or why
// auto-shutdown is inactive (BAT-FR-12).
func formatAutoShutdown(state battery.State, cfg batterySettings) string {
	switch state {
	case battery.StateDisabled:
		return "Inactive: battery monitoring disabled"
	case battery.StateNotDetected:
		return "Inactive: gauge not detected"
	case battery.StateReadError:
		return "Inactive: read error"
	case battery.StateStale:
		return "Inactive: readings stale"
	}
	line := fmt.Sprintf("Shutdown at %s%% or %s V",
		strconv.FormatFloat(cfg.ShutdownSOC, 'f', -1, 64), formatFloat(cfg.ShutdownVolts, 2))
	if cfg.DryRun {
		line += " (dry run)"
	}
	return line
}

// formatShutdowns returns the newest event and up to maxShutdownsShown
// events (newest first, as ShutdownLog.List returns them).
func formatShutdowns(events []battery.Event) (*ShutdownView, []ShutdownView) {
	if len(events) == 0 {
		return nil, nil
	}
	if len(events) > maxShutdownsShown {
		events = events[:maxShutdownsShown]
	}
	list := make([]ShutdownView, len(events))
	for i, ev := range events {
		list[i] = formatShutdown(ev)
	}
	last := list[0]
	return &last, list
}

func formatShutdown(ev battery.Event) ShutdownView {
	v := ShutdownView{
		Time:      ev.Time.Local().Format(timeLayout),
		Reason:    formatReason(ev.Reason),
		Threshold: formatThreshold(ev.Threshold),
		Reading:   "no battery reading",
		DryRun:    ev.DryRun,
	}
	if !ev.Synced {
		v.Time += " (clock not synced)"
	}
	if ev.HasBattery {
		v.Reading = fmt.Sprintf("%s V, %d%%", formatFloat(ev.Volts, 2), clampSOC(ev.SOC))
	}
	return v
}

func formatReason(r battery.Reason) string {
	switch r {
	case battery.ReasonLowBattery:
		return "low battery"
	case battery.ReasonManual:
		return "manual"
	case "":
		return "unknown"
	default:
		return string(r)
	}
}

func formatThreshold(t string) string {
	switch t {
	case battery.ReasonSOC:
		return "SOC"
	case battery.ReasonVolts:
		return "voltage"
	default:
		return t
	}
}

// formatPowerOff describes the power-off form state.
func formatPowerOff(available, canPowerOff bool, note string, dryRun bool) PowerOffView {
	switch {
	case !available:
		return PowerOffView{Note: "Power off is not available."}
	case dryRun:
		return PowerOffView{Enabled: true, DryRun: true, Note: "Dry run: the request is logged only."}
	case !canPowerOff:
		if note == "" {
			note = "Power off is not permitted."
		}
		return PowerOffView{Note: note}
	default:
		return PowerOffView{Enabled: true}
	}
}

// powerMessageFromStatus maps the ?power= value to a notice.
func powerMessageFromStatus(status, note string) (string, string) {
	switch status {
	case "confirm-required":
		return "Tick the confirmation box to power off.", "notice warn"
	case "not-permitted":
		return withReason("Power off is not permitted", note), "notice err"
	case "dry-run":
		return "Dry run: the power-off request was logged only.", "notice ok"
	case "error":
		return "The power-off request failed; see the journal.", "notice err"
	case "unavailable":
		return "Power off is not available.", "notice err"
	default:
		return "", ""
	}
}

// buildBatteryView assembles the card and returns warnings to show on the
// page.
func (s *Server) buildBatteryView(powerStatus string) (BatteryView, []string) {
	var warnings []string
	st := s.battery.Status()
	if st.Note != "" {
		warnings = append(warnings, "Battery: "+st.Note)
	}

	view := formatBattery(st, s.now(), batterySettings{
		ShutdownSOC:   s.shutdownSOC,
		ShutdownVolts: s.shutdownVolts,
		DryRun:        s.dryRun,
	})
	if s.shutdownLog != nil {
		events, err := s.shutdownLog.List()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Shutdown log unavailable: %v", err))
		}
		view.LastShutdown, view.Shutdowns = formatShutdowns(events)
	}
	view.PowerOff = formatPowerOff(s.shutdown != nil, s.canPowerOff, s.powerOffNote, s.dryRun)
	view.Message, view.MessageClass = powerMessageFromStatus(powerStatus, s.powerOffNote)
	return view, warnings
}

// manualEvent builds the manual shutdown event from the current status.
func manualEvent(st battery.Status, client string) battery.Event {
	ev := battery.Event{Reason: battery.ReasonManual, Client: client}
	if !st.ReadAt.IsZero() {
		ev.HasBattery = true
		ev.Volts = st.Reading.Volts
		ev.SOC = st.Reading.SOC
	}
	return ev
}

// handlePowerOff serves POST /power-off (BAT-FR-17).
func (s *Server) handlePowerOff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if r.PostFormValue("confirm") != "yes" {
		http.Redirect(w, r, "/?power=confirm-required", http.StatusSeeOther)
		return
	}
	if s.shutdown == nil {
		http.Redirect(w, r, "/?power=unavailable", http.StatusSeeOther)
		return
	}
	if !s.dryRun && !s.canPowerOff {
		http.Redirect(w, r, "/?power=not-permitted", http.StatusSeeOther)
		return
	}

	if s.dryRun {
		status := "dry-run"
		ev := manualEvent(s.battery.Status(), r.RemoteAddr)
		if err := s.shutdown.Execute(r.Context(), ev); err != nil && !errors.Is(err, battery.ErrShutdownInProgress) {
			log.Printf("web: power off (dry run): %v", err)
			status = "error"
		}
		http.Redirect(w, r, "/?power="+status, http.StatusSeeOther)
		return
	}

	pending := s.shutdown.Pending()
	if err := s.renderShutdownPage(w); err != nil {
		log.Printf("web: render shutdown page: %v", err)
	}
	if pending {
		return
	}
	ev := manualEvent(s.battery.Status(), r.RemoteAddr)
	go s.executeShutdown(ev)
}

func (s *Server) renderShutdownPage(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tmpl.ExecuteTemplate(w, "shutdown.html", nil); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// executeShutdown waits for the page to reach the browser and then runs the
// shutdown with its own context, independent of the finished request.
func (s *Server) executeShutdown(ev battery.Event) {
	s.powerOffDelay()
	err := s.shutdown.Execute(context.Background(), ev)
	if err != nil && !errors.Is(err, battery.ErrShutdownInProgress) {
		log.Printf("web: power off: %v", err)
	}
}
