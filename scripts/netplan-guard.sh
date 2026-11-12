#!/bin/sh
# raspicam-netplan-guard: keep the WiFi config alive across hard resets.
#
# On Raspberry Pi OS Trixie, NetworkManager stores its connections as netplan
# YAML in /etc/netplan/90-NM-*.yaml and rewrites those files on every start
# (it runs /usr/libexec/netplan/generate and reloads itself). The files are not
# fsync'ed, so a power loss or watchdog reset shortly after a rewrite can leave
# them empty (0 bytes). NetworkManager then has no WiFi profile and the Pi
# never rejoins the network, even after a power-cycle.
#
#   backup   Copy every non-empty /etc/netplan/*.yaml to the backup dir and
#            fsync it. Empty files are never backed up. Backups of files that
#            no longer exist are removed, so deleted connections stay deleted.
#   restore  Before NetworkManager starts: replace every EMPTY
#            /etc/netplan/*.yaml with its backup, then regenerate the
#            NetworkManager profiles. Missing files are left alone.
#
# Installed by scripts/setup-pi.sh as /usr/local/sbin/raspicam-netplan-guard.
set -eu

NETPLAN_DIR="/etc/netplan"
BACKUP_DIR="${RASPICAM_NETPLAN_BACKUP_DIR:-/var/lib/raspicam/netplan-backup}"

log() { echo "raspicam-netplan-guard: $*"; }

backup() {
	install -d -m 0700 "${BACKUP_DIR}"
	changed=0
	for f in "${NETPLAN_DIR}"/*.yaml; do
		[ -e "${f}" ] || continue
		[ -s "${f}" ] || { log "skipping empty ${f}"; continue; }
		b="${BACKUP_DIR}/$(basename "${f}")"
		if [ ! -f "${b}" ] || ! cmp -s "${f}" "${b}"; then
			install -m 0600 "${f}" "${b}.tmp"
			sync "${b}.tmp"
			mv -f "${b}.tmp" "${b}"
			changed=1
			log "backed up ${f}"
		fi
	done
	for b in "${BACKUP_DIR}"/*.yaml; do
		[ -e "${b}" ] || continue
		if [ ! -e "${NETPLAN_DIR}/$(basename "${b}")" ]; then
			rm -f "${b}"
			changed=1
			log "removed stale backup ${b}"
		fi
	done
	[ "${changed}" -eq 1 ] && sync "${BACKUP_DIR}"
	return 0
}

restore() {
	restored=0
	for f in "${NETPLAN_DIR}"/*.yaml; do
		[ -e "${f}" ] || continue
		[ -s "${f}" ] && continue
		b="${BACKUP_DIR}/$(basename "${f}")"
		if [ -s "${b}" ]; then
			install -m 0600 "${b}" "${f}.tmp"
			sync "${f}.tmp"
			mv -f "${f}.tmp" "${f}"
			restored=1
			log "restored empty ${f} from ${b}"
		else
			log "WARNING: ${f} is empty and has no backup"
		fi
	done
	if [ "${restored}" -eq 1 ]; then
		sync "${NETPLAN_DIR}"
		# The netplan systemd generator already ran at early boot with the
		# empty files; regenerate the NetworkManager profiles now.
		netplan generate
		log "regenerated NetworkManager profiles"
	fi
	return 0
}

case "${1:-}" in
backup) backup ;;
restore) restore ;;
*)
	echo "usage: $0 backup|restore" >&2
	exit 2
	;;
esac
