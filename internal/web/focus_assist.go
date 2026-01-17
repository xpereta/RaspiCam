package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xpereta/RaspiCam/internal/config"
	"github.com/xpereta/RaspiCam/internal/focusassist"
)

type FocusAssistView struct {
	NavActive        string
	GeneratedAt      string
	Status           FocusAssistStatusView
	Results          []FocusAssistResultView
	Best             *FocusAssistResultView
	Selected         *FocusAssistResultView
	Message          string
	MessageClass     string
	CaptureStatus    string
	CameraAfMode     string
	Form             FocusAssistFormView
	Logs             []FocusAssistLogView
	HasResults       bool
	ApplyDisabled    bool
	SelectedDisabled bool
}

type FocusAssistFormView struct {
	Region    string
	Start     string
	End       string
	Step      string
	RegionTip string
}

type FocusAssistStatusView struct {
	State        string
	StateClass   string
	Message      string
	Total        int
	Completed    int
	CurrentLens  string
	ProgressText string
	ProgressPct  int
	UpdatedAt    string
	Running      bool
}

type FocusAssistResultView struct {
	ID           string
	LensPosition string
	Score        string
	IsBest       bool
}

type FocusAssistLogView struct {
	Time         string
	LensPosition string
	Status       string
	Output       string
}

func (s *Server) handleFocusAssist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	message, messageClass := focusAssistMessageFromStatus(r.URL.Query().Get("status"))
	selectedID := strings.TrimSpace(r.URL.Query().Get("select"))
	view, err := s.buildFocusAssistView(selectedID, message, messageClass)
	if err != nil {
		http.Error(w, "focus assist unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.tmpl.ExecuteTemplate(w, "focus_assist.html", view); err != nil {
		http.Error(w, "template render error", http.StatusInternalServerError)
	}
}

