#!/usr/bin/env bash
# setup-pi.sh - install and configure the RaspiCam stack on Raspberry Pi OS.
#
# Installs Go, MediaMTX, builds the RaspiCam UI from this checkout, writes
# the MediaMTX config and systemd units, and starts both services.
#
# Run it on the Pi, from a clone of this repository:
#   sudo ./scripts/setup-pi.sh
#
# Safe to re-run: existing MediaMTX config is kept, Go and MediaMTX are only
# downloaded when the installed version differs, and the UI is rebuilt from
# the current checkout every time (use this to deploy updates).
#
# See RPI_SETUP.md for the full from-scratch guide.

set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults (override with environment variables or flags).
# ---------------------------------------------------------------------------
GO_VERSION="${GO_VERSION:-1.27.4}"
MEDIAMTX_VERSION="${MEDIAMTX_VERSION:-v1.21.3}"
RASPICAM_USER="${RASPICAM_USER:-${SUDO_USER:-}}"
UI_ADDR="${UI_ADDR:-:8080}"
MEDIAMTX_PATH_NAME="cam" # the UI edits paths.cam; keep in sync with internal/config
MEDIAMTX_CONF_DIR="/usr/local/etc/mediamtx"
MEDIAMTX_CONF="${MEDIAMTX_CONF_DIR}/mediamtx.yml"
SKIP_APT=0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
	cat <<EOF
Usage: sudo $0 [options]

Options:
  --user NAME              User that runs the UI and owns the MediaMTX config
                           (default: the user that invoked sudo: ${RASPICAM_USER:-unset})
  --go-version X.Y.Z       Go toolchain version (default: ${GO_VERSION})
  --mediamtx-version vX.Y.Z
                           MediaMTX release (default: ${MEDIAMTX_VERSION})
  --ui-addr ADDR           Listen address for the UI (default: ${UI_ADDR})
  --skip-apt               Do not run apt-get update/install
  -h, --help               Show this help

Environment variables GO_VERSION, MEDIAMTX_VERSION, RASPICAM_USER and UI_ADDR
are honoured as defaults.
EOF
}

log() { printf '\n==> %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
warn() { printf '    WARNING: %s\n' "$*" >&2; }
die() {
	printf 'ERROR: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--user)
		RASPICAM_USER="${2:?--user needs a value}"
		shift 2
		;;
	--go-version)
		GO_VERSION="${2:?--go-version needs a value}"
		shift 2
		;;
	--mediamtx-version)
		MEDIAMTX_VERSION="${2:?--mediamtx-version needs a value}"
		shift 2
		;;
	--ui-addr)
		UI_ADDR="${2:?--ui-addr needs a value}"
		shift 2
		;;
	--skip-apt)
		SKIP_APT=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown option: $1"
		;;
	esac
done

GO_VERSION="${GO_VERSION#go}"
[[ "${MEDIAMTX_VERSION}" == v* ]] || MEDIAMTX_VERSION="v${MEDIAMTX_VERSION}"

# ---------------------------------------------------------------------------
# Preflight.
# ---------------------------------------------------------------------------
log "Preflight checks"

[[ "${EUID}" -eq 0 ]] || die "run as root: sudo $0"
[[ -f "${REPO_DIR}/go.mod" && -d "${REPO_DIR}/cmd/ui" ]] ||
	die "${REPO_DIR} does not look like the RaspiCam repository"

[[ -n "${RASPICAM_USER}" ]] || die "cannot tell which user to run as; pass --user NAME"
[[ "${RASPICAM_USER}" != "root" ]] || die "refusing to run the UI as root; pass --user NAME"
id "${RASPICAM_USER}" >/dev/null 2>&1 || die "user ${RASPICAM_USER} does not exist"
RASPICAM_GROUP="$(id -gn "${RASPICAM_USER}")"
RASPICAM_HOME="$(getent passwd "${RASPICAM_USER}" | cut -d: -f6)"

command -v dpkg >/dev/null 2>&1 || die "dpkg not found; this script targets Raspberry Pi OS"
command -v systemctl >/dev/null 2>&1 || die "systemctl not found"

# Use the userland architecture, not `uname -m`: 32-bit Raspberry Pi OS can
# boot a 64-bit kernel, in which case uname reports aarch64 on an armhf system.
DEB_ARCH="$(dpkg --print-architecture)"
case "${DEB_ARCH}" in
arm64)
	GO_ARCH="arm64"
	MTX_ARCH="arm64"
	;;
armhf)
	GO_ARCH="armv6l"
	# Pi Zero / Pi 1 have an ARMv6 CPU; everything newer is ARMv7 or later.
	if grep -qE '^CPU architecture:\s*([7-9]|[1-9][0-9])' /proc/cpuinfo; then
		MTX_ARCH="armv7"
	else
		MTX_ARCH="armv6"
	fi
	;;
