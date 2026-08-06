#!/usr/bin/env bash

set -euo pipefail

wait_for_job() {
	local namespace="$1"
	local job="$2"
	local timeout_seconds="$3"
	local deadline=$((SECONDS + timeout_seconds))
	local condition
	while ((SECONDS < deadline)); do
		condition="$(
			kubectl -n "${namespace}" get job "${job}" -o json |
				jq -r '
                  if any(.status.conditions[]?; .type == "Complete" and .status == "True") then "complete"
                  elif any(.status.conditions[]?; .type == "Failed" and .status == "True") then "failed"
                  else "pending"
                  end'
		)"
		case "${condition}" in
		complete) return 0 ;;
		pending) ;;
		failed)
			echo "::error::Job ${namespace}/${job} failed."
			kubectl -n "${namespace}" logs "job/${job}" || true
			return 1
			;;
		*)
			echo "::error::Job ${namespace}/${job} returned unknown condition ${condition}."
			return 1
			;;
		esac
		sleep 2
	done
	echo "::error::Timed out waiting for Job ${namespace}/${job}."
	kubectl -n "${namespace}" logs "job/${job}" || true
	return 1
}

agent_pod="$(
	kubectl -n agentops get pods \
		-l app.kubernetes.io/name=agentops-agent \
		-o jsonpath='{.items[0].metadata.name}'
)"
readonly agent_pod
kubectl -n agentops exec -i "${agent_pod}" -- \
	python - "${BACKUP_EVIDENCE_MARKER}" \
	<infra/scripts/platform_backup_seed.py \
	>"${RUNNER_TEMP}/platform-backup-evidence.json"
jq -e '.session_id and .task_id and .memory_note and .audit_invocation_id' \
	"${RUNNER_TEMP}/platform-backup-evidence.json" >/dev/null
kubectl -n agentops create configmap platform-backup-evidence \
	--from-file="evidence.json=${RUNNER_TEMP}/platform-backup-evidence.json"
kubectl -n agentops create configmap platform-backup-programs \
	--from-file=platform_backup_mutate.py=infra/scripts/platform_backup_mutate.py \
	--from-file=platform_backup_verify.py=infra/scripts/platform_backup_verify.py

backup_stamp="$(date -u +%Y%m%dT%H%M%SZ)"
readonly backup_stamp
kubectl -n agentops create job \
	--from=cronjob/agentops-state-backup \
	platform-state-backup \
	--dry-run=client -o json |
	jq --arg stamp "${backup_stamp}" \
		'.spec.template.spec.containers[0].env =
          ((.spec.template.spec.containers[0].env // []) +
            [{name: "STATE_BACKUP_TIMESTAMP", value: $stamp}])' |
	kubectl create --filename -
wait_for_job agentops platform-state-backup 300
kubectl -n agentops logs job/platform-state-backup

agent_image="$(
	kubectl -n agentops get pods \
		-l app.kubernetes.io/name=agentops-agent \
		-o jsonpath='{.items[0].spec.containers[0].image}'
)"
readonly agent_image
kubectl -n agentops patch agent agentops-agent --type=merge \
	--patch '{"spec":{"byo":{"deployment":{"replicas":0}}}}'
kubectl -n agentops scale deployment agentops-agent agentops-mcp --replicas=0
readonly writer_stop_deadline=$((SECONDS + 120))
while :; do
	agent_pods="$(kubectl -n agentops get pods -l app.kubernetes.io/name=agentops-agent -o name)"
	mcp_pods="$(kubectl -n agentops get pods -l app.kubernetes.io/name=agentops-mcp -o name)"
	if [[ -z ${agent_pods} && -z ${mcp_pods} ]]; then
		break
	fi
	((SECONDS < writer_stop_deadline)) || {
		echo "::error::Timed out while stopping state readers and writers."
		exit 1
	}
	sleep 2
done

cat <<EOF | kubectl create --filename -
apiVersion: batch/v1
kind: Job
metadata:
  name: platform-state-restore
  namespace: agentops
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 300
  template:
    spec:
      serviceAccountName: agentops-state-backup
      automountServiceAccountToken: false
      restartPolicy: Never
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        runAsGroup: 10001
        fsGroup: 10001
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: restore
          image: ${agent_image}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-eu", "-c"]
          args:
            - |
              snapshot="/backups/\${BACKUP_STAMP}"
              test -f "\${snapshot}/manifest.json"
              test -f "\${snapshot}/.complete"
              python /programs/platform_backup_mutate.py /app/state /evidence/evidence.json
              python -m agent.state restore "\${snapshot}" --state-dir /app/state
              test ! -e /app/state/obsolete.db
              PYTHONPATH=/programs python /programs/platform_backup_verify.py \
                "\${snapshot}" /app/state "\${EXPECTED_COMMIT}" /evidence/evidence.json
          env:
            - name: BACKUP_STAMP
              value: "${backup_stamp}"
            - name: EXPECTED_COMMIT
              value: "${GITHUB_SHA}"
          resources:
            requests:
              cpu: 50m
              memory: 128Mi
            limits:
              cpu: 200m
              memory: 256Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: [ALL]
          volumeMounts:
            - name: backups
              mountPath: /backups
              readOnly: true
            - name: evidence
              mountPath: /evidence
              readOnly: true
            - name: programs
              mountPath: /programs
              readOnly: true
            - name: state
              mountPath: /app/state
      volumes:
        - name: backups
          persistentVolumeClaim:
            claimName: agentops-state-backups
        - name: evidence
          configMap:
            name: platform-backup-evidence
        - name: programs
          configMap:
            name: platform-backup-programs
        - name: state
          persistentVolumeClaim:
            claimName: agentops-agent-state
EOF
wait_for_job agentops platform-state-restore 300
kubectl -n agentops logs job/platform-state-restore
kubectl -n agentops patch agent agentops-agent --type=merge \
	--patch '{"spec":{"byo":{"deployment":{"replicas":1}}}}'
kubectl -n agentops scale deployment agentops-mcp --replicas=1
kubectl -n agentops rollout status deployment/agentops-agent --timeout=300s
kubectl -n agentops rollout status deployment/agentops-mcp --timeout=300s
kubectl -n agentops delete job platform-state-backup platform-state-restore --wait=true
kubectl -n agentops delete configmap platform-backup-evidence platform-backup-programs