func (s *Server) handleFocusAssistScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/focus-assist?status=invalid-form", http.StatusSeeOther)
		return
	}

	region := focusassist.Region(strings.TrimSpace(r.FormValue("region")))
	if !isValidRegion(region) {
		http.Redirect(w, r, "/focus-assist?status=invalid-region", http.StatusSeeOther)
		return
	}

	start, ok := parseFloatField(r.FormValue("start"))
	if !ok {
		http.Redirect(w, r, "/focus-assist?status=invalid-start", http.StatusSeeOther)
		return
	}
	end, ok := parseFloatField(r.FormValue("end"))
	if !ok {
		http.Redirect(w, r, "/focus-assist?status=invalid-end", http.StatusSeeOther)
		return
	}
	step, ok := parseFloatField(r.FormValue("step"))
	if !ok {
		http.Redirect(w, r, "/focus-assist?status=invalid-step", http.StatusSeeOther)
		return
	}

	scanCtx, _ := context.WithTimeout(context.Background(), 2*time.Minute)

	err := s.focusAssist.StartScan(scanCtx, focusassist.ScanParams{
		Start:  start,
		End:    end,
		Step:   step,
		Region: region,
	})
	if err != nil {
		switch err {
		case focusassist.ErrScanRunning:
			http.Redirect(w, r, "/focus-assist?status=scan-running", http.StatusSeeOther)
			return
		case focusassist.ErrCaptureUnavailable:
			http.Redirect(w, r, "/focus-assist?status=capture-unavailable", http.StatusSeeOther)
			return
		default:
			http.Redirect(w, r, "/focus-assist?status=scan-error", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/focus-assist?status=scan-started", http.StatusSeeOther)
}

func (s *Server) handleFocusAssistImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	kind := strings.TrimSpace(r.URL.Query().Get("type"))
	if id == "" {
		http.NotFound(w, r)
		return
	}

	imageKind := focusassist.ImageFull
	if kind == string(focusassist.ImageThumb) {
		imageKind = focusassist.ImageThumb
	}
	data, ok := s.focusAssist.Image(id, imageKind)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleFocusAssistApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/focus-assist?status=invalid-form", http.StatusSeeOther)
		return
	}

	lensRaw := strings.TrimSpace(r.FormValue("lens"))
	lensValue, ok := parseLensPosition(lensRaw)
	if !ok || lensValue < 0 {
		http.Redirect(w, r, "/focus-assist?status=invalid-lens-position", http.StatusSeeOther)
		return
	}

	cfg, err := config.LoadCameraConfig(s.configPath)
	if err != nil {
		http.Redirect(w, r, "/focus-assist?status=load-error", http.StatusSeeOther)
		return
	}

	cfg.AfMode = "manual"
	cfg.LensPositionSet = true
	cfg.LensPosition = &lensValue

	if err := config.SaveCameraConfig(s.configPath, cfg); err != nil {
		http.Redirect(w, r, "/focus-assist?status=save-error", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/focus-assist?status=applied", http.StatusSeeOther)
}

func (s *Server) handleFocusAssistAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg, err := config.LoadCameraConfig(s.configPath)
	if err != nil {
		http.Redirect(w, r, "/focus-assist?status=load-error", http.StatusSeeOther)
		return
	}

	cfg.AfMode = "continuous"
	cfg.LensPositionSet = true
	cfg.LensPosition = nil

	if err := config.SaveCameraConfig(s.configPath, cfg); err != nil {
		http.Redirect(w, r, "/focus-assist?status=save-error", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/focus-assist?status=auto-reset", http.StatusSeeOther)
}

func (s *Server) buildFocusAssistView(selectedID, message, messageClass string) (FocusAssistView, error) {
	cameraAfMode := "unknown"
	if cfg, err := config.LoadCameraConfig(s.configPath); err == nil {
		cameraAfMode = cfg.AfMode
	} else if message == "" {
		message = fmt.Sprintf("Camera config unavailable: %v", err)
		messageClass = "notice warn"
	}

	snapshot := s.focusAssist.Snapshot()
	statusView := buildFocusAssistStatus(snapshot.Status)
	results := make([]FocusAssistResultView, 0, len(snapshot.Results))
	logs := make([]FocusAssistLogView, 0, len(snapshot.Logs))
	var best *FocusAssistResultView
	var selected *FocusAssistResultView

	for _, result := range snapshot.Results {
		view := FocusAssistResultView{
			ID:           result.ID,
			LensPosition: formatFloat(result.LensPosition, 2),
			Score:        formatFloat(result.Score, 2),
			IsBest:       result.ID == snapshot.BestID,
		}
		if view.IsBest {
			best = &view
		}
		if result.ID == selectedID {
			selected = &view
		}
		results = append(results, view)
	}
	for i := len(snapshot.Logs) - 1; i >= 0; i-- {
		entry := snapshot.Logs[i]
		status := "ok"
		if !entry.Success {
			status = "error"
		}
		output := strings.TrimSpace(entry.Output)
		if entry.Error != "" {
			if output != "" {
				output = output + "\n" + entry.Error
			} else {
				output = entry.Error
			}
		}
		logs = append(logs, FocusAssistLogView{
			Time:         entry.Time.Format("15:04:05"),
			LensPosition: formatFloat(entry.LensPosition, 2),
			Status:       status,
			Output:       output,
		})
	}

	form := FocusAssistFormView{
		Region:    string(focusassist.RegionCenter),
		Start:     "0.0",
		End:       "2.0",
		Step:      "0.1",
		RegionTip: "Center focuses on the middle third of the frame.",
	}

	captureStatus := ""
	if s.captureUnavailable != "" {
		captureStatus = s.captureUnavailable
	}

	view := FocusAssistView{
		NavActive:        "focus",
		GeneratedAt:      time.Now().Format("2006-01-02 15:04:05"),
		Status:           statusView,
		Results:          results,
		Best:             best,
		Selected:         selected,
		Message:          message,
		MessageClass:     messageClass,
		CaptureStatus:    captureStatus,
		CameraAfMode:     cameraAfMode,
		Form:             form,
		Logs:             logs,
		HasResults:       len(results) > 0,
		ApplyDisabled:    best == nil,
		SelectedDisabled: selected == nil,
	}
	return view, nil
}

func buildFocusAssistStatus(status focusassist.Status) FocusAssistStatusView {
	stateClass := "badge warn"
	running := false
	switch status.State {
	case focusassist.StateRunning:
		stateClass = "badge warn"
		running = true
	case focusassist.StateDone:
		stateClass = "badge ok"
	case focusassist.StateFailed:
		stateClass = "badge err"
	case focusassist.StateIdle:
		stateClass = "badge warn"
	}

	progressPct := 0
	progressText := ""
	if status.Total > 0 {
		progressPct = int((float64(status.Completed) / float64(status.Total)) * 100)
		progressText = fmt.Sprintf("%d/%d", status.Completed, status.Total)
	}

	currentLens := ""
	if status.State == focusassist.StateRunning {
		currentLens = formatFloat(status.CurrentLens, 2)
	}

	updatedAt := ""
	if !status.UpdatedAt.IsZero() {
		updatedAt = status.UpdatedAt.Format("15:04:05")
	}

	return FocusAssistStatusView{
		State:        status.State,
		StateClass:   stateClass,
		Message:      status.Message,
		Total:        status.Total,
		Completed:    status.Completed,
		CurrentLens:  currentLens,
		ProgressText: progressText,
		ProgressPct:  progressPct,
		UpdatedAt:    updatedAt,
		Running:      running,
	}
}

func focusAssistMessageFromStatus(status string) (string, string) {
	switch status {
	case "scan-started":
		return "Focus scan started.", "notice ok"
	case "scan-running":
		return "A scan is already running.", "notice warn"
	case "scan-error":
		return "Failed to start scan.", "notice err"
	case "invalid-form":
		return "Invalid focus assist form data.", "notice err"
	case "invalid-start":
		return "Invalid start value.", "notice err"
	case "invalid-end":
		return "Invalid end value.", "notice err"
	case "invalid-step":
		return "Invalid step value.", "notice err"
	case "invalid-region":
		return "Invalid focus region selection.", "notice err"
	case "capture-unavailable":
		return "Focus capture command not found. Install rpicam-still or libcamera-still.", "notice warn"
	case "applied":
		return "Lens position applied to configuration.", "notice ok"
	case "auto-reset":
		return "Autofocus restored to continuous mode.", "notice ok"
	case "invalid-lens-position":
		return "Invalid lens position value.", "notice err"
	case "save-error":
		return "Failed to save camera configuration.", "notice err"
	case "load-error":
		return "Failed to load camera configuration.", "notice err"
	default:
		return "", ""
	}
}

func isValidRegion(region focusassist.Region) bool {
	switch region {
	case focusassist.RegionCenter, focusassist.RegionThirds, focusassist.RegionFull:
		return true
	default:
		return false
	}
}

func parseFloatField(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