*)
	die "unsupported architecture ${DEB_ARCH} (expected armhf or arm64)"
	;;
esac

if [[ -r /etc/os-release ]]; then
	# shellcheck disable=SC1091
	. /etc/os-release
	info "OS:           ${PRETTY_NAME:-unknown}"
	case "${VERSION_CODENAME:-}" in
	bookworm | trixie) ;;
	*) warn "MediaMTX's Pi camera support targets Bookworm or Trixie; found ${VERSION_CODENAME:-unknown}" ;;
	esac
fi
info "Architecture: ${DEB_ARCH} (Go ${GO_ARCH}, MediaMTX ${MTX_ARCH})"
info "Model:        $(tr -d '\0' </proc/device-tree/model 2>/dev/null || echo unknown)"
info "Service user: ${RASPICAM_USER}:${RASPICAM_GROUP}"
info "Repository:   ${REPO_DIR}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

# ---------------------------------------------------------------------------
# Packages.
# ---------------------------------------------------------------------------
if [[ "${SKIP_APT}" -eq 0 ]]; then
	log "Installing OS packages"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update
	# curl/ca-certificates: downloads. git: repo updates. iw: WiFi details in
	# the UI. rpicam-apps-lite: camera self-test (rpicam-hello).
	apt-get install -y --no-install-recommends ca-certificates curl git iw
	apt-get install -y --no-install-recommends rpicam-apps-lite ||
		warn "rpicam-apps-lite not available; camera self-test will be skipped"
else
	log "Skipping apt (--skip-apt)"
fi
command -v curl >/dev/null 2>&1 || die "curl is required"

if ! command -v vcgencmd >/dev/null 2>&1; then
	warn "vcgencmd not found; the UI will run in degraded mode (no temp/volts/throttle)"
fi

# ---------------------------------------------------------------------------
# Go toolchain (official tarball; distro packages can be older than go.mod).
# ---------------------------------------------------------------------------
log "Go ${GO_VERSION}"
GO_BIN="/usr/local/go/bin/go"
current_go=""
if [[ -x "${GO_BIN}" ]]; then
	current_go="$("${GO_BIN}" env GOVERSION 2>/dev/null || true)"
fi
if [[ "${current_go}" == "go${GO_VERSION}" ]]; then
	info "already installed"
else
	go_tar="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
	info "downloading ${go_tar}"
	curl -fsSL -o "${TMP_DIR}/${go_tar}" "https://dl.google.com/go/${go_tar}"
	go_sum="$(curl -fsSL "https://dl.google.com/go/${go_tar}.sha256")"
	echo "${go_sum}  ${TMP_DIR}/${go_tar}" | sha256sum -c --quiet - ||
		die "checksum mismatch for ${go_tar}"
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "${TMP_DIR}/${go_tar}"
	info "installed $("${GO_BIN}" env GOVERSION)"
fi
cat >/etc/profile.d/golang.sh <<'EOF'
# Added by RaspiCam setup-pi.sh
case ":${PATH}:" in
*:/usr/local/go/bin:*) ;;
*) export PATH="${PATH}:/usr/local/go/bin" ;;
esac
EOF

# ---------------------------------------------------------------------------
# MediaMTX binary.
# ---------------------------------------------------------------------------
log "MediaMTX ${MEDIAMTX_VERSION}"
MTX_BIN="/usr/local/bin/mediamtx"
current_mtx=""
if [[ -x "${MTX_BIN}" ]]; then
	current_mtx="$("${MTX_BIN}" --version 2>/dev/null | tr -d '[:space:]' || true)"
fi
MTX_TAR="mediamtx_${MEDIAMTX_VERSION}_linux_${MTX_ARCH}.tar.gz"
MTX_URL="https://github.com/bluenviron/mediamtx/releases/download/${MEDIAMTX_VERSION}"
mtx_fetched=0
fetch_mediamtx() {
	[[ "${mtx_fetched}" -eq 1 ]] && return 0
	info "downloading ${MTX_TAR}"
	curl -fsSL -o "${TMP_DIR}/${MTX_TAR}" "${MTX_URL}/${MTX_TAR}"
	curl -fsSL -o "${TMP_DIR}/checksums.sha256" "${MTX_URL}/checksums.sha256"
	(cd "${TMP_DIR}" && grep " \*\?${MTX_TAR}\$" checksums.sha256 | sed 's/ \*/  /' | sha256sum -c --quiet -) ||
		die "checksum mismatch for ${MTX_TAR}"
	mkdir -p "${TMP_DIR}/mediamtx"
	tar -C "${TMP_DIR}/mediamtx" -xzf "${TMP_DIR}/${MTX_TAR}"
	mtx_fetched=1
}
mtx_binary_changed=0
if [[ "${current_mtx}" == "${MEDIAMTX_VERSION}" ]]; then
	info "already installed"
