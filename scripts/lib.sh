#!/usr/bin/env bash

# Shared preamble for the repository's shell scripts.
#
# Source it as the first thing a script does:
#
#     source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
#
# It sets strict mode, and provides `require_cmd`, `make_tmpdir`, `log` and `fail`. The point of
# `require_cmd` is that a missing tool should tell you which `mise` command installs it, instead of
# dying with a bare "command not found" — the pinned toolchain is laddered by tier, and a script
# knows which tier it belongs to.

set -Eeuo pipefail

# log <message...> — progress on stderr, so stdout stays usable for a script's real output.
log() {
	printf '%s\n' "$*" >&2
}

# fail <message...> — report and exit non-zero.
fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

# require_cmd <command> [doctor-profile] — assert a tool is on PATH, naming how to install it.
require_cmd() {
	local command_name="$1"
	local profile="${2:-}"

	if command -v "${command_name}" >/dev/null 2>&1; then
		return 0
	fi

	if [[ -n ${profile} ]]; then
		fail "missing ${command_name}: run 'mise install', then 'mise run doctor:${profile}' to check the whole tier"
	fi
	fail "missing ${command_name}: run 'mise install' to materialize the pinned toolchain"
}

# make_tmpdir <slug> — create a scratch directory that removes itself when the script exits.
make_tmpdir() {
	local slug="${1:-agentops}"
	local directory

	directory=$(mktemp -d "${TMPDIR:-/tmp}/${slug}.XXXXXX")
	# shellcheck disable=SC2064 # expand the path now: the trap must not depend on a later variable.
	trap "rm -rf -- '${directory}'" EXIT
	printf '%s\n' "${directory}"
}
