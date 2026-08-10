# Go evaluation harness

This standalone module evaluates the AgentOps Go agent only through its public ADK REST or A2A wire contract. It does not require or import `agents/go`; the offline gate checks the resolved Go import graph to keep that boundary mechanical.

## What the offline gate can and cannot prove

`mise run check` and `mise run test` prove parsing, scoring, transport normalization, asset consistency, import separation, process isolation, evidence serialization, and in-memory telemetry. They call no model and start no observability stack, so a green offline run is never evidence that the agent answered well, that a judge agreed, or that a span reached Tempo. Only a model-backed `mise run eval` produces that, and only against the runtime it actually ran.

The wire shapes this module pins were recovered from the discarded Python scaffold and are recorded in `DESIGN.md`. They are held in place by fixed-wire and transport-equivalence tests rather than by that history.

## Commands

Run the offline gate from this directory:

```bash
mise run install
mise run format
mise run check
mise run test
mise run build
```

`mise run eval:validate` validates every committed asset, domain reference, report example, cost case set, dashboard, and import boundary without a model. `eval:ab` is also model-free: it compares two explicit sanitized artifacts. Every remaining `eval:*` task may call the configured generative or embedding model. Agent-facing runs isolate one runtime per case; retrieval isolates each mode; judge calibration calls the gateway directly.

The default runtime is the compiled `../agents/go/bin/agent` with temporary host state. Before any trial, the harness compares that exact binary's `version` tuple with the checkout source identity. The scheduled workflow instead sets `EVAL_AGENT_RUNTIME=container` and `EVAL_AGENT_IMAGE=agentops-agent:dev`; the harness resolves the tag once, compares the immutable image's OCI labels and `version` output with the checkout, then starts only that image ID. A missing or mismatched runtime is terminal; container mode never falls back to the host binary.

The model-backed task families are:

- `eval` and `eval:a2a`: judge-scored release qualification over the 15-case conversational set. These intentionally refuse to run while `release-policy.json` is `calibration-required`; once approved, the policy owns the pass and judge-agreement floors, repeat count, mandatory cases, and run budgets.
- `eval:policy-trial` and `eval:a2a:policy-trial`: three exact-source REST or A2A samples using the policy's case classification and learner floor. They collect calibration and cost observations but cannot qualify a release.
- `eval:workflow`: three samples of the bounded workflow set at the reviewed `0.33` aggregate floor.
- `eval:report`: three samples of the separately published `triage_report_agent` with strict JSON schema scoring.
- `eval:ground`: per-turn entity grounding against only the question and captured tool evidence.
- `eval:cost`: the reviewed per-case token/model-call budget, written to `cost-results.json` without replacing canonical release evidence.
- `eval:judge-calibration:trial`: balanced judge calibration at an explicit observation floor while policy approval is open.
- `eval:judge-calibration`: balanced judge calibration at the approved policy-owned agreement floor.
- `eval:retrieval`: exploratory seed-derived keyword-versus-semantic runbook hit-rate at 1 and 3 through isolated read-only MCP runtimes.
- `eval:ab`: artifact-only comparison of explicit runs captured from two Git-pinned prompt revisions.

Build the agent before a live task. Each live task loads the redacted root `.env`; exported variables still take precedence:

```bash
mise run --cd ../agents/go build
mise run eval
```

On a Linux host, the same harness can evaluate a locally built image:

```bash
mise run --cd .. build:agent-image
EVAL_AGENT_RUNTIME=container EVAL_AGENT_IMAGE=agentops-agent:dev mise run eval
```

The container path uses host networking because the supported scheduled runner is Ubuntu and its Ollama, agentgateway, and observability listeners are loopback-only. Credentials are passed to Docker by environment-variable name, never embedded in command arguments.

No live model, cluster, collector, or paid service belongs to `check` or `test`. `eval:cost:observe` deliberately replaces `cost-observed.json` with the positive usage needed before a reviewed re-baseline; ordinary `eval:cost` never does. Prompt comparison accepts only content-free run artifacts with the same model, transport, evalset digest, case samples, and score names per sample from distinct source commits; it recomputes summaries and fails on a deterministic-score or pass-rate regression.

Capture prompt candidates in separate Git worktrees so each agent binary and artifact comes from its declared revision:

```bash
cd "$(git rev-parse --show-toplevel)"
git worktree add ../agentops-baseline "$BASELINE_SHA"
git worktree add ../agentops-candidate "$CANDIDATE_SHA"
mise run --cd ../agentops-baseline/agents/go build
mise run --cd ../agentops-baseline/evals eval
mise run --cd ../agentops-candidate/agents/go build
mise run --cd ../agentops-candidate/evals eval
mise run eval:ab -- \
  --baseline ../../agentops-baseline/evals/eval-results.json \
  --candidate ../../agentops-candidate/evals/eval-results.json
```

Set `BASELINE_SHA` and `CANDIDATE_SHA` to reviewed immutable commits first. Both worktrees must resolve the same model configuration, temperature, seed data, and runtime class. Review the source diff to confirm the instruction is the intended variable; the harness cannot prove that a commit changed nothing else.

## Committed assets

The stable inputs are:

- `ops.evalset.json` — representative release evalset, 15 cases.
- `release-policy.json` — versioned case classification and the owner-approved qualification settings. Its current `calibration-required` status blocks release qualification without blocking exact-source trials.
- `workflow.evalset.json` — bounded workflow evalset, 3 cases.
- `triage-report.evalset.json` — structured report evalset, 3 cases.
- `cost_baseline.json` — inherited positive token/model-call observations for the 15 core cases. It is not a Go release baseline and cannot be promoted without reviewed Go trials.
- `judge-calibration.json` — balanced 12-case labeled judge set.
- `grafana-dashboard.json` — Prometheus comparison of a reviewed baseline run and candidate run.