else
	fetch_mediamtx
	install -m 0755 "${TMP_DIR}/mediamtx/mediamtx" "${MTX_BIN}"
	mtx_binary_changed=1
	info "installed $("${MTX_BIN}" --version)"
fi

# ---------------------------------------------------------------------------
# MediaMTX config.
#
# The UI saves mediamtx.yml by writing a temp file in the same directory and
# renaming it over the original (plus a timestamped backup), so the service
# user needs write access to the directory, not only the file. A dedicated
# directory keeps that permission narrow.
# ---------------------------------------------------------------------------
log "MediaMTX config (${MEDIAMTX_CONF})"
install -d -m 0755 -o "${RASPICAM_USER}" -g "${RASPICAM_GROUP}" "${MEDIAMTX_CONF_DIR}"
if [[ -f "${MEDIAMTX_CONF}" ]]; then
	info "keeping existing config"
else
	fetch_mediamtx
	src="${TMP_DIR}/mediamtx/mediamtx.yml"
	# Keep the pristine upstream file next to ours for reference.
	install -m 0644 -o "${RASPICAM_USER}" -g "${RASPICAM_GROUP}" "${src}" "${MEDIAMTX_CONF_DIR}/mediamtx.yml.dist"

	grep -q '^api: false$' "${src}" || die "unexpected upstream config: 'api: false' not found"
	grep -q '^apiAddress: :9997$' "${src}" || die "unexpected upstream config: 'apiAddress: :9997' not found"
	grep -q '^paths:$' "${src}" || die "unexpected upstream config: 'paths:' not found"

	# Enable the Control API on localhost only, and replace the `paths`
	# section (always the last one) with the camera path.
	sed -e 's/^api: false$/api: true/' \
		-e 's/^apiAddress: :9997$/apiAddress: 127.0.0.1:9997/' \
		-e '/^paths:$/,$d' \
		"${src}" >"${TMP_DIR}/mediamtx.yml"
	cat >>"${TMP_DIR}/mediamtx.yml" <<EOF
paths:
  ${MEDIAMTX_PATH_NAME}:
    source: rpiCamera
EOF
	install -m 0644 -o "${RASPICAM_USER}" -g "${RASPICAM_GROUP}" "${TMP_DIR}/mediamtx.yml" "${MEDIAMTX_CONF}"
	info "written (upstream defaults + api on 127.0.0.1:9997 + paths.${MEDIAMTX_PATH_NAME})"
fi
# Make sure ownership is right on re-runs (e.g. after a user change).
chown "${RASPICAM_USER}:${RASPICAM_GROUP}" "${MEDIAMTX_CONF}"

# Legacy location from earlier manual setups; MediaMTX would not read it now.
if [[ -f /usr/local/etc/mediamtx.yml ]]; then
	warn "/usr/local/etc/mediamtx.yml exists but is no longer used; the services use ${MEDIAMTX_CONF}"
fi

# ---------------------------------------------------------------------------
# Build and install the UI.
# ---------------------------------------------------------------------------
log "Building raspicam-ui"
# Build as the service user so the repo and Go caches stay user-owned.
# -p 1 keeps peak memory low on a 512 MB Pi Zero 2 W.
BUILD_DIR="${TMP_DIR}/build"
install -d -m 0700 -o "${RASPICAM_USER}" -g "${RASPICAM_GROUP}" "${BUILD_DIR}"
chmod 0711 "${TMP_DIR}"
BUILD_OUT="${BUILD_DIR}/raspicam-ui"
sudo -u "${RASPICAM_USER}" -H env \
	PATH="/usr/local/go/bin:/usr/bin:/bin" \
	HOME="${RASPICAM_HOME}" \
	GOFLAGS="-p=1" \
	sh -c 'cd "$1" && go build -trimpath -ldflags "-s -w" -o "$2.tmp" ./cmd/ui' _ "${REPO_DIR}" "${BUILD_OUT}" ||
	die "go build failed"
install -m 0755 "${BUILD_OUT}.tmp" /usr/local/bin/raspicam-ui
info "installed /usr/local/bin/raspicam-ui ($(git -C "${REPO_DIR}" describe --always --dirty 2>/dev/null || echo unknown))"

