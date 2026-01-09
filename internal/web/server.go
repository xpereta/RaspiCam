package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/xpereta/RaspiCam/internal/metrics"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Server struct {
	tmpl *template.Template
}

type StatusView struct {
	GeneratedAt time.Time
	Metrics     MetricsView
	Warnings    []string
}

type MetricsView struct {
	CPUUsagePercent string
	TemperatureC    string
	VoltageV        string
	Throttled       string
	ThrottledFlags  []string
}

func NewServer() (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/status.html")
	if err != nil {
		return nil, err
	}

	return &Server{tmpl: tmpl}, nil
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
	view := StatusView{
		GeneratedAt: time.Now(),
		Metrics:     formatMetrics(snap),
		Warnings:    warnings,
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
