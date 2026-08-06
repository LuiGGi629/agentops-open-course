#!/usr/bin/env bash

lib_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib.sh
source "${lib_dir}/lib.sh"

tmp_dir=$(mktemp -d)
trap 'rm -r -- "${tmp_dir}"' EXIT

mkdir "${tmp_dir}/v1" "${tmp_dir}/v2"
touch "${tmp_dir}/v2/cgroup.controllers"

require_cgroup_v2 "${tmp_dir}/v2"
if (require_cgroup_v2 "${tmp_dir}/v1") 2>"${tmp_dir}/error"; then
	fail "require_cgroup_v2 accepted a cgroup v1 hierarchy"
fi
grep -Fqx \
	"cgroup v2 required for pinned Kubernetes; enable the unified cgroup hierarchy before running local k3d" \
	"${tmp_dir}/error"

log "test progress" 2>"${tmp_dir}/log"
grep -Fqx "test progress" "${tmp_dir}/log"

if (fail "expected failure") 2>"${tmp_dir}/fail"; then
	fail "fail returned successfully"
fi
grep -Fqx "expected failure" "${tmp_dir}/fail"

require_cmd sh
if (PATH="${tmp_dir}" require_cmd definitely-not-a-command validation) 2>"${tmp_dir}/require"; then
	fail "require_cmd accepted a missing command"
fi
grep -Fqx \
	"missing definitely-not-a-command: run 'mise install', then 'mise run doctor:validation' to check the whole tier" \
	"${tmp_dir}/require"

assert_eq "matching invariant" "value" "value"
if (assert_eq "named invariant" "actual" "expected") 2>"${tmp_dir}/assert"; then
	fail "assert_eq accepted different values"
fi
grep -Fqx "named invariant: got 'actual', want 'expected'" "${tmp_dir}/assert"
