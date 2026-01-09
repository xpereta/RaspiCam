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

## Notes
- Camera config changes edit `mediamtx.yml`. MediaMTX auto-restarts on file changes.
- The UI shows the last update time using the file modification time of `mediamtx.yml`.

## UI Endpoints
- `GET /` status UI
- `POST /camera-config` update camera flip settings

## Testing
```
go test ./...
```
