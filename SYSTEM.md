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

### Finding the Cutoff Voltage (Calibration)
The default thresholds (5% / 3.30 V) are placeholders. The UPS module's
over-discharge cutoff is not documented, so measure the voltage at which the
Pi loses power, then set `BATTERY_SHUTDOWN_VOLTS` with a margin above it.
Whatever trips first (the module's cell protection, or the boost converter
no longer holding 5 V under load) is the number that matters.

What you need:
- The Pi with the gauge wired and detected (Battery card shows a voltage).
- A second machine with SSH access to the Pi, kept awake for the whole run.
- Method A: fully charged cells and a few hours. Method B: an adjustable
  bench supply (0-5 V, current limit) instead of the cells.

#### Prepare
1. Add to the unit (`sudo systemctl edit raspicam-ui` or the unit file):
   ```
   Environment=BATTERY_SHUTDOWN_DRY_RUN=true
   Environment=BATTERY_POLL_INTERVAL=10s
   ```
   Dry run logs every reading and never powers off. 10s is the minimum poll
   interval and gives a finer reading near the cutoff (the 30s default can
   miss up to 30s of discharge).
2. `sudo systemctl daemon-reload && sudo systemctl restart raspicam-ui`.
   The Battery card shows `(dry run)`.
3. On the second machine, record the log to a local file, so it survives
   the Pi losing power (journald on the Pi may not have synced its last
   lines to disk, so `journalctl -b -1` after the next boot can miss them):
   ```
   ssh pi@<pi-host> 'journalctl -u raspicam-ui -f -o short-iso' | tee cutoff-$(date +%Y%m%d).log
   ```
   Each poll appears as
   `battery: reading volts=3.912 soc=64.2 crate=-11.85 trend=discharging level=OK`.

#### Method A: full discharge (real load, recommended)
1. Charge fully with the Pi off, then boot it and unplug the charger.
2. Keep the camera streaming (open the stream in a player) so the load
   matches use in the train. Note the start time.
3. Wait. Around the configured threshold the log also shows
   `battery: shutdown requested: reason=low_battery ... dry_run=true` and
   `battery: dry run, not powering off`; this is expected, keep going.
4. When the module cuts its output the SSH session freezes or drops. Note
   the time.
5. Read the last readings from the local file:
   ```
   grep 'battery: reading' cutoff-*.log | tail -n 5
   ```
   The last `volts=` value is the cutoff voltage under load (accurate to one
   poll interval). Also note the last `soc=` and the run time from step 2.
6. Optional: measure the cells with a multimeter right after the cutoff.
   They rebound once unloaded, so expect a higher value than the log; use
   the logged value for the threshold.
7. Recharge (connecting the charger restores 5V after a protection trip).

#### Method B: bench supply (fast, no discharge)
1. Remove the cells and connect a bench supply to the module's battery
   terminals (B+/B-), set to 4.0 V with a current limit of about 2 A. Do not
   connect a USB charger to the module while the bench supply is attached.
   Removing the cells resets the MAX17048, so SOC values in this test are
   meaningless; only `volts=` matters.
2. Boot the Pi and start the camera stream.
3. Lower the supply in 0.05 V steps, waiting at least two polls (20s) per
   step. Below about 3.4 V use 0.02 V steps.
4. Note the supply voltage at which the Pi loses power, and compare it with
   the last `volts=` in the log (the difference is the wiring drop between
   supply and gauge).
5. The supply holds its voltage under load, while real cells sag more, so
   treat this result as optimistic and confirm it with Method A when you can.

#### Set the threshold
1. `BATTERY_SHUTDOWN_VOLTS` = cutoff voltage + at least 0.15 V (margin for
   load spikes and cell aging). Example: cutoff at 3.05 V -> 3.20 V. Values
   must stay within the valid range in the table above.
2. If the last SOC before the cutoff was well above 0%, raise
   `BATTERY_SHUTDOWN_SOC` too (the gauge's SOC is less reliable near empty
   on a new pack; it improves after a few full cycles).
3. Remove the two calibration lines (or set
   `BATTERY_SHUTDOWN_DRY_RUN=false`), add the new thresholds, then reload and
   restart the service.

#### Verify (real shutdown)
1. Run one more discharge with dry run off and the camera streaming, still
   logging over SSH.
2. Expected: `battery: shutdown requested: reason=low_battery` and a clean
   power-off before the module cuts the output. After recharging and
   booting, the Battery card's shutdown log lists a `low_battery` entry with
   the voltage and SOC.
3. If the module still cuts first, raise `BATTERY_SHUTDOWN_VOLTS` by 0.05 V
   and repeat.

Record the result (date, cells, cutoff voltage, SOC at cutoff, run time,
chosen thresholds) so later runs can show cell aging.

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
