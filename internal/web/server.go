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
	Network     NetworkView
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

type NetworkView struct {
	Interface       string
	IPAddress       string
	RxRate          string
	TxRate          string
	WiFiSSID        string
	WiFiRate        string
	WiFiLinkQuality string
}

type CameraView struct {
	VFlip        bool
	HFlip        bool
	Resolution   string
	AWB          string
	Mode         string
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

	message, messageClass := cameraMessageFromStatus(r.URL.Query().Get("camera"))
	view, err := s.buildStatusView(r.Context(), message, messageClass)
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
			http.Redirect(w, r, "/?camera=invalid-resolution", http.StatusSeeOther)
			return
		}
		cfg.Width = width
		cfg.Height = height
	}
	awb := r.FormValue("rpiCameraAWB")
	if awb != "" {
		if !isValidAWB(awb) {
			http.Redirect(w, r, "/?camera=invalid-awb", http.StatusSeeOther)
			return
		}
		cfg.AWB = awb
	}
	mode := r.FormValue("rpiCameraMode")
	if mode != "" && !isValidCameraMode(mode) {
		http.Redirect(w, r, "/?camera=invalid-mode", http.StatusSeeOther)
		return
	}
	cfg.Mode = mode

	if err := config.SaveCameraConfig(s.configPath, cfg); err != nil {
		http.Redirect(w, r, "/?camera=save-error", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/?camera=saved", http.StatusSeeOther)
}

func (s *Server) buildStatusView(ctx context.Context, message, messageClass string) (StatusView, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	snap, warnings := metrics.Collect(ctx)
	mtxStatus, mtxWarnings := mediamtx.Collect(ctx, s.mediamtxURL, s.mediamtxPath)
	device := system.Collect()
	network, networkWarnings := system.CollectNetwork(ctx)
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
		Network:     formatNetwork(network),
		Warnings:    append(warnings, append(append(mtxWarnings, camWarnings...), networkWarnings...)...),
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

func formatNetwork(snap system.NetworkSnapshot) NetworkView {
	view := NetworkView{
		Interface:       "unavailable",
		IPAddress:       "unavailable",
		RxRate:          "unavailable",
		TxRate:          "unavailable",
		WiFiSSID:        "unavailable",
		WiFiRate:        "unavailable",
		WiFiLinkQuality: "unavailable",
	}

	if snap.Interface != "" {
		view.Interface = snap.Interface
	}
	if snap.IPAddress != "" {
		view.IPAddress = snap.IPAddress
	}
	if snap.RxBytesPerSec != nil {
		view.RxRate = formatRate(*snap.RxBytesPerSec)
	}
	if snap.TxBytesPerSec != nil {
		view.TxRate = formatRate(*snap.TxBytesPerSec)
	}
	if snap.WiFiSSID != "" {
		view.WiFiSSID = snap.WiFiSSID
	}
	if snap.WiFiLinkQuality != "" {
		view.WiFiLinkQuality = snap.WiFiLinkQuality
	}
	view.WiFiRate = formatWiFiRate(snap.WiFiTxRate, snap.WiFiRxRate)

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
		AWB:          cfg.AWB,
		Mode:         cfg.Mode,
		LastUpdated:  lastUpdated,
		Message:      message,
		MessageClass: messageClass,
	}
}

func formatRate(bytesPerSec float64) string {
	unit := "B/s"
	value := bytesPerSec
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	for i := 0; i < len(units); i++ {
		if value < 1024 || i == len(units)-1 {
			unit = units[i]
			break
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func formatWiFiRate(txRate, rxRate string) string {
	if txRate == "" && rxRate == "" {
		return "unavailable"
	}
	if txRate != "" && rxRate != "" {
		return fmt.Sprintf("TX %s, RX %s", txRate, rxRate)
	}
	if txRate != "" {
		return "TX " + txRate
	}
	return "RX " + rxRate
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

func isValidAWB(value string) bool {
	switch value {
	case "auto", "incandescent", "tungsten", "fluorescent", "indoor", "daylight", "cloudy", "custom":
		return true
	default:
		return false
	}
}

func isValidCameraMode(value string) bool {
	switch value {
	case "2304:1296:10:P", "1536:864:10:P":
		return true
	default:
		return false
	}
}

func cameraMessageFromStatus(status string) (string, string) {
	switch status {
	case "saved":
		return "Camera configuration saved.", "notice ok"
	case "save-error":
		return "Failed to save camera configuration.", "notice err"
	case "invalid-resolution":
		return "Invalid resolution selection.", "notice err"
	case "invalid-awb":
		return "Invalid AWB selection.", "notice err"
	case "invalid-mode":
		return "Invalid camera mode selection.", "notice err"
	default:
		return "", ""
	}
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
