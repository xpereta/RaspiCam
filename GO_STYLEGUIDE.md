# Go Style Guide (Project)

This guide complements the official Go Code Review Comments.

References:
- https://go.dev/wiki/CodeReviewComments

## Conventions
- Format with `gofmt`.
- Use standard library first; add deps only with clear need.
- Keep functions small and single-purpose.
- Prefer explicit error handling; add context with `fmt.Errorf("...: %w", err)`.
- Avoid global state; inject dependencies via constructors.
- Use `context.Context` for long-running or cancelable operations.
- Minimize allocations and copies; avoid large buffers in memory.
- Keep package APIs minimal and cohesive.

## Project-Specific
- Parse `vcgencmd` outputs with strict, tested parsing functions.
- Handle missing `vcgencmd` as degraded mode with clear UI output.
- Keep HTTP handlers thin; move logic to internal packages.
