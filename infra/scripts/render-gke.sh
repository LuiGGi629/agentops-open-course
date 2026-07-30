#!/usr/bin/env bash

# Resolve the cloud coordinates in a rendered GKE manifest. OpenTofu outputs
# are the default source after apply; explicit environment variables keep the
# renderer testable before any cloud resources exist.

source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"

require_cmd kubectl platform

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
template="${1:-}"
project_id="${GCP_PROJECT_ID:-}"
bucket_name="${MLFLOW_BUCKET_NAME:-}"
cluster_dns_ip="${GKE_CLUSTER_DNS_IP:-}"

if [[ -z ${project_id} || -z ${bucket_name} || -z ${cluster_dns_ip} ]]; then
	require_cmd tofu gcp
	project_id="${project_id:-$(tofu -chdir=infra/gcp output -raw project_id)}"
	bucket_name="${bucket_name:-$(tofu -chdir=infra/gcp output -raw mlflow_bucket_name)}"
	cluster_dns_ip="${cluster_dns_ip:-$(tofu -chdir=infra/gcp output -raw cluster_dns_ip)}"
fi

[[ ${project_id} =~ ^[a-z][a-z0-9-]{4,28}[a-z0-9]$ ]] ||
	fail "GCP_PROJECT_ID must be a valid Google Cloud project ID"
[[ ${bucket_name} =~ ^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$ ]] ||
	fail "MLFLOW_BUCKET_NAME must be a valid 3-63 character GCS bucket name"
[[ ${cluster_dns_ip} =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
	fail "GKE_CLUSTER_DNS_IP must be an IPv4 address"

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/agentops-gke-render.XXXXXX")"
trap 'rm -rf "${tmp_dir}"' EXIT
template_file="${tmp_dir}/template.yaml"
rendered_file="${tmp_dir}/rendered.yaml"

case ${template} in
"")
	kubectl kustomize infra/k8s/overlays/gke >"${template_file}"
	;;
-)
	cat >"${template_file}"
	;;
*)
	[[ -f ${template} ]] || fail "GKE manifest template does not exist: ${template}"
	cp "${template}" "${template_file}"
	;;
esac

sed \
	-e "s/__GCP_PROJECT_ID__/${project_id}/g" \
	-e "s/__MLFLOW_BUCKET_NAME__/${bucket_name}/g" \
	-e "s/__GKE_CLUSTER_DNS_IP__/${cluster_dns_ip}/g" \
	"${template_file}" >"${rendered_file}"

if rg -q '__[A-Z0-9_]+__' "${rendered_file}"; then
	fail "rendered GKE manifest still contains unresolved placeholders"
fi

cat "${rendered_file}"
