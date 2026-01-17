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
  - Raspberry Pi model and camera model.
  - Memory and disk usage.
  - Network IP address.
  - Network input/output rate.
  - WiFi SSID, data rate, and link quality.
  - MediaMTX service status.
  - MediaMTX stream state if exposed by Control API.
- Provides configuration editing for a limited set of parameters (TBD).
- Uses MediaMTX Control API when available:
  - https://mediamtx.org/docs/usage/control-api
  - https://mediamtx.org/docs/references/control-api
  - https://github.com/bluenviron/mediamtx/blob/v1.15.6/api/openapi.yaml
- MediaMTX status fields shown in UI (when API enabled):
  - Service state (systemd).
  - API reachability.
  - Path readiness, source type, readers, and tracks.
- Reads device metrics using system tools:
  - `vcgencmd measure_temp`
  - `vcgencmd measure_volts`
  - `vcgencmd get_throttled`

### Focus Assist
- Provide a focus assist view to help pick the right camera focus setting.
- The output is a recommended `rpiCameraLensPosition` value for MediaMTX config.
- Show a live snapshot or short preview loop from the active camera stream.
- Allow users to choose a focus region (center/thirds/full frame) that the assist uses.
- Surface a sharpness score for the selected region and highlight the best score seen.
- Offer step controls for focus distance with a recommended value based on scores.
- Clearly indicate when autofocus is enabled and disable manual focus steps in that mode.
- If focus control is unavailable, show a degraded-mode message with guidance.
- Provide a simple "reset to auto" action to restore autofocus defaults.
- Provide an image inspection strip (thumbnails) for each captured frame so the user
  can compare sharpness and manually pick the lens position to save.
- All focus assist actions (preview, capture, inspect, apply) must be available in
  the web UI.
- Any long-running actions (scan, capture, apply) must show progress and status
  so the user knows the operation is still running, including auto-refresh during scans.
- Provide a full-resolution view of captured stills so users can inspect fine detail.
- Capture logs should be available on demand without overwhelming the page by default.
- Provide a way to stop/start the MediaMTX service from the web UI to release the camera
  during focus assist scans.
- Captured stills must use the current camera configuration for sensor mode and flips.
- Feasibility notes:
  - Real-time scoring is only feasible if the Pi camera stack exposes a low-latency
    frame source plus per-frame focus metadata or supports quick successive snapshots.
  - If real-time metrics are not exposed, the fallback is a step-and-capture flow:
    take a still frame per lens step, compute sharpness, and recommend the best
    `rpiCameraLensPosition`.

### Focus Assist Technical Design (Draft)
- Inputs
  - Current camera mode (`rpiCameraAfMode`) and lens position (`rpiCameraLensPosition`).
  - Live preview frames sourced from the active stream when feasible.
  - User-selected focus region (center/thirds/full frame).
- Processing
  - Compute a sharpness score per frame for the selected region (e.g., variance of
    Laplacian) and keep the best score with its lens position.
  - If autofocus is enabled, render focus metrics as read-only and prompt to switch
    to manual to run focus assist.
  - If lens control is unavailable, surface degraded-mode guidance and disable steps.
- Output
  - Recommended `rpiCameraLensPosition` value with a short rationale (best score).
  - Snapshot stills per lens step with a selectable gallery to override the
    recommended value based on visual inspection.

### Focus Assist Implementation Next Steps
1. Inventory the Pi camera stack capabilities for real-time frames and focus metadata
   (libcamera, rpicam-vid, MediaMTX pipeline).
2. Decide the capture path:
   - Real-time: tap a low-res preview stream for scoring.
   - Fallback: step-and-capture stills per lens position.
3. Add a small image scoring helper (server-side) with strict bounds on image size.
4. Extend UI config form to trigger focus assist and display score history.
5. Persist recommended `rpiCameraLensPosition` into `mediamtx.yml` when accepted.
6. Add unit tests for scoring on fixture images and for lens step validation.
7. Add a thumbnail gallery with lens position labels and a "pick this image" action.

### Focus Assist UX Proposal
#### User Flows
- Happy path (manual focus):
  1. Open Camera Config in the web UI and select "Focus Assist."
  2. Choose a focus region and step range, then start a scan.
  3. Review score chart and thumbnail strip; click a thumbnail to inspect larger.
  4. Accept recommended lens position or choose a specific thumbnail to apply.
  5. Confirm and save to `mediamtx.yml`.
- Autofocus active:
  1. Open Focus Assist and see autofocus status banner.
  2. Optionally switch to manual mode to run scan.
  3. Resume autofocus via "reset to auto" if desired.
- Degraded mode:
  1. Open Focus Assist, see unavailable lens control message.
  2. View guidance (enable control API or change camera mode).
  3. Use manual input field as fallback if still needed.

#### Navigation
- Primary nav: Status | Camera Config | Focus Assist.
- Focus Assist entry point is also available inside Camera Config near lens controls.
- After a scan, a "Apply to config" action stays in the Focus Assist view.

#### UI Wireframes (ASCII)
Focus Assist page:
```
-------------------------------------------------------------
 Focus Assist
 [Status: Manual]  [Reset to auto]

 Region: (o) Center ( ) Thirds ( ) Full
 Range:  [Start 0.0] [End 3.0] [Step 0.1]  [Start Scan]

 Best score: 124.6 at lens 1.2  [Apply Recommended]

 Score chart:  (simple line plot or table)

 Thumbnails:
 [1.0] [1.1] [1.2*] [1.3] [1.4]
 (click thumbnail to preview)

 Preview:
 +-----------------------------+
 |  selected frame image       |
 +-----------------------------+
 [Apply Selected Lens Position]
```

Camera Config entry point:
```
-------------------------------------------------------------
 Camera Config
 AF Mode: [manual v]
 Lens Position: [1.2]  [Infinity]
 [Focus Assist]  (opens Focus Assist page)
```

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