The evalsets use the ADK JSON interchange schema. Domain vocabulary is read directly from immutable `../agents/data`; no agent package supplies expected values. `memory-note-recall` is also the mandatory PII boundary case: it requires the raw email address to become `<EMAIL_ADDRESS>` before the saved note and final output and deterministically forbids the raw value.

## Release evidence contract

Model-backed runs write sanitized evidence at the eval module root. These files are generated handoffs and are not committed fixtures:

- `eval-results.json` from `run`.
- `judge-calibration-results.json` from `calibrate`.
- `cost-observed.json` when `run --cost-output cost-observed.json` is selected.

The canonical `eval` task alone owns `eval-results.json` and writes `cost-observed.json` from that same model run, so release qualification can bind identical run ids and usage. Workflow, report, A2A, cost, groundedness, and retrieval tasks write `workflow-results.json`, `triage-report-results.json`, `a2a-results.json`, `cost-results.json`, `grounded-results.json`, and `retrieval-results.json`. Prompt A/B writes `prompt-comparison.json`. None can overwrite the release-bearing core result.

The exploratory retrieval artifact contains only schema version, typed source and platform identities, case count, a digest binding the seed-derived cases and sorted runbook bytes, embedding model identity, and keyword/semantic hit-rate at 1 and 3. Set `EVAL_EMBEDDING_MODEL_DIGEST` when the serving runtime exposes a reviewed immutable digest; endpoint URLs, queries, slugs, runbook bodies, tool payloads, and errors never enter the artifact.

`eval-results.json` has this stable shape:

```json
{
  "schema_version": 3,
  "run_id": "uuid",
  "source": {
    "mode": "release",
    "identity": "full-git-sha",
    "revision": "full-git-sha",
    "tree_digest": "sha256:content-digest",
    "dirty": false,
    "shallow": false
  },
  "platform_identity": "reviewed-runtime-class",
  "model": { "provider": "provider", "name": "model", "digest": "optional-digest" },
  "evalset": { "id": "evalset-id", "digest": "sha256" },
  "policy": { "version": "go-v1.0", "digest": "sha256" },
  "transport": "rest",
  "started_at": "RFC3339 timestamp",
  "completed_at": "RFC3339 timestamp",
  "cases": [
    {
      "id": "case-id",
      "sample": 1,
      "passed": true,
      "scores": { "trajectory": 1, "judge": 1 },
      "usage": { "input_tokens": 1, "output_tokens": 1, "total_tokens": 2, "model_calls": 1 }
    }
  ],
  "summary": {
    "passed": 1,
    "failed": 0,
    "pass_rate": 1,
    "minimum_pass_rate": 0.33,
    "required_cases_passed": true
  }
}
```

The schema-3 calibration artifact contains typed `source`, release-policy identity, `platform_identity`, typed judge provider/name/digest, calibration digest, policy-owned floor, matches, total, agreement, and per-case match fields. The schema-3 cost observation binds the same run, typed source, model, transport, and evalset, then records context length, serving version, temperature, stable `git` prompt-authority mode, evaluation-contract digest, and positive per-case/per-sample `total_tokens` and `model_calls`.

These schemas deliberately cannot contain prompts, answers, reference answers, tool arguments, tool responses, judge rationales, provider errors, endpoint URLs, credentials, or secrets. The Go serialization tests pin that boundary.

The run summary retains its observed `minimum_pass_rate` and `required_cases_passed` fields so a learner or trial can explain its own result. Release qualification does not trust those values: it loads `release-policy.json`, requires status `approved`, validates the policy and exact source-tree identity across the run, calibration, and cost handoffs, requires trajectory, judge, and control-specific scores, recomputes mandatory outcomes, and enforces the policy's pass, judge-agreement, repeat, and usage floors. Usage counters must be non-negative and every aggregation is overflow-checked, so malformed provider or artifact data cannot reduce a release budget.

## OpenTelemetry evidence

Set `EVAL_OTEL_EXPORTER_OTLP_ENDPOINT` to an explicit HTTP(S) OTLP endpoint to export evaluation evidence. The harness never inherits an agent OTLP endpoint, and every child agent is forced to run with `OTEL_SDK_DISABLED=true`.

The stable signals are:

- Spans: `agentops.eval.run`, `agentops.eval.case`, `agentops.eval.score`.
- Metrics: `agentops.eval.score`, `agentops.eval.case.passed`, `agentops.eval.tokens`, `agentops.eval.model_calls`, `agentops.eval.run.passed`.
- Attributes include `eval.run.id`, typed `agentops.source.{mode,identity,revision,tree_digest,dirty}`, `agentops.eval.platform`, `gen_ai.request.model`, `agentops.eval.evalset`, `agentops.eval.transport`, case/sample, and score name/pass state.

The names intentionally contain no experiment-tracker vocabulary. In-memory tests prove the trace hierarchy, metrics, and content-free attributes. Visibility in Tempo, Prometheus, and Grafana remains runtime evidence and requires an explicitly started observability stack.

## Cost re-baseline boundary

The committed baseline was inherited from the last fully attributed behavioral-reference run. It is schema-valid but is not comparable with the direct-load Go eval contract and is not evidence of a Go-agent run. An exact-source `eval:policy-trial` must produce `cost-observed.json`; a maintainer must review its model digest, source identity and tree digest, evalset digest, platform, context, serving version, temperature, prompt selection, repeat variance, and positive usage before replacing `cost_baseline.json`. Offline validation never claims that model-backed proof.
