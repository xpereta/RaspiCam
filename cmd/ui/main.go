package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xpereta/RaspiCam/internal/battery"
	"github.com/xpereta/RaspiCam/internal/config"
	"github.com/xpereta/RaspiCam/internal/system"
	"github.com/xpereta/RaspiCam/internal/web"
)

const (
	powerCheckTimeout   = 5 * time.Second
	httpShutdownTimeout = 5 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	err := run(ctx)
	stop()
	if err != nil {
		log.Fatal(err)
	}
}

// run wires the UI and blocks until ctx is done or the HTTP server fails.
func run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cfg, warnings := config.LoadBatteryConfig(os.Getenv)
	for _, w := range warnings {
		log.Printf("config: %s", w)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		log.Printf("state dir: %v (shutdown log writes will fail)", err)
	}
	shutdownLog := battery.NewShutdownLog(filepath.Join(cfg.StateDir, "shutdowns.jsonl"))

	power := system.SystemdPower{}
	canPowerOff, powerOffNote := checkPowerOff(ctx, power)

	sd := battery.NewShutdown(shutdownLog, power, cfg.DryRun, nil, nil,
		battery.WithTimeSynced(system.TimeSynced))

	bat := buildBattery(cfg, sd)
	defer bat.close()

	monitorDone := make(chan struct{})
	if bat.monitor != nil {
		go func() {
			defer close(monitorDone)
			bat.monitor.Run(ctx)
		}()
	} else {
		close(monitorDone)
	}

	srv, err := web.NewServer(web.Deps{
		MediaMTXURL:   getEnvDefault("MEDIAMTX_API_URL", "http://127.0.0.1:9997"),
		MediaMTXPath:  getEnvDefault("MEDIAMTX_PATH_NAME", "cam"),
		ConfigPath:    getEnvDefault("MEDIAMTX_CONFIG_PATH", "/usr/local/etc/mediamtx.yml"),
		Battery:       bat.status,
		Shutdown:      sd,
		ShutdownLog:   shutdownLog,
		CanPowerOff:   canPowerOff,
		PowerOffNote:  powerOffNote,
		DryRun:        cfg.DryRun,
		ShutdownSOC:   cfg.ShutdownSOC,
		ShutdownVolts: cfg.ShutdownVolts,
		Warnings:      warnings,
	})
	if err != nil {
		cancel()
		<-monitorDone
		return fmt.Errorf("init server: %w", err)
	}

	addr := getEnvDefault("UI_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("ui listening on %s", addr)
		serveErr <- httpServer.ListenAndServe()
	}()

	var runErr error
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		log.Printf("ui stopping")
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), httpShutdownTimeout)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		cancelShutdown()
	}

	cancel()
	<-monitorDone
	return runErr
}

// checkPowerOff asks logind once whether the service may power off.
func checkPowerOff(ctx context.Context, power system.SystemdPower) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, powerCheckTimeout)
	defer cancel()
	ok, note := power.CanPowerOff(ctx)
	if ok {
		log.Printf("power off: permitted")
	} else {
		log.Printf("power off: not permitted: %s", note)
	}
	return ok, note
}

// batteryRuntime is the battery status source and, when a gauge was found,
// the monitor that polls it.
type batteryRuntime struct {
	status  battery.StatusProvider
	monitor *battery.Monitor
	gauge   *battery.MAX17048
}

// close releases the I2C bus. Call it after the monitor has stopped.
func (b batteryRuntime) close() {
	if b.gauge == nil {
		return
	}
	if err := b.gauge.Close(); err != nil {
		log.Printf("battery: close bus: %v", err)
	}
}

// buildBattery opens the gauge and builds the monitor. Without a gauge it
// returns a static Disabled or Not detected status, so no shutdown can
// trigger (BAT-FR-12).
func buildBattery(cfg config.BatteryConfig, sd *battery.Shutdown) batteryRuntime {
	if !cfg.Enabled {
		log.Printf("battery: disabled")
		return batteryRuntime{status: battery.NewDisabledStatus()}
	}

	gauge, err := openGauge(cfg)
	if err != nil {
		log.Printf("battery: gauge not detected: %v", err)
		return batteryRuntime{status: battery.NewNotDetectedStatus(err)}
	}
	if w := gauge.Warning(); w != "" {
		log.Printf("battery: %s", w)
	}
	log.Printf("battery: MAX17048 detected on %s at 0x%02x (version 0x%04x)",
		cfg.I2CDevice, cfg.I2CAddr, gauge.Version())

	policy := battery.NewPolicy(battery.PolicyConfig{
		LowSOC:          cfg.LowSOC,
		ShutdownSOC:     cfg.ShutdownSOC,
		ShutdownVolts:   cfg.ShutdownVolts,
		RequiredSamples: cfg.ShutdownSamples,
		GracePeriod:     cfg.GracePeriod,
	})
	monitor := battery.NewMonitor(gauge, policy, sd, cfg.PollInterval,
		battery.MonitorClock{Now: time.Now, Uptime: system.Uptime})
	return batteryRuntime{status: monitor, monitor: monitor, gauge: gauge}
}

// openGauge opens the bus and detects the gauge, closing the bus when
// detection fails.
func openGauge(cfg config.BatteryConfig) (*battery.MAX17048, error) {
	bus, err := battery.OpenBus(cfg.I2CDevice)
	if err != nil {
		return nil, err
	}
	gauge, err := battery.NewMAX17048(bus, cfg.I2CAddr)
	if err != nil {
		if cerr := bus.Close(); cerr != nil {
			log.Printf("battery: close bus: %v", cerr)
		}
		return nil, err
	}
	return gauge, nil
}

func getEnvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
