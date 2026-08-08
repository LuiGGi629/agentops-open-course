# Phase 0 and Phase 1 — evidence record

Report for [CONTINUE.md](../prompts/CONTINUE.md) Phase 0 (enforce the Go module) and Phase 1 (make the Go agent the deployed agent). Every result below was produced on 2026-08-08 on Linux with Docker 29.6.2, Ollama 0.32.6, Go 1.26.5, golangci-lint 2.12.2, and agentgateway 1.4.1.

## Phase 0 — enforcement

| Item                | What shipped                                                                                                                                                              |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Toolchain pins      | `go`, `golangci-lint`, `gotestsum` in the **root** `mise.toml` `[tools]` — see the deviation note below                                                                   |
| Root aggregates     | `install`, `format:core`, `check:source`, and `test` each gained a Go child (`format:go`, `check:go`, `test:go`); `test` became an aggregate of `test:go` + `test:python` |
| Vulnerability owner | `check:vuln` fans out to `check:vuln:go` (govulncheck) and `check:vuln:python` (pip-audit); `scripts/check-vulnerabilities.sh` records the Go exemption in its header     |
| Hooks               | `format-go` and `check-go` added; the dead `check-docs` glob `{docs/**,mkdocs.yml}` replaced with the Hugo reality                                                        |
| CI                  | `mise run check` already carries `check:vuln`, so the govulncheck gate arrives with the aggregate; a Go module/build cache step was added beside the existing uv cache    |
| Dependabot          | `gomod` for `/agents/go` and `/`, plus `docker` for `/agents/go`                                                                                                          |

**Deviation from the work order.** CONTINUE.md §6 item 1 asks for a `[tools]` table in `agents/go/mise.toml`. The pins went into the **root** `mise.toml` instead, because mise merges parent configs (verified: with the global catalog removed via `MISE_CONFIG_DIR`, `cd agents/go &&
mise ls --current` resolves all three from the repository), and one table keeps three properties the split would have lost: `hugo mod get` needs `go` for the Hextra module and had no in-repo pin at all; `scripts/freshness_report.py` reads only the root `[tools]` when it reports drift; and there is one `mise.lock` rather than two. The stated goal — a clean clone must not depend on the owner's global mise config — is met either way.

**Finding fixed while wiring.** The new `check:vuln:go` gate immediately failed on **GO-2026-6061** in `google.golang.org/grpc@v1.81.0`, reachable from `config.Check` and `telemetry.RecordSchemaFailure` through the OTel exporters — exactly the transitive case MIGRATE §4.4 item 4 describes. Fixed by bumping the flagged module (`go get google.golang.org/grpc@latest` → v1.83.0), which minimal version selection carried to OTel 1.44.0. That deprecated `attribute.Value.Emit`, so three telemetry test call sites moved to the upstream replacement `Value.String` (identical for the scalar attribute types those tests read).

## Phase 1 — the deployed agent

### Gateway (C-5)

`pathOverride` is now `/v1/responses` in `infra/agentgateway/{host/config.yaml,host/config-auth.yaml,k3d/config.yaml}`.

`pathOverride` alone was **not sufficient** and this is the load-bearing finding: agentgateway classifies the _client_ route from the inbound path, and without an explicit override it assumed chat-completions and rejected every Responses request with `failed to parse request: missing field
"messages"` (HTTP 503). The fix is the `ai.routes` map — `"/v1/responses": responses` — which is the "responses route type" MIGRATE §5.3 names. With it, the same three files parse the Responses body, and the guards that depend on that parse work again.

Measured through agentgateway 1.4.1 against Ollama 0.32.6 and `qwen3:4b-instruct`, using the shipped `:4000` bind copied verbatim:

| Behaviour                     | Result                                                                           |
| ----------------------------- | -------------------------------------------------------------------------------- |
| Responses request proxied     | HTTP 200, `status: completed`, correct `model`                                   |
| Token usage metadata (M7/M9)  | `gen_ai.usage.input_tokens=14`, `gen_ai.usage.output_tokens=5` in the access log |
| Request guard — email         | HTTP 400 `Request rejected by the course prompt guard.`                          |
| Request guard — injection     | HTTP 400 `Request rejected by the course prompt guard.`                          |
| **502 data-loss guard (C-5)** | HTTP 502 `Model response rejected by the course data-loss guard.`                |

The `gke` profile was left untouched per C-5. **Open risk, owner input needed:** the Go agent posts `/v1/responses` to whichever gateway it is pointed at, including the gke one, so that profile very likely needs the same `ai.routes` override (it still needs **no** `pathOverride` — Vertex never speaks the OpenAI-compatible path). Whether agentgateway 1.4.1 can translate a Responses-shaped request to Vertex at all is unverified: it needs `mise run gke:smoke` against a real project, which is owner-gated (§14 decision 4).

