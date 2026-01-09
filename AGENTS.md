# Agent Guidance

## Language and Runtime
- Go 1.22+.
- Standard library only unless a dependency is clearly justified.

## Coding Standards
- Follow Go Code Review Comments: https://go.dev/wiki/CodeReviewComments
- Run `gofmt` on all Go files.
- Prefer explicit error handling; wrap with context using `fmt.Errorf("...: %w", err)`.
- Avoid global state; pass dependencies via constructors.
- Use context-aware functions for long-running calls (HTTP handlers, polling).
- Keep allocations low; avoid unnecessary copies and large buffers.
- Favor small, focused packages with clear responsibilities.

## Architecture
- `cmd/ui`: main entrypoint and wiring.
- `internal/metrics`: system metrics collection (CPU usage, temp, volts, throttling).
- `internal/mediamtx`: Control API client (if enabled).
- `internal/web`: HTTP handlers, templates, and routing.
- `internal/config`: config loading, validation, and persistence.
- `internal/system`: service and OS helpers (systemd status, IP discovery).

## Patterns
- Parse `vcgencmd` output using strict, tested parsing functions.
- Treat missing `vcgencmd` as a degraded mode with clear UI messaging.
- Use HTML templates for server rendering; no auto-refresh by default.

## Testing
- Unit tests for parsing (`vcgencmd` outputs, `/proc` CPU stats).
- Avoid integration tests that require Pi hardware unless explicitly requested.

## Documentation
- Keep `SYSTEM.md` and `PRD.md` updated when behavior changes.
- Use ASCII in docs and comments unless Unicode is required.
