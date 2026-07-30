#!/usr/bin/env bash

# Build, render, validate, and apply the GKE workload bundle after an explicitly
# approved OpenTofu apply. The exact kubectl context check prevents a valid
# bundle from reaching the wrong cluster.

source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"

for command_name in helmfile kubeconform kubectl skaffold tofu; do
	require_cmd "${command_name}" platform
done

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
project_id="$(tofu -chdir=infra/gcp output -raw project_id)"
cluster_name="$(tofu -chdir=infra/gcp output -raw cluster_name)"
cluster_zone="$(tofu -chdir=infra/gcp output -raw cluster_zone)"
repository="$(tofu -chdir=infra/gcp output -raw artifact_registry_repository)"
expected_context="gke_${project_id}_${cluster_zone}_${cluster_name}"
current_context="$(kubectl config current-context)"

[[ ${current_context} == "${expected_context}" ]] ||
	fail "kubectl context is ${current_context}; expected ${expected_context}"

mkdir -p .agents/tmp
artifacts=".agents/tmp/gke-artifacts.json"
template=".agents/tmp/gke-template.yaml"
manifest=".agents/tmp/gke.yaml"

(
	cd infra
	SKAFFOLD_DEFAULT_REPO="${repository}" skaffold build \
		--filename skaffold.yaml \
		--profile gke \
		--file-output "../${artifacts}"
	skaffold render \
		--filename skaffold.yaml \
		--profile gke \
		--build-artifacts "../${artifacts}" \
		--output "../${template}"
)
infra/scripts/render-gke.sh "${template}" >"${manifest}"
kubeconform -strict -ignore-missing-schemas -summary "${manifest}"

kubectl apply -f infra/k8s/base/namespace.yaml
helmfile --file infra/helmfile.yaml apply --skip-diff-on-install
kubectl apply -f "${manifest}"