### Container (C-4) — the spike chose the fallback

`ko` was built and inspected (`ko build --local`, 31 MB, `cgr.dev/chainguard/static`, uid 65532). It fails **both** contracts C-4 required it to satisfy:

1. **No image environment variable.** `ko`'s config surface is `defaultBaseImage`, `baseImageOverrides`, `builds[].{main,dir,env,flags,ldflags,linkmode}`, `defaultPlatforms`, `defaultFlags`, `defaultLdflags` — none of which sets a runtime `Env`. The produced config carries exactly `PATH`, `SSL_CERT_FILE`, and `KO_DATA_PATH`. `AGENT_SOURCE_COMMIT` has to be readable from the process environment because `agents/go/state/cli.go` stamps it into every backup manifest and `infra/scripts/backup-state.sh` documents that a release image injects it.
1. **No default `CMD`.** `ko` produces `Entrypoint: ["/ko-app/agent"]` and `Cmd: null`, so the image with no arguments starts ADK's interactive console. The kagent `type: BYO` Agent in `infra/kagent/agent.yaml` has no `command` field at all, so the image's own default must be `a2a`.

A third friction, not disqualifying on its own: `ko` ships static assets only from a `kodata` directory inside the Go package, while the dataset is the sibling `agents/data` tree that every manifest already mounts at `/app/data`.

So `agents/go/Dockerfile` ships: two stages, `golang:1.26.5-alpine` → `cgr.dev/chainguard/static`, both digest-pinned, `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`, `USER 10001:10001`, `ENTRYPOINT ["/app/agent"]`, `CMD ["a2a"]`. **16 MB** — Chapter 6.1's "tens of megabytes" story holds. Every surface the manifests need was exercised against the built image:

