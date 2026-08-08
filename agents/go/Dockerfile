# syntax=docker/dockerfile:1.11@sha256:10c699f1b6c8bdc8f6b4ce8974855dd8542f1768c26eb240237b8f1c9c6c9976
# AgentOps Agent — container image (Chapter 6.1).
#
# Two stages, no package manager in the result: the Go toolchain compiles one static,
# cgo-free binary, and a distroless base carries only that binary, the bundled dataset,
# and an empty state directory. Both bases are digest-pinned for reproducible builds.
#
# Why a Dockerfile and not `ko`, which is the Go stack's default image builder: `ko` can
# set neither an image environment variable nor a default `CMD`, and this image needs both.
# `AGENT_SOURCE_COMMIT` has to be readable from the process environment because the state
# CLI stamps it into every backup manifest (agents/go/state/cli.go), and the default command
# has to be `a2a` because kagent's `type: BYO` Agent deploys the image with no command of its
# own (infra/kagent/agent.yaml). `ko` also ships static assets only from a `kodata` directory
# inside the Go package, while the dataset is the sibling agents/data tree that the Python
# image and every manifest already mount at /app/data.
#
# The image serves the agent over A2A on :8080 by default — the contract kagent's BYO Agent
# expects (it deploys this image and probes /.well-known/agent-card.json). The MCP server and
# the state CLI are the same binary under different arguments, which is why the manifests
# override `args` and never `command`.
#
# Build context is the repo's agents/ directory, so both go/ and data/ are visible.
# From the repo root:
# docker build -f agents/go/Dockerfile -t agentops-agent:dev agents

# --- Stage 1: compile one static, cgo-free binary ----------------------------
# --8<-- [start:base-images]
FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
# --8<-- [end:base-images]

WORKDIR /src

# The locked module graph first, so the dependency layer stays cached until go.mod or
# go.sum changes rather than on every source edit.
COPY go/go.mod go/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY go/ ./

# --8<-- [start:static-build]
# CGO_ENABLED=0 is a course invariant, not an optimization: it is what lets the binary ship
# on a base with no libc and keeps exactly one SQLite implementation in the process.
# -trimpath removes the builder's absolute paths so two machines produce the same binary;
# -s -w drop the symbol table and DWARF, which debug.ReadBuildInfo does not need.
ENV CGO_ENABLED=0 GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/agent ./cmd/agent
# --8<-- [end:static-build]

# The runtime base has no shell, so the writable state directory is created here and
# copied in with its ownership already set.
RUN install -d -o 10001 -g 10001 /out/state

# --8<-- [start:traceability-guard]
# Where the "refuses an untraceable build" contract is actually enforced. Skaffold renders an
# unset `{{.AGENT_SOURCE_COMMIT}}` as the literal string `<no value>` and builds happily, so
# the image would ship claiming that as its revision — the guard has to live where the value
# lands, and the distroless runtime stage has no shell to check it in.
#
# The bare `docker build` Chapter 6.1 documents passes no build argument at all and keeps the
# `unknown` default: that is an honest local image. An empty or templated-but-unset value is
# not, because something meant to supply a commit and silently did not.
ARG AGENT_SOURCE_COMMIT=unknown
RUN case "${AGENT_SOURCE_COMMIT}" in \
    "" | "<no value>") \
    echo "AGENT_SOURCE_COMMIT resolved to '${AGENT_SOURCE_COMMIT}': refusing to build an untraceable image." >&2; \
    echo "Pass a commit (AGENT_SOURCE_COMMIT=\$(git rev-parse HEAD)) or omit the build argument entirely." >&2; \
    exit 1 ;; \
    esac
# --8<-- [end:traceability-guard]

# --- Stage 2: distroless, non-root runtime -----------------------------------
# --8<-- [start:runtime-base]
FROM cgr.dev/chainguard/static@sha256:24dd7ff8788fdfadda39eeeaefefb6d1cec6002a545935a5f7e017484053734f AS runtime
# --8<-- [end:runtime-base]

ARG AGENT_SOURCE_COMMIT=unknown
ARG OCI_CREATED=1970-01-01T00:00:00Z
ARG OCI_VERSION=dev
LABEL org.opencontainers.image.created="${OCI_CREATED}" \
      org.opencontainers.image.description="Executable Google ADK reference agent for the AgentOps Open Course" \
      org.opencontainers.image.documentation="https://agentops-open-course.fmind.dev/" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.revision="${AGENT_SOURCE_COMMIT}" \
      org.opencontainers.image.source="https://github.com/MLOps-Courses/agentops-open-course" \
      org.opencontainers.image.title="AgentOps Agent" \
      org.opencontainers.image.version="${OCI_VERSION}"

ENV AGENT_SOURCE_COMMIT=${AGENT_SOURCE_COMMIT} \
    AGENT_DATA_DIR=/app/data \
    AGENT_STATE_DIR=/app/state \
    AGENT_A2A_BIND_HOST=0.0.0.0 \
    ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS=false \
    OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=false

WORKDIR /app

COPY --from=build /out/agent /app/agent
COPY --chown=10001:10001 data /app/data
COPY --from=build --chown=10001:10001 /out/state /app/state

USER 10001:10001
EXPOSE 8080

# ENTRYPOINT is the binary and CMD is the surface it serves, so a manifest selects the MCP
# server or the state CLI by overriding `args` alone. A `command:` override would have to
# repeat the binary path and would silently drift from it.
ENTRYPOINT ["/app/agent"]
CMD ["a2a"]
