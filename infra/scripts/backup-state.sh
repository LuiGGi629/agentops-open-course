#!/usr/bin/env bash
# Snapshot the agent SQLite state (sessions, A2A tasks, audit trail) while it
# may be in use. Plain `cp` of a live database can capture a torn transaction;
# `VACUUM INTO` produces a transactionally consistent copy through SQLite.
#
# A snapshot is built in a hidden temporary directory, verified completely,
# marked with `.complete`, then atomically renamed into the visible timestamped
# namespace. Failed/interrupted attempts therefore cannot masquerade as the
# newest restorable snapshot.
#
# Usage: backup-state.sh [state_dir] [backup_root]
#   state_dir    defaults to agents/python/.state
#   backup_root  defaults to .state-backups (gitignored); each run publishes a
#                UTC-timestamped snapshot directory and keeps the most recent
#                STATE_BACKUP_KEEP (default 7) completed snapshots.

# shellcheck source=scripts/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"

require_cmd sqlite3 base

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
state_dir="${1:-${repo_dir}/agents/python/.state}"
backup_root="${2:-${repo_dir}/.state-backups}"
keep="${STATE_BACKUP_KEEP:-7}"
snapshot_staging=""
snapshot_lock=""

cleanup_incomplete_snapshot() {
	if [[ -n "${snapshot_staging}" && -d "${snapshot_staging}" ]]; then
		rm -rf "${snapshot_staging}"
	fi
	if [[ -n "${snapshot_lock}" && -d "${snapshot_lock}" ]]; then
		rmdir "${snapshot_lock}" 2>/dev/null || true
	fi
}

completed_snapshots() {
	local candidate

	for candidate in "${backup_root}"/*; do
		[[ -d "${candidate}" && -f "${candidate}/.complete" ]] || continue
		printf '%s\n' "${candidate}"
	done
}

trap cleanup_incomplete_snapshot EXIT

if [[ ! -d "${state_dir}" ]]; then
	log "error: state directory not found: ${state_dir}"
	fail "hint: run the agent or MCP server once to initialize it"
fi
if [[ ! "${keep}" =~ ^[1-9][0-9]*$ ]]; then
	fail "error: STATE_BACKUP_KEEP must be a positive integer, got ${keep}"
fi

stamp="${STATE_BACKUP_TIMESTAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
if [[ ! "${stamp}" =~ ^[0-9]{8}T[0-9]{6}Z$ ]]; then
	fail "error: STATE_BACKUP_TIMESTAMP must use YYYYMMDDTHHMMSSZ, got ${stamp}"
fi
snapshot_dir="${backup_root}/${stamp}"
mkdir -p "${backup_root}"
if [[ -e "${snapshot_dir}" ]]; then
	fail "error: completed snapshot already exists: ${snapshot_dir}"
fi
lock_candidate="${backup_root}/.lock-${stamp}"
if ! mkdir "${lock_candidate}" 2>/dev/null; then
	fail "error: another backup owns timestamp ${stamp}"
fi
snapshot_lock="${lock_candidate}"
snapshot_staging="$(mktemp -d "${backup_root}/.incomplete-${stamp}.XXXXXX")"

databases="$(find "${state_dir}" -maxdepth 1 -name '*.db' | sort)"

backed_up=0
while IFS= read -r database; do
	[[ -n "${database}" ]] || continue
	target="${snapshot_staging}/$(basename "${database}")"
	# The target path is interpolated into SQL, so refuse the one character
	# that could break out of the string literal.
	if [[ "${target}" == *"'"* ]]; then
		fail "error: backup path must not contain a single quote: ${target}"
	fi
	sqlite3 -readonly -batch -init /dev/null "${database}" "VACUUM INTO '${target}'"
	integrity="$(sqlite3 -batch -init /dev/null "${target}" 'PRAGMA integrity_check')"
	if [[ "${integrity}" != "ok" ]]; then
		fail "error: integrity check failed for ${target}: ${integrity}"
	fi
	echo "backed up $(basename "${database}") -> ${target}"
	backed_up=$((backed_up + 1))
done <<<"${databases}"

if [[ "${backed_up}" -eq 0 ]]; then
	fail "error: no SQLite databases in ${state_dir}"
fi

# The marker and rename are the publication boundary. Restore/retention ignore
# hidden or unmarked directories left by an ungraceful interruption.
printf 'completed_at=%s\ndatabases=%d\n' "${stamp}" "${backed_up}" >"${snapshot_staging}/.complete"
mv "${snapshot_staging}" "${snapshot_dir}"
snapshot_staging=""
rmdir "${snapshot_lock}"
snapshot_lock=""

# Retention: keep only the newest completed snapshots so the lab cannot fill
# the disk. Hidden/incomplete directories are deliberately ignored.
completed_snapshots | sort -r |
	tail -n "+$((keep + 1))" | while IFS= read -r old_snapshot; do
	rm -rf "${old_snapshot}"
	echo "pruned ${old_snapshot}"
done

echo "snapshot complete: ${snapshot_dir}"
