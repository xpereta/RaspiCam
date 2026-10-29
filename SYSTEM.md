# System Overview: Raspberry Pi Zero 2 W Streaming Stack

## Purpose
Provide a low-power, always-on streaming setup using a Raspberry Pi Zero 2 W with a Pi Camera Module V3. The system exposes the stream via MediaMTX and provides a lightweight local web UI for health status and basic configuration.

## Goals
- Stream camera output reliably with minimal resource usage.
- Auto-start all services on boot.
- Provide a small web UI for status and configuration.
- Keep the system operable on 32-bit Raspberry Pi OS.

## Non-Goals (for now)
- Multi-camera support.
- Remote/cloud management.
- User authentication/authorization.
- Video analytics or storage.

## Components

### 1) Streaming Component (MediaMTX)
- Runs on Raspberry Pi Zero 2 W (32-bit Raspberry Pi OS).
- Auto-starts at boot (systemd service).
- Responsible for exposing the camera stream.
- Configuration is minimal and tuned for low power and low latency.

### 2) Status and Configuration UI
- Lightweight local web server running on the Pi.
- Auto-starts at boot (systemd service).
- Provides:
  - Basic system health status (CPU load, memory, disk, temperature, uptime).
  - MediaMTX status (service up/down, stream state if available).
  - Network status (IP address, interface, rates, WiFi details).
  - A form to edit selected configuration parameters (TBD).
  - Battery status (MAX17048 fuel gauge), low-battery auto-shutdown and a
    power-off button (see "Battery Monitoring (MAX17048)").
- Uses MediaMTX Control API for health/state where applicable:
  - https://mediamtx.org/docs/usage/control-api
  - https://mediamtx.org/docs/references/control-api
  - OpenAPI spec: https://github.com/bluenviron/mediamtx/blob/v1.15.6/api/openapi.yaml
- Reads device metrics locally via:
  - `vcgencmd measure_temp`
  - `vcgencmd measure_volts`
  - `vcgencmd get_throttled`
- Reads the battery fuel gauge directly over I2C (`/dev/i2c-1`).

### 3) Battery Fuel Gauge (MAX17048)
- Optional I2C fuel gauge on the battery pack.
- Polled by a monitor inside the UI process (no extra service).
- Triggers a clean `systemctl poweroff` when the battery is nearly empty.

## Technical Stack (UI)
- Language/runtime: Go 1.22+.
- Server: Go standard library (`net/http`, `html/template`).
- UI: server-rendered HTML with no auto-refresh.
- Packaging: single static binary for low footprint.
- MediaMTX API endpoint is configurable via `MEDIAMTX_API_URL` (default `http://127.0.0.1:9997`).
- MediaMTX path name is configurable via `MEDIAMTX_PATH_NAME` (default `cam`).

## Runtime Architecture (High Level)
- Pi Camera Module V3 -> MediaMTX ingest pipeline -> network stream output.
- UI web server reads local system metrics and MediaMTX Control API on request.
- UI writes configuration updates to a local config file and/or MediaMTX config.
- All services managed by systemd.

## Operational Constraints
- Raspberry Pi Zero 2 W resources are limited (CPU, RAM).
- 32-bit OS; prefer lightweight runtimes (Go or Python with minimal deps).
- Networking may be unreliable; UI should degrade gracefully.

## Security Posture (Initial)
- Local network access only; no authentication initially.
- Control API should bind to localhost or be access-restricted.
- State-changing POSTs reject cross-origin requests (Sec-Fetch-Site/Origin check).

## Observability
- Basic logs for MediaMTX and UI (systemd journal).
- UI exposes a simple status page with last update time.

## Service Management
- Systemd unit file for the UI lives at `systemd/raspicam-ui.service`.
- Follow the same start-on-boot pattern as MediaMTX:
  - Build the UI binary on the Pi:
    ```
    cd /path/to/RaspiCam
    go build -o raspicam-ui ./cmd/ui
    ```
  - Move the binary to a global path:
    ```
    sudo mv raspicam-ui /usr/local/bin/
    ```
  - Create the service:
