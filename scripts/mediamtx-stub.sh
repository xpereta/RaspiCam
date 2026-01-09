#!/usr/bin/env sh
# Simple stub for MediaMTX Control API (paths/get) used for local UI testing.

PORT="${PORT:-9997}"
PATH_NAME="${MEDIAMTX_PATH_NAME:-cam}"

body=$(cat <<JSON
{
  "name": "${PATH_NAME}",
  "confName": "${PATH_NAME}",
  "ready": true,
  "source": { "type": "rpiCameraSource" },
  "tracks": ["video"],
  "readers": [{ "type": "rtspSession" }]
}
JSON
)

printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %s\r\n\r\n%s' \
  "$(printf '%s' "$body" | wc -c | tr -d ' ')" \
  "$body"

# Serve a single request.
# Usage (macOS): ./scripts/mediamtx-stub.sh | nc -l 9997
