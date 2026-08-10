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

if (PATH="${tmp_dir}" require_host_cmd definitely-not-a-command "install it from a reviewed host package source") \
	2>"${tmp_dir}/require-host"; then
	fail "require_host_cmd accepted a missing command"
fi
grep -Fqx \
	"missing definitely-not-a-command: install it from a reviewed host package source" \
	"${tmp_dir}/require-host"

printf 'verified bytes' >"${tmp_dir}/artifact"
artifact_sha256="$(sha256sum "${tmp_dir}/artifact" | awk '{ print $1 }')"
verify_sha256 "${tmp_dir}/artifact" "${artifact_sha256}" "test artifact"
if (verify_sha256 "${tmp_dir}/artifact" "$(printf '0%.0s' {1..64})" "test artifact") \
	2>"${tmp_dir}/checksum"; then
	fail "verify_sha256 accepted a mismatched digest"
fi
grep -Fq "test artifact checksum mismatch" "${tmp_dir}/checksum"

mkdir "${tmp_dir}/plugin"
git -C "${tmp_dir}/plugin" init -q
printf 'reviewed executable' >"${tmp_dir}/plugin/tool"
printf 'command: tool\n' >"${tmp_dir}/plugin/plugin.yaml"
printf 'tool\n' >"${tmp_dir}/plugin/.gitignore"
git -C "${tmp_dir}/plugin" add .gitignore plugin.yaml
git -C "${tmp_dir}/plugin" -c user.name=test -c user.email=test@example.test \
	commit -qm "test plugin"
plugin_commit="$(git -C "${tmp_dir}/plugin" rev-parse HEAD)"
plugin_sha256="$(sha256_file "${tmp_dir}/plugin/tool")"
verify_git_binary_install \
	"${tmp_dir}/plugin" "${plugin_commit}" "tool" "${plugin_sha256}" "test plugin"
printf 'command: bin/diff\n' >"${tmp_dir}/plugin/plugin.yaml"
if (verify_git_binary_install \
	"${tmp_dir}/plugin" "${plugin_commit}" "tool" "${plugin_sha256}" "test plugin") \
	2>"${tmp_dir}/plugin-dirty"; then
	fail "verify_git_binary_install accepted modified plugin metadata"
fi
grep -Fq "test plugin source checkout is dirty" "${tmp_dir}/plugin-dirty"
printf 'command: tool\n' >"${tmp_dir}/plugin/plugin.yaml"
printf 'tampered' >>"${tmp_dir}/plugin/tool"
if (verify_git_binary_install \
	"${tmp_dir}/plugin" "${plugin_commit}" "tool" "${plugin_sha256}" "test plugin") \
	2>"${tmp_dir}/plugin-checksum"; then
	fail "verify_git_binary_install accepted a modified executable"
fi
grep -Fq "test plugin executable checksum mismatch" "${tmp_dir}/plugin-checksum"

assert_eq "matching invariant" "value" "value"
if (assert_eq "named invariant" "actual" "expected") 2>"${tmp_dir}/assert"; then
	fail "assert_eq accepted different values"
fi
grep -Fqx "named invariant: got 'actual', want 'expected'" "${tmp_dir}/assert"

# The false case is the regression that matters: every clean checkout reports dirty=false,
# and a `jq -e` read of it aborted check:infra, the image builds, and the backup drill.
flag="$(json_flag '.dirty' '{"dirty":false}')"
assert_eq "false flag" "${flag}" "false"
flag="$(json_flag '.dirty' '{"dirty":true}')"
assert_eq "true flag" "${flag}" "true"
for invalid in '{}' '{"dirty":null}' '{"dirty":"false"}' '{"dirty":0}'; do
	if (json_flag '.dirty' "${invalid}") >/dev/null 2>"${tmp_dir}/flag"; then
		fail "json_flag accepted a non-boolean: ${invalid}"
	fi
	grep -Fqx "expected a JSON boolean at .dirty" "${tmp_dir}/flag"
done
