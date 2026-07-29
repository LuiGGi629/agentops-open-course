# AgentOps Agent Evaluations

The evaluation layer separates deterministic engineering gates from model-backed behavioral evidence. It never weakens the offline unit tests to accommodate non-deterministic output.

## Evaluation layers

| Layer                  | Command                  | Model required? | Run contract                                                                                   |
| ---------------------- | ------------------------ | --------------- | ---------------------------------------------------------------------------------------------- |
| Unit/integration       | `mise run test`          | No              | Exact typed behavior and at least 95% branch coverage.                                         |
| Adversarial regression | `mise run redteam`       | No              | Deterministic injection, boundary, and policy cases.                                           |
| Evalset consistency    | `mise run eval:validate` | No              | Cases reference committed seed entities and skills; strict in-order trajectory criteria.       |
| ADK trajectory         | `mise run eval`          | Yes             | Strict per-case tools/arguments; at least 25% of the fixed cases must pass on the 4B baseline. |
| Structured report      | `mise run eval:report`   | Yes             | `TriageReport` schema enforcement plus its required read-tool trajectory.                      |
| Bounded workflow       | `mise run eval:workflow` | Yes             | Plan, investigation, evidence-review, and recommendation path over one fixed incident.         |
| MLflow evaluation      | `mise run eval:mlflow`   | Yes             | Isolated state, required code-scorer thresholds, prompt/model lineage, and optional judge.     |
| Cost regression        | `mise run eval:cost`     | Yes             | Per-case token/model-call usage stays within tolerance of `cost_baseline.json` (evidence).     |
| Groundedness           | `mise run eval:ground`   | Yes             | Recognized incident/severity and known service/runbook claims appear in that turn's context.   |

## Run the live evaluations

From `agents/python/`, the default configuration calls Qwen3 through local Ollama. Pull the model once, then run the evaluations:

```bash
ollama pull qwen3:4b-instruct
mise run eval
mise run eval:report
mise run eval:workflow
mise run eval:mlflow
mise run eval:cost
mise run eval:ground
```

To evaluate through agentgateway, change `OPENAI_BASE_URL` to `http://127.0.0.1:4000/v1`; the provider remains `openai-compatible`. Native Gemini is optional through `AGENT_MODEL_PROVIDER=gemini` with AI Studio credentials or Vertex ADC.

Keep `AGENT_MODEL_FALLBACK` unset during every behavioral evaluation. Otherwise one run could silently combine two models while attributing every answer to the primary. Evaluate the alternate separately by making it `AGENT_MODEL`.

The MLflow tracking URI defaults to local SQLite unless `MLFLOW_TRACKING_URI` is set; `MLFLOW_EXPERIMENT_NAME` defaults to `agentops-agent`. Chapter 7 starts the self-hosted server at `http://localhost:5000`. The command prints the authoritative destination and suggests a local `mlflow ui` command only for a `sqlite:` URI, never for an HTTP server.

## Configure an optional MLflow judge

The code-only scorers always run. An LLM judge is opt-in:

```bash
MLFLOW_JUDGE_MODEL=qwen3:4b-instruct
MLFLOW_JUDGE_BASE_URL=http://localhost:4000/v1
MLFLOW_JUDGE_API_KEY=agentgateway
```

All three judge variables are explicit and required together. They do not fall back to the agent's generic `OPENAI_*` settings, because the optional judge must traverse the deliberately configured agentgateway route rather than silently calling direct Ollama or another provider. No evaluation path uses LiteLLM.

Treat judge output as evidence, not truth. Record the judge model and prompt, inspect disagreement, and never use a single model score as the only release criterion.

## Files