```
sudo tee /etc/systemd/system/raspicam-ui.service > /dev/null << EOF
[Unit]
Description=RaspiCam Status UI
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# Adjust User/Group as needed for your Pi setup.
User=pi
Group=pi
# I2C access for the battery fuel gauge.
SupplementaryGroups=i2c
# Creates /var/lib/raspicam (owned by User=) and sets STATE_DIRECTORY.
StateDirectory=raspicam
Environment=UI_ADDR=:8080
Environment=MEDIAMTX_API_URL=http://127.0.0.1:9997
Environment=MEDIAMTX_PATH_NAME=cam
# Battery monitor (defaults shown; uncomment to change).
#Environment=BATTERY_ENABLED=true
#Environment=BATTERY_SHUTDOWN_SOC=5
#Environment=BATTERY_SHUTDOWN_VOLTS=3.30
#Environment=BATTERY_SHUTDOWN_DRY_RUN=false
ExecStart=/usr/local/bin/raspicam-ui
# The low-battery monitor runs inside the UI; restart it quickly on any exit.
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF
```
  - User/Group: the UI runs as a non-root user (`pi` above; match your Pi
    setup). The same user must be in the polkit rule (see "Battery
    Monitoring (MAX17048)").
  - Battery settings: all battery environment variables (see "Environment
    Variables") can be added as `Environment=` lines in `[Service]`. During
    calibration set `Environment=BATTERY_SHUTDOWN_DRY_RUN=true`.
  - Ensure network is ready before start:
    ```
    sudo systemctl enable systemd-networkd-wait-online.service
    ```
  - Enable and start:
    ```
    sudo systemctl daemon-reload
    sudo systemctl enable raspicam-ui
    sudo systemctl start raspicam-ui
    ```
  - Verify:
    ```
    systemctl status raspicam-ui
    journalctl -u raspicam-ui -f
    ```
  - Permissions:
    - The UI must be able to write `/usr/local/etc/mediamtx.yml`.
    - MediaMTX auto-restarts on file changes; no manual restart required.
    - Battery monitoring needs the `i2c` group and the polkit power-off rule
      (see "Battery Monitoring (MAX17048)").

## Environment Variables
- `UI_ADDR` (default `:8080`)
- `MEDIAMTX_API_URL` (default `http://127.0.0.1:9997`)
- `MEDIAMTX_PATH_NAME` (default `cam`)
- `MEDIAMTX_CONFIG_PATH` (default `/usr/local/etc/mediamtx.yml`)

Battery monitor. Unset or empty values use the default. Invalid or
out-of-range values are replaced by the default and logged as a warning at
startup; the UI never fails to start because of them.

| Variable | Default | Valid |
|---|---|---|
| `BATTERY_ENABLED` | `true` | bool (`true`/`false`, `1`/`0`) |
| `BATTERY_I2C_DEVICE` | `/dev/i2c-1` | absolute path |
| `BATTERY_I2C_ADDR` | `0x36` | 0x03-0x77 (hex `0x36`, octal `0o66` or decimal `54`) |
| `BATTERY_POLL_INTERVAL` | `30s` | 10s-10m (Go duration) |
| `BATTERY_LOW_SOC` | `20` | 1-90 (percent) |
| `BATTERY_SHUTDOWN_SOC` | `5` | 0-50, must be below `BATTERY_LOW_SOC` |
| `BATTERY_SHUTDOWN_VOLTS` | `3.30` | 3.00-3.80 (volts) |
| `BATTERY_SHUTDOWN_SAMPLES` | `3` | 1-20 |
| `BATTERY_GRACE_PERIOD` | `2m` | 0-30m, measured from system boot |
| `BATTERY_SHUTDOWN_DRY_RUN` | `false` | bool |
| `STATE_DIR` | `$STATE_DIRECTORY`, else `/var/lib/raspicam` | absolute path |

- If `BATTERY_SHUTDOWN_SOC` is not below `BATTERY_LOW_SOC`, both fall back to
  their defaults (5 and 20).
- State directory resolution: `STATE_DIR`, then `STATE_DIRECTORY` (set by
  systemd `StateDirectory=`; the first entry if it is a colon-separated
  list), then `/var/lib/raspicam`.

Phase 2 (battery history; parsed and validated, not used yet):

| Variable | Default | Valid |
|---|---|---|
| `BATTERY_HISTORY_ENABLED` | `true` | bool |
| `BATTERY_HISTORY_SAMPLE_INTERVAL` | `60s` | 30s-10m; rounded up to a multiple of `BATTERY_POLL_INTERVAL` |
| `BATTERY_HISTORY_FLUSH_INTERVAL` | `15m` | 1m-1h |
| `BATTERY_HISTORY_MAX_SIZE` | `10MB` | 1MB-500MB; integer with optional `B`, `KB`, `MB`, `GB` suffix (case-insensitive, 1024-based, no fractions or spaces) |
| `BATTERY_FULL_SOC` | `95` | 50-100 (percent) |

Phase 2 history files will live under `<state dir>/battery`.

## Battery Monitoring (MAX17048)
The UI reads a MAX17048 fuel gauge on the 1S battery pack (2x18650 in
parallel) and powers the Pi off cleanly before the pack is empty. Design:
`PRD_BATTERY_MONITOR.md` and `SPEC_BATTERY_MONITOR.md`.

### Behavior
- The monitor polls the gauge every `BATTERY_POLL_INTERVAL` (default 30s),
  reading it directly over `/dev/i2c-1` (`I2C_RDWR` ioctl, no kernel driver).
  Access is read-only: the chip defaults are kept (no quick-start, `RCOMP` or
  alert writes).
- Low-battery shutdown fires when SOC <= `BATTERY_SHUTDOWN_SOC` (5%) or
  voltage <= `BATTERY_SHUTDOWN_VOLTS` (3.30 V):
  - It needs `BATTERY_SHUTDOWN_SAMPLES` (3) consecutive valid low readings; a
    read error resets the count. At defaults that is about 90s.
  - Readings in the first `BATTERY_GRACE_PERIOD` (2m) after system boot are
    ignored. The grace period counts from boot, so a UI restart does not add
    another one.
  - It never shuts down while charging, or when the gauge is missing, failing
    or stale.
  - With `BATTERY_SHUTDOWN_DRY_RUN=true` it only logs that it would shut
    down.
- Shutdown runs `systemctl poweroff` and records the event in the shutdown
  log.
- Power-off permission is checked once at startup with
  `busctl ... CanPowerOff`. When it is not `yes`, the power-off button is
  disabled with the reason. The low-battery path still tries to power off
  and logs any error.

### Status Page
The Battery card shows:
- Voltage, SOC, trend (charging/discharging/idle), approximate time remaining
  until the shutdown threshold, level (`OK`, `Low`, `Critical`) and the last
  reading time.
- Degraded states: `Disabled`, `Not detected`, `Read error`, `Stale`, or
  `Waiting for first reading`. The page always loads.
- The auto-shutdown thresholds, with `(dry run)` when dry run is on.
- The last shutdown and a list of the last 10 (reason and time).
- A power-off form: `POST /power-off` with `confirm=yes` (checkbox).
  Cross-origin requests are rejected with `403 Forbidden` (same check as
  `/camera-config`). In dry run the request is only logged.

### Wiring
- The gauge measures the battery pack directly, before the UPS/boost module:
  pack `+`/`-` to the gauge `CELL+`/`CELL-` (or its JST battery input).
- `SDA` -> GPIO2 (pin 3), `SCL` -> GPIO3 (pin 5), `GND` -> Pi GND (pin 6).
- Check that the breakout's I2C pull-ups go to 3.3V (3.3V-compatible logic
  level), not to the battery voltage.

### Enable I2C
- `sudo raspi-config` -> Interface Options -> I2C, or add this line to
  `/boot/firmware/config.txt` (`/boot/config.txt` on older images) and
  reboot:
  ```
  dtparam=i2c_arm=on
  ```
- Verify the gauge is visible at address `0x36`:
  ```
  sudo apt install i2c-tools
  i2cdetect -y 1
  ```
  The table shows `36`.

### Permissions
- I2C: the service user must be in the `i2c` group. The unit adds it with
  `SupplementaryGroups=i2c`; to test outside systemd, add it to the user too:
  ```
  sudo usermod -aG i2c pi
  ```
- Power off: install a polkit rule so the service user may power off without
  root. `subject.user` must match `User=` in the systemd unit:
  ```
  // /etc/polkit-1/rules.d/50-raspicam-poweroff.rules
  polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.login1.power-off" ||
         action.id == "org.freedesktop.login1.power-off-multiple-sessions") &&
        subject.user == "pi") { // must match User= in the systemd unit
      return polkit.Result.YES;
    }
  });
  ```
- Check it as the service user:
  ```
  busctl call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanPowerOff
  ```
  It should print `s "yes"`. Restart the UI after changing the rule, since
  the check runs only at startup.

### State Directory
- `StateDirectory=raspicam` makes systemd create `/var/lib/raspicam`, owned
  by the service user, and set `STATE_DIRECTORY`. Override with `STATE_DIR`.
- Shutdown log: `<state dir>/shutdowns.jsonl`, one JSON object per line,
  keeping the last 10 shutdowns (low battery, manual, and dry-run events).
  Corrupt lines are skipped.
- Phase 2 battery history will use `<state dir>/battery`.

### Dry-Run Calibration
The default thresholds (5% / 3.30 V) are placeholders. Calibrate them with
one full discharge:
1. Set `Environment=BATTERY_SHUTDOWN_DRY_RUN=true`, then
   `sudo systemctl daemon-reload && sudo systemctl restart raspicam-ui`.
   The Battery card shows `(dry run)`.
2. Charge fully, unplug the charger, and keep the camera streaming (the
   real load).
3. Watch the log:
   ```
   journalctl -u raspicam-ui -f
   ```
   When the threshold is reached the log shows
   `battery: shutdown requested: reason=low_battery ... dry_run=true` with
   the voltage and SOC, followed by `battery: dry run, not powering off`.
   Reload the status page now and then to follow the Battery card readings.
4. Let it run until the UPS module cuts its output. Note the last voltage
   seen before the cut (the module's over-discharge cutoff is not
   documented).
5. Set `BATTERY_SHUTDOWN_VOLTS` at least 0.15 V above that cutoff voltage
   (margin for voltage sag under load), and adjust `BATTERY_SHUTDOWN_SOC` if
   needed.
6. Turn dry run off (`BATTERY_SHUTDOWN_DRY_RUN=false` or remove the line),
   then reload and restart the service.

### Hardware Notes
- Power tip: the 18650 UPS charge/boost module draws about 25mA on its own.
  Desoldering its output indicator LED saves more than 10mA (seller spec),
  about 2% while streaming and a larger share when idle or halted.
- After a low-battery shutdown the Pi stays halted even with the charger
  connected, because the UPS keeps 5V present. To restart, connect the
  charger and power-cycle the 5V output (switch). Exception: if the module's
  over-discharge protection has already cut the output, connecting the
  charger restores 5V and the Pi boots on its own.
- After a shutdown the halted Pi and the module keep draining the pack until
  the module's own cutoff trips; this is expected.
- Charge the cells in place: removing them resets the MAX17048 and its SOC
  estimate.

## Local Dev Notes
- MediaMTX API stub for local UI testing:
  - Start stub (single request, macOS): `./scripts/mediamtx-stub.sh | nc -l 9997`
  - Start stub (multiple requests, macOS): `while true; do ./scripts/mediamtx-stub.sh | nc -l 9997; done`
  - Single request exits after one UI load; multiple requests keeps the listener available across refreshes.
  - Set `MEDIAMTX_PATH_NAME` if you want a different path name.
- Run the UI locally:
  - `go run ./cmd/ui` (default `:8080`)
  - Optional: `UI_ADDR=:8081 go run ./cmd/ui`
- Run all tests:
  - `go test ./...`

## UI Features
- Status cards: system metrics, MediaMTX state, device info, network stats,
  battery.
- Battery: live readings, auto-shutdown thresholds, shutdown log, power-off
  button.
- Camera configuration: toggle `rpiCameraVFlip` and `rpiCameraHFlip`, set resolution, AWB, and sensor mode.
- Last update time uses `mediamtx.yml` modification time.

## Configuration Scope (TBD)
- MediaMTX stream settings (bitrate, resolution, codec settings).
- Network endpoints (RTSP/RTMP/WebRTC toggles).
- Optional UI refresh behavior.

## Open Questions
- What exact streaming protocol(s) are required (RTSP, RTMP, WebRTC)?
- Should the UI allow restarting MediaMTX or only edit config?
- What subset of config parameters should be user-editable?
- Should the UI persist config separately or edit MediaMTX config directly?
