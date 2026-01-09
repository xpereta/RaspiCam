# Product Requirements Document (PRD)

## Product Name
Pi Zero 2 W Streaming Stack

## Overview
A lightweight streaming system for Raspberry Pi Zero 2 W using a Pi Camera Module V3. MediaMTX provides the stream, while a local web UI offers health status and limited configuration. Both services start automatically on boot.

## Objectives
- Deliver a reliable, low-resource stream from the camera module.
- Provide a simple, accessible browser UI for status and configuration.
- Minimize power usage and CPU load.

## Target Users
- Makers and hobbyists deploying a low-cost camera stream.
- Developers needing a simple, self-hosted streaming endpoint.

## Success Criteria
- MediaMTX and UI start automatically on boot.
- Stream is available within 30 seconds of boot.
- UI loads in under 2 seconds on local network.
- CPU usage stays within acceptable limits for Pi Zero 2 W.

## Functional Requirements

### Streaming Component (MediaMTX)
- Runs on Raspberry Pi OS 32-bit.
- Starts on boot.
- Streams camera feed via configured protocol(s) (TBD: RTSP/RTMP/WebRTC).
- Configuration stored in a local file.
- Install and boot setup follow MediaMTX docs:
  - https://mediamtx.org/docs/kickoff/install
  - https://mediamtx.org/docs/usage/start-on-boot
- Expected locations:
  - `/usr/local/bin/mediamtx`
  - `/usr/local/etc/mediamtx.yml`
  - `/recordings/cam`
- Pi Camera Module V3 configuration in `/usr/local/etc/mediamtx.yml`:
  ```
  paths:
    cam:
      source: rpiCamera
      rpiCameraWidth: 1280
      rpiCameraHeight: 720
      #rpiCameraVFlip: true
      #rpiCameraHFlip: true
      #rpiCameraBitrate: 4000000
      rpiCameraAfMode: "continuous"
      #rpiCameraTextOverlayEnable: true
      #rpiCameraTextOverlay: "cam1"
      # For european grid blink frequency
      rpiCameraFlickerPeriod: 20000
      rpiCameraAfSpeed: "fast"
      rpiCameraMetering: "matrix"

      # Color & image tuning
      rpiCameraSaturation: 1.25
      rpiCameraContrast: 1.15
      rpiCameraSharpness: 1.05

      rpiCameraAWB: "indoor"
  ```
- Enable Control API only if needed (local access only):
  ```
  api: yes
  ```
- Enable recording:
  ```
  record: yes
  ```
  Recordings stored at `/recordings/cam`.
- Stream access (same network as the Pi):
  - `http://zero2:8889/cam/`

### Status and Configuration UI
- Runs on the Pi and starts on boot.
- Browser-accessible UI (no client install).
- Shows:
  - System uptime.
  - CPU load and temperature.
  - CPU usage percentage and voltage.
  - Throttling status (from `vcgencmd get_throttled`).
  - Memory and disk usage.
  - Network IP address.
  - MediaMTX service status.
  - MediaMTX stream state if exposed by Control API.
- Provides configuration editing for a limited set of parameters (TBD).
- Uses MediaMTX Control API when available:
  - https://mediamtx.org/docs/usage/control-api
  - https://mediamtx.org/docs/references/control-api
  - https://github.com/bluenviron/mediamtx/blob/v1.15.6/api/openapi.yaml
- Reads device metrics using system tools:
  - `vcgencmd measure_temp`
  - `vcgencmd measure_volts`
  - `vcgencmd get_throttled`

### UI Technical Stack (Decision)
- Language/runtime: Go 1.22+.
- Web server: Go standard library (`net/http`, `html/template`).
- UI: server-rendered HTML with no auto-refresh.
- Packaging: single static binary; no external runtime.
- Rationale:
  - Low resource usage on Pi Zero 2 W with a single static binary.
  - Simple deployment and maintenance (no runtime or heavy deps).
  - Good developer experience with standard library HTTP and templates.

## Non-Functional Requirements
- Low CPU and memory footprint (optimized for Pi Zero 2 W).
- Minimal dependencies; prefer static binaries or lightweight runtimes.
- Operate without internet access (local-only).
- Safe defaults; configuration changes validated before applying.

## Operational Notes
### Copy recordings
With checksum (slow but safe):
```
rsync -av --progress --partial --inplace --checksum --stats xavi@zero2:/recordings .
```

Without checksum (fast, good with stable connection):
```
rsync -av --progress --partial --inplace --stats xavi@zero2:/recordings .
```

Notes:
- Safe to copy while a stream is playing.

## Constraints
- 32-bit Raspberry Pi OS.
- Limited RAM and CPU (512 MB RAM on Pi Zero 2 W).
- Power and heat constraints.

## Out of Scope
- Cloud-based management.
- Multi-user accounts or authentication (initial version).
- Persistent recording or storage.
- High-availability/failover.

## Risks and Mitigations
- Risk: CPU overload from streaming and UI.
  - Mitigation: use low-overhead stack and avoid background refresh.
- Risk: Misconfiguration breaks stream.
  - Mitigation: validate config and support rollback to last-known-good.
- Risk: Control API unavailable or unstable.
  - Mitigation: fallback to systemd status checks.

## Open Questions
- Which streaming protocol(s) are required?
- What exact parameters should be editable in UI?
- Should UI expose start/stop/restart controls for MediaMTX?
- Will the UI require any authentication later?

## Milestones (Draft)
- M1: System documentation and PRD complete.
- M2: MediaMTX setup on Pi with auto-start.
- M3: UI prototype with health status.
- M4: Config editing wired to MediaMTX.
