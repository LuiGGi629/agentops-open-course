#!/usr/bin/env bash
# Publish a versioned, integrity-checked snapshot of every agent SQLite database.
#
# Usage: backup-state.sh [state_dir] [backup_root]
#   state_dir    defaults to agents/go/.state
#   backup_root  defaults to .state-backups

# shellcheck source=scripts/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"

require_cmd go base

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
state_dir="$(absolute_path "${1:-${repo_dir}/agents/go/.state}")"
backup_root="$(absolute_path "${2:-${repo_dir}/.state-backups}")"

# A release image injects AGENT_SOURCE_COMMIT. In a checkout, record the exact
# reviewed source automatically without making git a runtime backup dependency.
if [[ -z "${AGENT_SOURCE_COMMIT:-}" ]] && command -v git >/dev/null 2>&1; then
	AGENT_SOURCE_COMMIT="$(git -C "${repo_dir}" rev-parse HEAD 2>/dev/null || true)"
	export AGENT_SOURCE_COMMIT
fi

# `go run` compiles the module's own state CLI, so a checkout snapshots with exactly the
# source under review — the same subcommand the deployed image serves as `agent state backup`.
cd "${repo_dir}/agents/go"
exec go run ./cmd/agent state backup \
	--state-dir "${state_dir}" \
	--backup-root "${backup_root}"