# ---------------------------------------------------------------------------
# Permissions for metrics.
# ---------------------------------------------------------------------------
if getent group video >/dev/null && ! id -nG "${RASPICAM_USER}" | tr ' ' '\n' | grep -qx video; then
	log "Adding ${RASPICAM_USER} to the video group (needed by vcgencmd)"
	usermod -aG video "${RASPICAM_USER}"
fi

# ---------------------------------------------------------------------------
# systemd units.
# ---------------------------------------------------------------------------
log "Installing systemd units"
install -m 0644 "${REPO_DIR}/systemd/mediamtx.service" /etc/systemd/system/mediamtx.service
install -m 0644 "${REPO_DIR}/systemd/raspicam-ui.service" /etc/systemd/system/raspicam-ui.service
install -d -m 0755 /etc/systemd/system/raspicam-ui.service.d
cat >/etc/systemd/system/raspicam-ui.service.d/override.conf <<EOF
# Written by scripts/setup-pi.sh; re-run the script to regenerate.
[Unit]
After=mediamtx.service

[Service]
User=${RASPICAM_USER}
Group=${RASPICAM_GROUP}
Environment=UI_ADDR=${UI_ADDR}
Environment=MEDIAMTX_API_URL=http://127.0.0.1:9997
Environment=MEDIAMTX_PATH_NAME=${MEDIAMTX_PATH_NAME}
Environment=MEDIAMTX_CONFIG_PATH=${MEDIAMTX_CONF}
EOF

# Boot ordering: Raspberry Pi OS uses NetworkManager. systemd-networkd's
# wait-online manages nothing here and delays boot by ~2 minutes.
if systemctl list-unit-files NetworkManager-wait-online.service >/dev/null 2>&1; then
	systemctl enable NetworkManager-wait-online.service >/dev/null 2>&1 || true
fi
if systemctl is-enabled systemd-networkd-wait-online.service >/dev/null 2>&1; then
	info "disabling systemd-networkd-wait-online.service"
	systemctl disable systemd-networkd-wait-online.service
fi

systemctl daemon-reload

# ---------------------------------------------------------------------------
# Camera self-test (before MediaMTX grabs the camera).
# ---------------------------------------------------------------------------
if command -v rpicam-hello >/dev/null 2>&1; then
	log "Camera self-test"
	systemctl stop mediamtx.service 2>/dev/null || true
	if cams="$(rpicam-hello --list-cameras 2>&1)" && grep -q '^[0-9] : ' <<<"${cams}"; then
		grep '^[0-9] : ' <<<"${cams}" | sed 's/^/    /'
	else
		warn "no camera detected; check the ribbon cable and see RPI_SETUP.md troubleshooting"
	fi
fi

# ---------------------------------------------------------------------------
# Start services.
# ---------------------------------------------------------------------------
log "Starting services"
systemctl enable mediamtx.service raspicam-ui.service >/dev/null 2>&1
systemctl restart mediamtx.service
systemctl restart raspicam-ui.service
if [[ "${mtx_binary_changed}" -eq 1 ]]; then
	info "MediaMTX binary was updated"
fi

# Give MediaMTX a moment, then check the API and the UI.
api_ok=0
for _ in $(seq 1 15); do
	if curl -fsS -o /dev/null "http://127.0.0.1:9997/v3/paths/list"; then
		api_ok=1
		break
	fi
	sleep 1
done
ui_port="${UI_ADDR##*:}"
ui_ok=0
for _ in $(seq 1 10); do
	if curl -fsS -o /dev/null "http://127.0.0.1:${ui_port}/"; then
		ui_ok=1
		break
	fi
	sleep 1
done

log "Status"
info "mediamtx:    $(systemctl is-active mediamtx.service || true)"
info "raspicam-ui: $(systemctl is-active raspicam-ui.service || true)"
if [[ "${api_ok}" -eq 1 ]]; then
	info "MediaMTX API: OK"
else
	warn "MediaMTX API not reachable; see: journalctl -u mediamtx -n 50"
fi
if [[ "${ui_ok}" -eq 1 ]]; then
	info "UI:           OK"
else
	warn "UI not reachable; see: journalctl -u raspicam-ui -n 50"
fi

host="$(hostname).local"
ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
cat <<EOF

Done.
  Status UI:  http://${host}:${ui_port}/   (http://${ip:-<pi-ip>}:${ui_port}/)
  WebRTC:     http://${host}:8889/${MEDIAMTX_PATH_NAME}
  HLS:        http://${host}:8888/${MEDIAMTX_PATH_NAME}
  RTSP:       rtsp://${host}:8554/${MEDIAMTX_PATH_NAME}
  Config:     ${MEDIAMTX_CONF}
  Logs:       journalctl -u mediamtx -u raspicam-ui -f
EOF
