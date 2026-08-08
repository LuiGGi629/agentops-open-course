#!/usr/bin/env bash

# Shared preamble for the repository's shell scripts.
#
# Source it as the first thing a script does:
#
#     source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
#
# It sets strict mode and provides the small prerequisite helpers shared by the
# repository scripts.

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

# assert_eq <label> <got> <want> — compare one exact invariant and name it on failure.
assert_eq() {
	local label="$1"
	local got="$2"
	local want="$3"

	[[ "${got}" == "${want}" ]] || fail "${label}: got '${got}', want '${want}'"
}

# absolute_path <path> — resolve a possibly relative path against the caller's directory.
#
# Scripts that `cd` into a project before invoking a tool must resolve caller-supplied paths
# first, or a relative argument silently resolves against the project instead. The parent
# directory must exist; the leaf need not, so this also works for a path about to be created.
absolute_path() {
	local path="$1"
	local parent
	parent="$(cd -- "$(dirname -- "${path}")" 2>/dev/null && pwd)" ||
		fail "cannot resolve ${path}: its parent directory does not exist"
	printf '%s/%s\n' "${parent}" "$(basename -- "${path}")"
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

# require_cgroup_v2 <cgroup-root> — Kubernetes 1.35 removed cgroup v1 support.
# Check the host before the pinned k3s line creates a partial cluster.
require_cgroup_v2() {
	local cgroup_root="$1"

	if [[ -r ${cgroup_root}/cgroup.controllers ]]; then
		return 0
	fi
	fail "cgroup v2 required for pinned Kubernetes; enable the unified cgroup hierarchy before running local k3d"
}