| Surface                                                                        | Result                                                            |
| ------------------------------------------------------------------------------ | ----------------------------------------------------------------- |
| `config:check` under `--read-only` + tmpfs (the CI smoke shape)                | exit 0, full masked configuration printed                         |
| Default entrypoint, no command (the kagent BYO shape)                          | A2A on `:8080`, agent card served, `/healthz` 200                 |
| `mcp` with `MCP_TRANSPORT=streamable-http`, no state                           | `/livez` 200, `/healthz` 503 — correct: it is not the state owner |
| `mcp` against the shared state volume mounted read-only (the `mcp.yaml` shape) | `/livez` 200, `/healthz` 200                                      |
| `state backup --state-dir --backup-root --keep 7` (the CronJob's exact args)   | published snapshot with `manifest.json` and `.complete`           |

`mcp.yaml` and `state-backup.yaml` now override `args` only, never `command`: the entrypoint stays the agent binary, so a manifest cannot introduce a second, untested CLI. `scripts/check-infra.sh` asserts that (`.command` must be unset) and `scripts/container-matrix.json` replaces the `--entrypoint python` import smoke with the `config:check` subcommand, since the distroless image has neither interpreter nor shell.

### Host smoke and the state CLI

`mise run smoke:host` is **green against the Go agent**: fake model, MCP through the gateway, A2A through the gateway, CORS allow and deny, host and in-container gateway metrics, readiness, and teardown. The retarget replaced three embedded Python programs — the MCP client check is now a Go program compiled against the module (so the expected tool set is asked of `compose.MCPReadToolNames()` rather than repeated in the script), and the model assertion posts `/v1/responses`. Everything the script still asks Python for (port arbitration, the loopback relay, `load/fake_model.py`) now uses the repository's own `.venv`, not a reference agent's.

`load/fake_model.py` now serves `/v1/responses` with a real Responses body, because that is the only path the taught runtime speaks.

`mise run state:drill` is green against the Go state CLI, including the injected second-file publication rollback — the drill now compiles a small program against `state.RestoreOptions.BeforePublish`, the seam the restore already carried for exactly this proof. `infra/scripts/{backup,restore}-state.sh` run `go run ./cmd/agent state …`, and `scripts/lib.sh` gained `absolute_path` so a caller-relative path survives the `cd` into the module.

### The traceability contract was not actually enforced

`AGENTS.md` and Chapter 6.2 both claimed `infra/skaffold.yaml` "refuses an untraceable image build". It did not, and had not before this work either — the mechanism is Skaffold's Go-template rendering of `buildArgs`, which is Dockerfile-independent. Measured: with `AGENT_SOURCE_COMMIT` unset, `skaffold build` **succeeded** and produced an image carrying `AGENT_SOURCE_COMMIT=<no value>` and `org.opencontainers.image.revision=<no value>` — Skaffold renders an unset template key as that literal string rather than failing.

C-4 named this as one of the two contracts the container work must satisfy, so the guard now exists where the value actually lands: the last layer of the Dockerfile's build stage, after the compile so the build cache is untouched. It rejects `""` and `<no value>` and accepts the `unknown` default, which is what the bare `docker build` documented in Chapter 6.1 produces. Verified across all three paths:

| Build                                               | Before    | After                            |
| --------------------------------------------------- | --------- | -------------------------------- |
| `skaffold build` with no `AGENT_SOURCE_COMMIT`      | succeeded | **fails**, naming the variable   |
| `skaffold build` with the commit                    | succeeded | succeeds, revision label correct |
| `docker build` with no build argument (Chapter 6.1) | `unknown` | `unknown`, unchanged             |

Both prose claims were corrected to say where the refusal lives and why.

### The HITL round trip: wire proven, model-backed run blocked

`clients/web` depends on four ADK-Python wire details. All four are ADK Go's too, read from the installed module: `adk_type` = `function_call`/`function_response` and `adk_is_long_running` are `server/adka2a/v2/parts.go`'s own constants, and `adk_request_confirmation` is `tool/toolconfirmation.FunctionCallName`. The exact round trip the client performs — pause at `input-required`, the confirmation call carrying `originalFunctionCall`, the `{"confirmed": true, "payload": {…}}` reply with `metadata.adk_type = "function_response"`, resume on the same `taskId`/`contextId` to `completed`, and exactly one approved tool run — is proven deterministically by `TestGuardedActionPausesForConfirmationAndResumes` in `agents/go/a2aserver/protocol_test.go`.

Driving that same sequence with the real model through the host gateway got as far as: agent-card discovery through the gateway, **CRLF** SSE framing (the framing the client's splitter expects), streaming partials carrying `adk_partial` and `adk_custom_metadata.openai_response_id`, and one real `get_incident` function call with correct `promptTokenCount`/`candidatesTokenCount`. It then failed with `adk_error_code: MODEL_UNAVAILABLE`. That is a host-capacity limit, not a defect: this machine has no GPU, and at a load average of ~43 on 16 cores Ollama served roughly 5 tokens/second, so a single ~2,900-token model call took about 9 minutes and exceeded `AGENT_MODEL_TIMEOUT_S` (whose ceiling is 600). The agent's resilience path reported the timeout correctly and the task ended `failed` rather than hanging.

### The k3d platform path cannot run on this host

`mise run cluster:start` refused: `cgroup v2 required for pinned Kubernetes`. The host mounts the legacy cgroup v1 hierarchy (`/sys/fs/cgroup/cgroup.controllers` is absent), and k3s v1.36.2 requires v2. The repository's own preflight (`require_cgroup_v2` in `scripts/lib.sh`) behaved exactly as designed — fail fast rather than create a partial cluster. Nothing was created and the kube context was left untouched.

What could still be verified without a cluster: both overlays render and pass `kubeconform` and `kube-linter`, the CronJob's `args` shape is asserted, and Skaffold resolves the retargeted `go/Dockerfile` artifact.

## Verification

From the repository root, all green and warning-free:

```bash
mise run format      # dprint, gofumpt/goimports, ruff, shfmt, tofu
mise run check       # + check:go, check:vuln:go, check:infra, check:docs
mise run test        # test:go (1346 tests) + test:python (674 tests, 95.14% branch)
mise run build:docs  # 88 pages
mise run smoke:host  # against the Go agent through agentgateway
mise run state:drill # against the Go state CLI
```

### `platform.yml`

Retargeted, but **unvalidated** — it is a 60-minute GitHub-hosted cluster job and this host cannot run k3d at all. Two steps used `kubectl exec … python` inside the agent pod, which a distroless image cannot serve. Both now use `kubectl debug` ephemeral containers on the same pod: the gateway-egress probe inherits the pod's network identity and therefore its NetworkPolicy, and the capture-defaults probe reads `/proc/1/environ` through the process namespace that `--target` shares. The injected `TELEMETRY_LOG_MARKER` canary became `TELEMETRY_LOG_LINE: serving A2A` — a log line the agent genuinely emits through the slog-to-OTLP bridge — because the distroless image has no interpreter to inject a canary with. The privacy assertions (capture defaults false, the sentinel prompt absent from traces and logs) are unchanged.

## Not done in Phase 1

1. **`mise run platform:dev` on k3d** and everything downstream of a running cluster: the deployed HITL round trip, Tempo trace/log correlation in Grafana, and the kagent BYO restart-survival check through the deployed path. Blocked by cgroup v1 on this host; needs a cgroup v2 machine.
1. **`platform.yml` is retargeted but unrun.** The ephemeral-container probes are the part most likely to need adjustment on first execution.
1. **The k6 re-baseline in `load/`.** The budgets are latency thresholds, and a host at load ~43 would record flattering-or-punitive numbers that mean nothing.
1. **The model-backed HITL run.** The wire contract is proven by the Go suite; what is missing is a real model reaching `restart_service`, which needs a host that can serve the model faster than one call per nine minutes.
