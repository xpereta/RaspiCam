# Contributing

## Workflow
- Create a short-lived branch from `main`.
- Keep changes focused and small when possible.
- Update docs when behavior changes.
- Run formatting and tests before opening a PR.

## Commit Messages
Use Conventional Commits:
- `feat: ...`
- `fix: ...`
- `docs: ...`
- `chore: ...`
- `refactor: ...`

Examples:
- `feat: add cpu telemetry endpoint`
- `docs: describe mediamtx setup`

## Testing
- Run `gofmt ./...` and `go test ./...` when Go code is introduced.
- If tests cannot be run, note it in the PR description.

## Pull Requests
- Fill out the PR template.
- Include steps to verify the change.
- Call out risks or limitations.
- When using `gh pr create`, provide the body via `--body-file` or a heredoc to avoid literal `\n` sequences.
  - Example: `gh pr create --title "..." --body-file pr-body.md`
  - Example: `gh pr create --title "..." --body "$(cat <<'EOF'
    # Summary
    ...
    EOF
    )"`
