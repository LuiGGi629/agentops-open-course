# AgentOps Reference Agent (Python)

The **AgentOps Agent** is the executable reference for the [AgentOps Open Course](../../docs/index.md). It uses Google ADK for the agent runtime, Pydantic for trusted boundaries, MCP and A2A for interoperability, SQLite for deterministic local state, and OpenTelemetry for runtime signals.

## Quickstart

Install and verify without a model:

```bash
mise run install
mise run check
mise run test
```

The test suite is deterministic, network-independent after installation, and enforces at least 95% branch coverage.

The defaults are enough for the account-free Qwen3 path. With Ollama running:

```bash
ollama pull qwen3:4b-instruct
mise run run
```

When Chapter 5 introduces agentgateway, change only the endpoint:

```bash
AGENT_MODEL_PROVIDER=openai-compatible
AGENT_MODEL=qwen3:4b-instruct
OPENAI_BASE_URL=http://127.0.0.1:4000/v1
OPENAI_API_KEY=local-ollama
AGENT_MCP_URL=http://127.0.0.1:3000/mcp
```

Native Gemini remains an optional provider path with `AGENT_MODEL_PROVIDER=gemini` plus AI Studio credentials or Vertex ADC.

## Runtime contracts

- The lazy `src/agent` package exposes one `root_agent` discovery boundary without initializing ADK on a plain import.
- `AGENT_ENTRYPOINT=agent|workflow|coordinator` selects the composition; the typed configuration rejects every other value.
- The default instruction plans multi-step investigations and re-reads state before claiming an approved action succeeded.
- `src/agent/structured_report` exposes the schema-validated report agent used by `eval:report`.
- The workflow selection exposes the bounded, read-only `plan → investigate → evidence_review → recommend` graph.
- The coordinator selection exposes the least-privilege coordinator and its two specialists.
- `python -m agent.server` binds A2A to `127.0.0.1:8080` by default and advertises `localhost:8080`; deployments can configure those addresses independently.
- Sessions and A2A tasks use persistent SQLite services under `.state/`.
- `AGENT_DATA_DIR` points to immutable seed data; `AGENT_STATE_DIR` holds its writable runtime copy.
- `AGENT_MODEL_PROVIDER=openai-compatible` is the account-free default; `OPENAI_BASE_URL` chooses direct Ollama or agentgateway.
- `AGENT_MODEL_PROVIDER=gemini` selects the optional native Gemini/Vertex integration.
- `AGENT_MCP_URL` routes the six read/runbook tools over streamable HTTP; without it, the composition registers their local in-process implementations. `mise run mcp` starts the standalone server.
- HTTP MCP keeps DNS-rebinding protection enabled; `MCP_ALLOWED_HOSTS` can narrow its explicit authority allowlist.
- message content capture in telemetry is disabled by default.

## Layout

```text
src/
  agent/
    __init__.py     Lazy root_agent package boundary
    composition.py  Validated entrypoint selection and default composition
    budget.py       Token accounting and per-session budget
    config.py       Typed environment settings
    config_check.py Masked effective-configuration diagnostic
    model.py        OpenAI-compatible local/gateway or optional Gemini model
    models.py       Trusted domain and tool boundary types
    data.py         Seed-to-runtime state and data access
    tools.py        Read-only incident, service, and log tools
    skills.py       Allowlisted skill discovery and loading
    mcp_server.py   stdio or streamable HTTP MCP server
    mcp_client.py   ADK MCP toolset selection
    longterm.py     Explicit cross-session incident notes
    compaction.py   Bounded conversation-history compaction
    memory.py       Runbook retrieval
    retrieval.py    Optional local semantic retrieval
    report.py       Schema-validated triage report
    structured_report/ ADK discovery package for the report evaluation
    resilience.py   Read/model deadlines and retry policy
    circuit.py      Deterministic clock-injectable circuit breaker
    workflow.py     Bounded planning and evidence-review workflow
    delegation.py   Least-privilege specialist delegation
    guardrails.py   Input and action policy
    actions.py      Approved writes and append-only audit
    pii.py          Presidio request/response/tool-output callbacks
    telemetry.py    Privacy-preserving OpenTelemetry setup
    server.py       Persistent A2A application factory and process
evals/            ADK trajectories and MLflow evaluation
tests/            Offline unit and local integration tests
```

The default path stays the simplest:

```bash
mise run run
```

Use an optional entrypoint only when its composition matches the task:

```bash
mise run workflow    # fixed plan and one evidence-review pass
mise run coordinator # model-routed specialist delegation
```

All three tasks call the same working `adk run src/agent` command. The two alternatives set the validated `AGENT_ENTRYPOINT` selector before ADK requests the package-level `root_agent`.

## Tasks

| Task                     | Network/model use                  | Purpose                                                                   |
| ------------------------ | ---------------------------------- | ------------------------------------------------------------------------- |
| `mise run format`        | None                               | Format imports and Python.                                                |
| `mise run check`         | Vulnerability database may refresh | Check metadata, lock, format, lint, types, and dependencies.              |
| `mise run test`          | None                               | Run branch-covered offline tests.                                         |
| `mise run redteam`       | None                               | Run deterministic adversarial regression cases; not a live-model scanner. |
| `mise run run`           | Model                              | Run the ADK terminal UI.                                                  |
| `mise run workflow`      | Model                              | Run the bounded read-only planning workflow.                              |
| `mise run coordinator`   | Model                              | Run the least-privilege specialist coordinator.                           |
| `mise run web`           | Model                              | Run the ADK developer UI.                                                 |
| `mise run mcp`           | None                               | Serve MCP over stdio.                                                     |
| `mise run mcp:http`      | None                               | Serve MCP over HTTP at `127.0.0.1:8000`.                                  |
| `mise run a2a`           | Depends on requests                | Serve persistent A2A on port `8080`.                                      |
| `mise run eval`          | Model                              | Run strict ADK trajectories and enforce the aggregate 4B case-pass floor. |
| `mise run eval:workflow` | Model                              | Exercise the bounded workflow's read-only evidence trajectory.            |
| `mise run eval:mlflow`   | Model; judge optional              | Log cases, prompt lineage, scorers, and optional judge results to MLflow. |
| `mise run data:reset`    | None                               | Delete disposable `.state/` and restore on next use.                      |

## Reset and cleanup

```bash
mise run data:reset
mise run clean
```

`data:reset` never deletes or edits `../data/incidents.db`. Do not place durable or sensitive information in the course runtime database.

## License

The code is [MIT licensed](../LICENSE). The bundled course content and external model/provider services have separate licenses and terms.
