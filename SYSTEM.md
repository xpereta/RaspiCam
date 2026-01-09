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
  - A form to edit selected configuration parameters (TBD).
- Uses MediaMTX Control API for health/state where applicable:
  - https://mediamtx.org/docs/usage/control-api
  - https://mediamtx.org/docs/references/control-api
  - OpenAPI spec: https://github.com/bluenviron/mediamtx/blob/v1.15.6/api/openapi.yaml
- Reads device metrics locally via:
  - `vcgencmd measure_temp`
  - `vcgencmd measure_volts`
  - `vcgencmd get_throttled`

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
After=network-online.target
Wants=network-online.target

[Service]
Environment=UI_ADDR=:8080
Environment=MEDIAMTX_API_URL=http://127.0.0.1:9997
Environment=MEDIAMTX_PATH_NAME=cam
ExecStart=/usr/local/bin/raspicam-ui

[Install]
WantedBy=multi-user.target
EOF
```
  - Optional: run as a non-root user (match your Pi setup):
    ```
    # Add to [Service]
    User=pi
    Group=pi
    ```
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

## Configuration Scope (TBD)
- MediaMTX stream settings (bitrate, resolution, codec settings).
- Network endpoints (RTSP/RTMP/WebRTC toggles).
- Optional UI refresh behavior.

## Open Questions
- What exact streaming protocol(s) are required (RTSP, RTMP, WebRTC)?
- Should the UI allow restarting MediaMTX or only edit config?
- What subset of config parameters should be user-editable?
- Should the UI persist config separately or edit MediaMTX config directly?
