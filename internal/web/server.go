package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/xpereta/RaspiCam/internal/config"
	"github.com/xpereta/RaspiCam/internal/mediamtx"
	"github.com/xpereta/RaspiCam/internal/metrics"
	"github.com/xpereta/RaspiCam/internal/system"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Server struct {
	tmpl         *template.Template
	mediamtxURL  string
	mediamtxPath string
	configPath   string
}

type StatusView struct {
	GeneratedAt string
	Hostname    string
	IPAddress   string
	DeviceModel string
	CameraModel string
	OSLabel     string
	Metrics     MetricsView
	Camera      CameraView
	MediaMTX    MediaMTXView
	Warnings    []string
}

type MetricsView struct {
	CPUUsagePercent string
	TemperatureC    string
	VoltageV        string
	Throttled       string
	ThrottledFlags  []string
	ThrottledClass  string
}

type MediaMTXView struct {
	ServiceStatus  string
	APIStatus      string
	PathName       string
	PathReady      string
	SourceType     string
	Readers        string
	Tracks         string
	ServiceClass   string
	APIClass       string
	PathReadyClass string
}

type CameraView struct {
	VFlip        bool
	HFlip        bool
	Resolution   string
	LastUpdated  string
	Message      string
	MessageClass string
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
		configPath:   getEnvDefault("MEDIAMTX_CONFIG_PATH", "/usr/local/etc/mediamtx.yml"),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleStatus)
	mux.HandleFunc("/camera-config", s.handleCameraUpdate)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	view, err := s.buildStatusView(r.Context(), "", "")
	if err != nil {
		http.Error(w, "status unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.tmpl.Execute(w, view); err != nil {
		http.Error(w, "template render error", http.StatusInternalServerError)
	}
}

func (s *Server) handleCameraUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	cfg := config.CameraConfig{
		VFlip: r.FormValue("rpiCameraVFlip") == "on",
		HFlip: r.FormValue("rpiCameraHFlip") == "on",
	}
	resolution := r.FormValue("resolution")
	if resolution != "" {
		width, height, ok := parseResolution(resolution)
		if !ok {
			view, err := s.buildStatusView(r.Context(), "Invalid resolution selection.", "notice err")
			if err != nil {
				http.Error(w, "status unavailable", http.StatusInternalServerError)
				return
			}
			if err := s.tmpl.Execute(w, view); err != nil {
				http.Error(w, "template render error", http.StatusInternalServerError)
			}
			return
		}
		cfg.Width = width
		cfg.Height = height
	}

	message := "Camera configuration saved."
	messageClass := "notice ok"
	if err := config.SaveCameraConfig(s.configPath, cfg); err != nil {
		message = "Failed to save camera configuration."
		messageClass = "notice err"
	}

	view, err := s.buildStatusView(r.Context(), message, messageClass)
	if err != nil {
		http.Error(w, "status unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.tmpl.Execute(w, view); err != nil {
		http.Error(w, "template render error", http.StatusInternalServerError)
	}
}

func (s *Server) buildStatusView(ctx context.Context, message, messageClass string) (StatusView, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	snap, warnings := metrics.Collect(ctx)
	mtxStatus, mtxWarnings := mediamtx.Collect(ctx, s.mediamtxURL, s.mediamtxPath)
	device := system.Collect()
	camera, camWarnings := s.loadCameraConfig()
	lastUpdated, ok, err := config.ConfigModTime(s.configPath)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Camera update time unavailable: %v", err))
	}

	view := StatusView{
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Hostname:    hostnameOrUnknown(),
		IPAddress:   primaryIPv4OrUnknown(),
		DeviceModel: device.Model,
		CameraModel: device.Camera,
		OSLabel:     device.OSLabel,
		Metrics:     formatMetrics(snap),
		Camera:      formatCamera(camera, lastUpdated, ok, message, messageClass),
		MediaMTX:    formatMediaMTX(mtxStatus),
		Warnings:    append(warnings, append(mtxWarnings, camWarnings...)...),
	}

	return view, nil
}

func formatMetrics(snap metrics.Snapshot) MetricsView {
	view := MetricsView{
		CPUUsagePercent: "unavailable",
		TemperatureC:    "unavailable",
		VoltageV:        "unavailable",
		Throttled:       "unavailable",
		ThrottledClass:  "badge warn",
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
			view.ThrottledClass = "badge err"
		} else {
			view.Throttled = "no"
			view.ThrottledClass = "badge ok"
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
		ServiceStatus:  status.ServiceStatus,
		APIStatus:      status.APIStatus,
		PathName:       status.PathName,
		PathReady:      "unavailable",
		SourceType:     status.SourceType,
		Readers:        "unavailable",
		Tracks:         "unavailable",
		ServiceClass:   "badge warn",
		APIClass:       "badge warn",
		PathReadyClass: "badge warn",
	}

	if status.PathReady != nil {
		if *status.PathReady {
			view.PathReady = "yes"
			view.PathReadyClass = "badge ok"
		} else {
			view.PathReady = "no"
			view.PathReadyClass = "badge err"
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
	if view.ServiceStatus == "active" {
		view.ServiceClass = "badge ok"
	} else if view.ServiceStatus == "failed" {
		view.ServiceClass = "badge err"
	}
	if view.APIStatus == "ok" {
		view.APIClass = "badge ok"
	} else if view.APIStatus == "unavailable" {
		view.APIClass = "badge err"
	}

	return view
}

func getEnvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (s *Server) loadCameraConfig() (config.CameraConfig, []string) {
	cfg, err := config.LoadCameraConfig(s.configPath)
	if err != nil {
		return config.CameraConfig{}, []string{fmt.Sprintf("Camera config unavailable: %v", err)}
	}
	return cfg, nil
}

func formatCamera(cfg config.CameraConfig, updated time.Time, ok bool, message, messageClass string) CameraView {
	lastUpdated := "never"
	if ok {
		lastUpdated = updated.Format("2006-01-02 15:04:05")
	}
	return CameraView{
		VFlip:        cfg.VFlip,
		HFlip:        cfg.HFlip,
		Resolution:   resolutionLabel(cfg.Width, cfg.Height),
		LastUpdated:  lastUpdated,
		Message:      message,
		MessageClass: messageClass,
	}
}

func parseResolution(value string) (int, int, bool) {
	switch value {
	case "1280x720":
		return 1280, 720, true
	case "1920x1080":
		return 1920, 1080, true
	default:
		return 0, 0, false
	}
}

func resolutionLabel(width, height int) string {
	if width == 1280 && height == 720 {
		return "1280x720"
	}
	if width == 1920 && height == 1080 {
		return "1920x1080"
	}
	return ""
}

func hostnameOrUnknown() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

func primaryIPv4OrUnknown() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unavailable"
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil {
			continue
		}
		if ip.IsLoopback() {
			continue
		}
		return ip.String()
	}
	return "unavailable"
}
