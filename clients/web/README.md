# AgentOps Agent web client

A single-file A2A browser client for the course's AgentOps Agent: one `index.html` with vanilla JavaScript, no build step, no framework, and no external requests (it works offline). It is teaching material for the _client_ side of the [A2A protocol](https://a2a-protocol.org/): agent-card discovery, `message/stream` (SSE) with a `message/send` fallback, task-state rendering, and the human-approval round-trip for guarded actions. MIT licensed (see [`../LICENSE`](../LICENSE)).

## What it does

1. Fetches and displays the agent card from `GET /.well-known/agent-card.json`.
1. Sends messages with `message/stream` when the card advertises streaming, and falls back to `message/send` (a single blocking JSON-RPC response) otherwise.
1. Parses SSE incrementally with CRLF, LF, or CR record separators; the Go A2A server emits LF, and the parser accepts all three.
1. Renders incremental `status-update` and `artifact-update` events, with a distinct badge per task state (`submitted`, `working`, `input-required`, `completed`, `failed`, ...).
1. Tells a provisional model chunk from the finished answer using the `adk_partial` flag ADK sets on each artifact update. Chunks accumulate into one dashed-border bubble; the non-partial artifact — the only text redacted as a whole message — replaces that bubble in place.
1. Surfaces a guarded action (`restart_service`, `resolve_incident`) as an explicit approval form: the task pauses in `input-required`, keeps the preceding evidence/tool results visible, repeats the exact action arguments, and explains that execution re-reads current state while the write transaction validates the target. The reply is a `FunctionResponse` data part carrying `{"confirmed": true, "payload": {"rationale": "..."}}` on the same task. The agent refuses approvals without a rationale, so the form requires one.
1. Cancels an active task through `tasks/cancel`. The Cancel button enables the moment the first frame carries a task id — before the model has produced anything — and the open stream ends with the terminal `canceled` event that ADK's executor publishes. If chunks are already on screen when the cancel lands, they stay marked provisional with a note saying so, because no whole-message redaction ever replaced them.
1. Preserves the current A2A context when Connect is used again, so a transient gateway disconnect does not silently create a new session.

## How to run it

1. In a first terminal, start the A2A server: `cd agents/go && mise run a2a` (raw `:8080`).
1. In a second terminal, start the digest-pinned host gateway wrapper: `mise run gateway:host` from the repository root (loopback A2A route on `:3001`).
1. In a third terminal, serve this directory: `mise run client:web` from the repository root.
1. Open `http://localhost:8001`, keep the base URL `http://localhost:3001`, and press Connect.

Point the client at agentgateway `:3001` — the governed data plane — not the raw application port `:8080`.

## CORS: why the browser needs a gateway policy

The page runs on origin `http://localhost:8001` and calls `http://localhost:3001`, so the browser enforces CORS. Neither the raw A2A server (which deliberately sends no CORS headers — see the `a2aserver` package documentation) nor the a2a/rate-limit policies alone emit CORS headers. The checked-in host and Kubernetes gateway profiles therefore include this route policy:

```yaml
cors:
  allowOrigins:
    - "http://localhost:8001"
  allowMethods:
    - GET
    - POST
    - OPTIONS
  allowHeaders:
    - content-type
```

The gateway answers the preflight itself (`200` with `access-control-allow-*` headers) and stamps `access-control-allow-origin` on card, RPC, and SSE responses. `allowOrigins` is an exact-match list: open the page on the checked-in origin (`http://localhost:8001`, not `http://127.0.0.1:8001`). Do not replace it with a wildcard when adding credentials.

## Limitations

1. Lab-only: no authentication, no TLS, loopback addresses — consistent with the course's no-public-endpoint stance.
1. The default A2A runtime records a synthetic `A2A_USER_<context-id>` approver. This proves confirmation continuity, not authenticated human identity.
1. One conversation per page load; reconnecting without a reload preserves it, but a reload does not list or resume tasks from `.state/runtime.db`.
1. Text parts only (the card advertises `text/plain`); file parts are not rendered.
1. Token-level streaming appears only when the server runs with `AGENT_A2A_STREAMING=true`; by default SSE carries whole events. The default is off for a safety reason, not a performance one — see [3.8. Streaming](../../content/3.%20Capabilities/3.8.%20Streaming.md).
