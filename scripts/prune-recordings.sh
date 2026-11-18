#!/bin/sh
# raspicam-prune-recordings: delete old recordings only when space runs low.
#
# MediaMTX can only delete recordings by age (recordDeleteAfter). Here that is
# disabled (recordDeleteAfter: 0s) so recordings are kept for as long as the
# SD card has room. This script deletes the oldest recording segments, one at
# a time, only while free space on the recordings filesystem is below
# MIN_FREE_GB. It does nothing while there is enough free space.
#
# Never touched:
#   - files modified in the last 5 minutes (the segment MediaMTX is writing),
#   - anything that is not an .mp4 file.
#
# Empty (0-byte) segments are always deleted, whatever the free space. MediaMTX
# leaves one when the camera fails right after it opens a segment (e.g. a
# brownout reboot), and a single empty segment makes the playback server's
# /list endpoint fail with "EOF" for every time range that includes it.
#
# Environment:
#   RECORDINGS_DIR  recordings root (default /recordings)
#   MIN_FREE_GB     free-space margin in GiB (default 15)
#   DRY_RUN=1       print what would be deleted first, delete nothing
#
# Installed by scripts/setup-pi.sh as /usr/local/sbin/raspicam-prune-recordings
# and run every 5 minutes by raspicam-prune-recordings.timer.
set -eu

DIR="${RECORDINGS_DIR:-/recordings}"
MIN_FREE_GB="${MIN_FREE_GB:-15}"
DRY_RUN="${DRY_RUN:-0}"

case "${MIN_FREE_GB}" in
'' | *[!0-9]*)
	echo "raspicam-prune-recordings: MIN_FREE_GB must be a whole number, got '${MIN_FREE_GB}'" >&2
	exit 2
	;;
esac
min_kb=$((MIN_FREE_GB * 1024 * 1024))

avail_kb() { df -Pk "${DIR}" | awk 'NR == 2 { print $4 }'; }

[ -d "${DIR}" ] || exit 0

find "${DIR}" -type f -name '*.mp4' -size 0 -mmin +5 |
	while IFS= read -r f; do
		if [ "${DRY_RUN}" = 1 ]; then
			echo "would delete empty ${f}"
			continue
		fi
		rm -f -- "${f}"
		logger -t raspicam-prune "deleted empty ${f}"
	done

[ "$(avail_kb)" -lt "${min_kb}" ] || exit 0

# Oldest first, by modification time. Segment names have no spaces or
# newlines (MediaMTX recordPath uses only date/time fields).
find "${DIR}" -type f -name '*.mp4' -mmin +5 -printf '%T@ %p\n' | sort -n | cut -d' ' -f2- |
	while IFS= read -r f; do
		avail="$(avail_kb)"
		[ "${avail}" -lt "${min_kb}" ] || break
		if [ "${DRY_RUN}" = 1 ]; then
			echo "would delete ${f} (free ${avail} KiB < ${min_kb} KiB)"
			break
		fi
		rm -f -- "${f}"
		logger -t raspicam-prune "deleted ${f} (free ${avail} KiB < ${min_kb} KiB)"
	done

if [ "${DRY_RUN}" != 1 ] && [ "$(avail_kb)" -lt "${min_kb}" ]; then
	logger -t raspicam-prune -p user.warning \
		"free space still below ${MIN_FREE_GB} GiB and no older recordings left to delete in ${DIR}"
fi
