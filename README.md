# RaspiCam

Lightweight status UI and MediaMTX helpers for a Raspberry Pi Zero 2 W camera setup.

## What This Is
- MediaMTX runs the camera stream.
- A small Go web UI shows health and MediaMTX status.
- The UI can edit two camera config flags in `mediamtx.yml`:
  - `rpiCameraVFlip`
  - `rpiCameraHFlip`

## Quick Start (Local)
```
go run ./cmd/ui
```
Open `http://localhost:8080`.

## Quick Start (Pi)
From a blank microSD card: see [RPI_SETUP.md](RPI_SETUP.md). In short, flash
Raspberry Pi OS Lite, then on the Pi:
```
git clone https://github.com/xpereta/RaspiCam.git ~/RaspiCam
cd ~/RaspiCam
sudo ./scripts/setup-pi.sh
```
The script installs Go and MediaMTX, writes the MediaMTX config, builds the
UI, and installs and starts both systemd services. Re-run it after `git pull`
to deploy updates.

## Environment Variables
- `UI_ADDR` (default `:8080`)
- `MEDIAMTX_API_URL` (default `http://127.0.0.1:9997`)
- `MEDIAMTX_PATH_NAME` (default `cam`)
- `MEDIAMTX_CONFIG_PATH` (default `/usr/local/etc/mediamtx.yml`)

## Notes
- Camera config changes edit `mediamtx.yml`. MediaMTX auto-restarts on file changes.
- The UI shows the last update time using the file modification time of `mediamtx.yml`.

## UI Endpoints
- `GET /` status UI
- `POST /camera-config` update camera flip settings; cross-origin requests are rejected with `403 Forbidden` (Sec-Fetch-Site/Origin check)

## Testing
```
go test ./...
```