- `ops.evalset.json` contains prompts, expected tool trajectories, and reference answers over the fixed dataset — happy paths plus deliberate negative and adversarial cases.
- `triage-report.evalset.json` runs the dedicated structured-output entry point in fresh temporary state; ADK enforces `TriageReport` while the eval checks the evidence-gathering trajectory.
- `workflow.evalset.json` selects the read-only `triage_workflow` through the shared `src/agent` package and checks the expected incident, service, log, and exact linked-runbook evidence path.
- `test_config.json` makes each ADK case strict (`1.0`) with `IN_ORDER` matching. `required_trajectory.py` keeps expected arguments strict as required subsets while allowing optional actual arguments. `run_adk_eval.py` selects and streams one case at a time so a single CPU model is not overloaded by ADK's four-way default, gives each case a fresh temporary state directory, rejects ADK's false-success exit behavior, and applies the separate `0.25` collapse floor for the required local 4B path.
- `mlflow_eval.py` preserves every turn and part in isolated case state. Each case builds a fresh agent and model, runs all of its turns on one disposable async loop, closes any materialized provider client on that same loop, then discards the state. It reuses an identical registered prompt or registers it on a miss, loads an explicitly pinned version when requested, links prompt/model lineage to the run, applies five required deterministic scorers (provider availability, required-subset `IN_ORDER` tools, complete turns, response facts, and exact write policy), enforces their configured floors, and adds an optional explicit-gateway judge. A terminal ADK confirmation request becomes a deterministic input-required response derived only from its guarded original call; the evaluator never approves it or mutates state.
- `cost_eval.py` measures every case, records provider/model-identified token and model-call usage in `cost-observed.json`, and compares it to `cost_baseline.json` (regenerated from real measurements with `--update`, so no counts are committed until you measure them). It catches a correct-but-expensive regression — a prompt or model change that keeps the trajectory scorers green while quietly inflating tokens — that the `IN_ORDER` scorers ignore by design. Direct local Ollama resolves its digest from `/api/tags`; gateway and other providers use an explicit `EVAL_MODEL_DIGEST` when one is available. A different provider, model, or digest, unavailable usage metadata, and a new case require a reviewed baseline; tune growth tolerance with `AGENT_COST_TOLERANCE` (default 0.25).
- `groundedness_eval.py` records the fixed questions, model responses, retrieved evidence, provider failures, and unsupported recognized claims in `ground-observed.json`, so the retained workflow artifact carries the full context behind a console failure. Its narrow deterministic vocabulary covers incident/severity patterns and known course services/runbooks; arbitrary unknown names need a broader scorer or judge.
- `retrieval_eval.py` compares keyword and semantic hit-rate from a fresh temporary vector index, so a runbook or chunking change cannot reuse stale embeddings from `.state/vectors.db`.
- The scheduled workflow captures each full-conversation model result once in `model-observed.json`, then gives that exact transcript to the cost and groundedness checks. The artifact is bound to the provider/model digest, prompt selection, normalized eval contract, and source revision; a new MLflow run removes any prior capture before it starts. Running `eval:cost` or `eval:ground` alone still calls the configured model.
- `../tests/test_evalset.py` is the offline consistency check behind `mise run eval:validate`: every referenced incident, service, runbook, and runtime skill must exist in the committed seed, and the deliberate negatives (`INC-999`, `warehouse`) must stay missing.

ROUGE-style response overlap is intentionally not a hard gate because valid generative wording varies. Tool selection, arguments, policy decisions, and trusted data boundaries are stronger contracts.

## Write a good case

- **One behavior per case.** A case that checks lookup, diagnosis, and approval at once cannot tell you which behavior regressed; split it.
- **Negative cases matter as much as happy paths.** Unknown entities (`INC-999`, `warehouse`), actions that must wait for approval, and injected instructions in tool output are where an agent fails dangerously — an all-happy-path set would score a regression there as green.
- **Assert the trajectory, not the prose.** Expected tool names and required arguments in order are stable across model versions and rewordings; `IN_ORDER` matching tolerates extra calls and optional arguments. State-changing calls are stricter: the MLflow policy scorer requires their exact name, arguments, order, and count.
- **Treat confirmation as a terminal input-required state.** A pending guarded action must say which action/target awaits a rationale and that no state changed; an evaluator must never auto-confirm merely to manufacture a final answer.
- **Grow the set from real failures.** When a trace shows a wrong tool choice or an unsafe proposal, distill it into the minimum conversation that reproduces it, then keep it forever as a regression case.

## Add a regression case

Add regression cases to `ops.evalset.json`; the report and workflow files deliberately retain one canonical specialized case each. Use a stable incident from the committed seed, record the minimum necessary expected trajectory, and avoid credentials or real operational data. Run `eval:validate` first, then `eval` and `eval:mlflow` with an explicit model. Document model/version changes when comparing results over time.

See [4.4. Evaluations](../../../docs/4.%20Quality/4.4.%20Evaluations.md) and [7.0. Reproducibility](../../../docs/7.%20Observability/7.0.%20Reproducibility.md).
