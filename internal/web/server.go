package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/xpereta/RaspiCam/internal/mediamtx"
	"github.com/xpereta/RaspiCam/internal/metrics"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Server struct {
	tmpl         *template.Template
	mediamtxURL  string
	mediamtxPath string
}

type StatusView struct {
	GeneratedAt time.Time
	Metrics     MetricsView
	MediaMTX    MediaMTXView
	Warnings    []string
}

type MetricsView struct {
	CPUUsagePercent string
	TemperatureC    string
	VoltageV        string
	Throttled       string
	ThrottledFlags  []string
}

type MediaMTXView struct {
	ServiceStatus string
	APIStatus     string
	PathName      string
	PathReady     string
	SourceType    string
	Readers       string
	Tracks        string
}

func NewServer() (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/status.html")
	if err != nil {
		return nil, err
	}

	return &Server{
		tmpl:         tmpl,
		mediamtxURL:  getEnvDefault("MEDIAMTX_API_URL", "http://127.0.0.1:9997"),
		mediamtxPath: getEnvDefault("MEDIAMTX_PATH_NAME", "cam"),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleStatus)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	snap, warnings := metrics.Collect(ctx)
	mtxStatus, mtxWarnings := mediamtx.Collect(ctx, s.mediamtxURL, s.mediamtxPath)
	view := StatusView{
		GeneratedAt: time.Now(),
		Metrics:     formatMetrics(snap),
		MediaMTX:    formatMediaMTX(mtxStatus),
		Warnings:    append(warnings, mtxWarnings...),
	}
	if err := s.tmpl.Execute(w, view); err != nil {
		http.Error(w, "template render error", http.StatusInternalServerError)
	}
}

func formatMetrics(snap metrics.Snapshot) MetricsView {
	view := MetricsView{
		CPUUsagePercent: "unavailable",
		TemperatureC:    "unavailable",
		VoltageV:        "unavailable",
		Throttled:       "unavailable",
	}

	if snap.CPUUsagePercent != nil {
		view.CPUUsagePercent = formatFloat(*snap.CPUUsagePercent, 1) + "%"
	}
	if snap.TemperatureC != nil {
		view.TemperatureC = formatFloat(*snap.TemperatureC, 1) + " C"
	}
	if snap.VoltageV != nil {
		view.VoltageV = formatFloat(*snap.VoltageV, 4) + " V"
	}
	if snap.Throttled != nil {
		if snap.Throttled.IsThrottled {
			view.Throttled = "yes"
		} else {
			view.Throttled = "no"
		}
		view.ThrottledFlags = snap.Throttled.Flags
	}

	return view
}

func formatFloat(v float64, decimals int) string {
	return fmt.Sprintf("%.*f", decimals, v)
}

func formatMediaMTX(status mediamtx.Status) MediaMTXView {
	view := MediaMTXView{
		ServiceStatus: status.ServiceStatus,
		APIStatus:     status.APIStatus,
		PathName:      status.PathName,
		PathReady:     "unavailable",
		SourceType:    status.SourceType,
		Readers:       "unavailable",
		Tracks:        "unavailable",
	}

	if status.PathReady != nil {
		if *status.PathReady {
			view.PathReady = "yes"
		} else {
			view.PathReady = "no"
		}
	}
	if status.Readers != nil {
		view.Readers = fmt.Sprintf("%d", *status.Readers)
	}
	if status.Tracks != nil {
		view.Tracks = fmt.Sprintf("%d", *status.Tracks)
	}

	if view.ServiceStatus == "" {
		view.ServiceStatus = "unknown"
	}
	if view.APIStatus == "" {
		view.APIStatus = "unknown"
	}
	if view.SourceType == "" {
		view.SourceType = "unknown"
	}

	return view
}

func getEnvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
