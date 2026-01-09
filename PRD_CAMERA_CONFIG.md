# PRD: Camera Configuration Card (VFlip/HFlip)

## Overview
Add a new "Camera configuration" card to the UI that allows editing two MediaMTX camera path settings:
- `rpiCameraVFlip`
- `rpiCameraHFlip`

This feature will focus only on the `cam` path and will not introduce broader path configuration editing.

## Goals
- Provide a simple, safe UI to toggle vertical and horizontal flip settings.
- Keep the UI lightweight and consistent with current status layout.
- Apply changes reliably to the MediaMTX configuration.

## Non-Goals
- Editing any other camera parameters.
- Editing any paths other than `cam`.
- Real-time preview or auto-refresh behavior.

## User Stories
- As an operator, I can toggle vertical flip to correct an upside-down camera.
- As an operator, I can toggle horizontal flip to mirror the camera image.

## Functional Requirements

### UI
- Add a "Camera configuration" card below the status sections.
- Two toggles (or checkboxes) for:
  - `rpiCameraVFlip`
  - `rpiCameraHFlip`
- Both values are boolean; the UI only allows toggling `true`/`false`.
- Show current values based on the active MediaMTX configuration.
- Provide a "Save" action to apply changes.
- Display success or error feedback after save.
- Show last update time for the camera configuration.

### Configuration Behavior
- Only affect the `cam` path in `/usr/local/etc/mediamtx.yml`.
- Read current values from the config file at load.
- Write updates to the same config file.
- Validate config file update before applying.
- MediaMTX auto-restarts on config file changes; no manual restart required.
- Control API updates are not persistent across restarts; the file is the source of truth.

## Non-Functional Requirements
- Avoid heavy dependencies or background polling.
- Changes should be atomic and recoverable (keep a backup of previous config).
- Failures should not break existing configuration.

## Constraints
- Runs on Raspberry Pi OS 32-bit.
- Uses local MediaMTX configuration at `/usr/local/etc/mediamtx.yml`.
- No authentication for now (local network only).

## Risks and Mitigations
- Risk: Incorrect YAML editing corrupts config.
  - Mitigation: parse/serialize safely and keep a backup.
- Risk: Restarting MediaMTX interrupts the stream.
  - Mitigation: confirm user intent and show impact.

## Open Questions
- Should the UI show the last update time and who/what changed it?
- Should we support a "revert" button to last-known-good config?
- Explore camera focus options (infinity focus and hyperfocal) and how to configure them.

## Acceptance Criteria
- UI displays current `rpiCameraVFlip` and `rpiCameraHFlip` values for `cam`.
- Toggling and saving updates the config file correctly.
- MediaMTX reflects the changes after apply (restart/reload).
- Errors are shown clearly and do not leave config in a broken state.
