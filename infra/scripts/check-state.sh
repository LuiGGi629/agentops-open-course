#!/usr/bin/env bash

# shellcheck source=scripts/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"

require_cmd kubectl platform
require_cmd yq gateway

infra_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

for overlay in local gke; do
	manifest="${tmp_dir}/${overlay}.yaml"
	if [[ "${overlay}" == "gke" ]]; then
		GCP_PROJECT_ID=agentops-course-check \
			MLFLOW_BUCKET_NAME=agentops-course-check-mlflow \
			GKE_CLUSTER_DNS_IP=10.30.0.10 \
			"${infra_dir}/scripts/render-gke.sh" >"${manifest}"
	else
		kubectl kustomize "${infra_dir}/k8s/overlays/${overlay}" >"${manifest}"
	fi

	agent_claim="$(
		yq -r 'select(.kind == "Agent" and .metadata.name == "agentops-agent")
      | .spec.byo.deployment.volumes[]
      | select(.name == "state")
      | .persistentVolumeClaim.claimName' "${manifest}"
	)"
	agent_tmp_limit="$(
		yq -r 'select(.kind == "Agent" and .metadata.name == "agentops-agent")
      | .spec.byo.deployment.volumes[]
      | select(.name == "tmp")
      | .emptyDir.sizeLimit' "${manifest}"
	)"
	mcp_claim="$(
		yq -r 'select(.kind == "Deployment" and .metadata.name == "agentops-mcp")
      | .spec.template.spec.volumes[]
      | select(.name == "state")
      | .persistentVolumeClaim.claimName' "${manifest}"
	)"
	mcp_fs_group="$(
		yq -r 'select(.kind == "Deployment" and .metadata.name == "agentops-mcp")
      | .spec.template.spec.securityContext.fsGroup' "${manifest}"
	)"
	mcp_state_read_only="$(
		yq -r 'select(.kind == "Deployment" and .metadata.name == "agentops-mcp")
      | .spec.template.spec.containers[]
      | select(.name == "mcp")
      | .volumeMounts[]
      | select(.name == "state")
      | .readOnly' "${manifest}"
	)"
	gateway_automount="$(
		yq -r 'select(.kind == "ServiceAccount" and .metadata.name == "agentgateway")
      | .automountServiceAccountToken' "${manifest}"
	)"
	mlflow_automount="$(
		yq -r 'select(.kind == "ServiceAccount" and .metadata.name == "mlflow")
      | .automountServiceAccountToken' "${manifest}"
	)"
	mlflow_artifact_root="$(
		yq -r 'select(.kind == "Deployment" and .metadata.name == "mlflow")
      | .spec.template.spec.containers[]
      | select(.name == "mlflow")
      | .env[]
      | select(.name == "MLFLOW_DEFAULT_ARTIFACT_ROOT")
      | .value' "${manifest}"
	)"
	mlflow_artifacts_destination="$(
		yq -r 'select(.kind == "Deployment" and .metadata.name == "mlflow")
      | .spec.template.spec.containers[]
      | select(.name == "mlflow")
      | .env[]
      | select(.name == "MLFLOW_ARTIFACTS_DESTINATION")
      | .value' "${manifest}"
	)"
	unbounded_tmp_volumes="$(
		yq -r 'select(
        .spec.template.spec.volumes != null
        and ([.spec.template.spec.volumes[]
          | select(.name == "tmp" and .emptyDir.sizeLimit == null)] | length > 0)
      )
      | .metadata.name' "${manifest}"
	)"

	assert_eq "${overlay} agent state claim" "${agent_claim}" "agentops-agent-state"
	assert_eq "${overlay} agent tmp limit" "${agent_tmp_limit}" "128Mi"
	assert_eq "${overlay} MCP shared state claim" "${mcp_claim}" "${agent_claim}"
	assert_eq "${overlay} MCP fsGroup" "${mcp_fs_group}" "10001"
	assert_eq "${overlay} MCP state read-only" "${mcp_state_read_only}" "true"
	assert_eq "${overlay} gateway token automount" "${gateway_automount}" "false"
	assert_eq "${overlay} MLflow token automount" "${mlflow_automount}" "false"
	assert_eq "${overlay} MLflow artifact root" "${mlflow_artifact_root}" "mlflow-artifacts:/"
	if [[ "${overlay}" == "gke" ]]; then
		assert_eq "GKE MLflow artifact destination" "${mlflow_artifacts_destination}" "gs://agentops-course-check-mlflow"
	else
		assert_eq "local MLflow artifact destination" "${mlflow_artifacts_destination}" "/var/lib/mlflow/artifacts"
	fi
	[[ -z "${unbounded_tmp_volumes}" ]]
done
