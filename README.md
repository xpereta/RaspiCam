# RaspiCam

Lightweight status UI and MediaMTX helpers for a Raspberry Pi Zero 2 W camera setup.

## What This Is
- MediaMTX runs the camera stream.
- A small Go web UI shows health and MediaMTX status.
- The UI can edit two camera config flags in `mediamtx.yml`:
  - `rpiCameraVFlip`
  - `rpiCameraHFlip`
- Optional battery monitoring (MAX17048) with low-battery safe shutdown.

## Quick Start (Local)
```
go run ./cmd/ui
```
Open `http://localhost:8080`.

## Quick Start (Pi)
1) Build:
```
cd /path/to/RaspiCam
 go build -o raspicam-ui ./cmd/ui
```
2) Install:
```
sudo mv raspicam-ui /usr/local/bin/
```
3) Create systemd unit (see `SYSTEM.md`).

## Environment Variables
- `UI_ADDR` (default `:8080`)
- `MEDIAMTX_API_URL` (default `http://127.0.0.1:9997`)
- `MEDIAMTX_PATH_NAME` (default `cam`)
- `MEDIAMTX_CONFIG_PATH` (default `/usr/local/etc/mediamtx.yml`)

Battery monitor (valid ranges in `SYSTEM.md`; invalid values fall back to the
default with a startup warning):
- `BATTERY_ENABLED` (default `true`)
- `BATTERY_I2C_DEVICE` (default `/dev/i2c-1`)
- `BATTERY_I2C_ADDR` (default `0x36`)
- `BATTERY_POLL_INTERVAL` (default `30s`)
- `BATTERY_LOW_SOC` (default `20`)
- `BATTERY_SHUTDOWN_SOC` (default `5`)
- `BATTERY_SHUTDOWN_VOLTS` (default `3.30`)
- `BATTERY_SHUTDOWN_SAMPLES` (default `3`)
- `BATTERY_GRACE_PERIOD` (default `2m`)
- `BATTERY_SHUTDOWN_DRY_RUN` (default `false`)
- `STATE_DIR` (default `$STATE_DIRECTORY`, else `/var/lib/raspicam`)
- `BATTERY_HISTORY_ENABLED` (default `true`; phase 2, not used yet)
- `BATTERY_HISTORY_SAMPLE_INTERVAL` (default `60s`; phase 2, not used yet)
- `BATTERY_HISTORY_FLUSH_INTERVAL` (default `15m`; phase 2, not used yet)
- `BATTERY_HISTORY_MAX_SIZE` (default `10MB`; phase 2, not used yet)
- `BATTERY_FULL_SOC` (default `95`; phase 2, not used yet)

## Notes
- Camera config changes edit `mediamtx.yml`. MediaMTX auto-restarts on file changes.
- The UI shows the last update time using the file modification time of `mediamtx.yml`.

## Battery Monitoring
An optional MAX17048 fuel gauge on the battery pack is read over I2C every
30s. The status page shows a Battery card with voltage, SOC, trend, time
remaining and the last shutdowns, plus a power-off button. When the battery
is nearly empty (default 5% SOC or 3.30 V, 3 consecutive readings) the UI
powers the Pi off cleanly; a dry-run mode only logs. Wiring, I2C, permissions
and calibration are in `SYSTEM.md` ("Battery Monitoring (MAX17048)"); design
in `PRD_BATTERY_MONITOR.md` and `SPEC_BATTERY_MONITOR.md`.

Before relying on auto-shutdown, measure the UPS module's cutoff voltage and
set `BATTERY_SHUTDOWN_VOLTS` above it; the test procedure is in `SYSTEM.md`
("Finding the Cutoff Voltage").

Power tip: the 18650 UPS charge/boost module draws about 25mA on its own.
Desoldering its output indicator LED saves more than 10mA (seller spec). That
is about 2% while streaming (roughly 10-15 extra minutes per charge), and a
larger share when the Pi is idle or halted after a low-battery shutdown.

## UI Endpoints
- `GET /` status UI
- `POST /camera-config` update camera flip settings; cross-origin requests are rejected with `403 Forbidden` (Sec-Fetch-Site/Origin check)
- `POST /power-off` power the Pi off; requires `confirm=yes`; cross-origin requests are rejected with `403 Forbidden` (same check); only logged in dry-run mode

## Testing
```
go test ./...
```
