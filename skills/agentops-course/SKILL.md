---
name: agentops-course
description: Index of the AgentOps patterns for operating LLM agents in production — telemetry, guardrails, resilience, evaluation, token budgets, least privilege, and incident response — with a pointer to the full open-source course. Use when you want an overview of how the AgentOps skills fit together, or where to start operating an agent you have built.
---

# AgentOps Patterns

Getting an agent to answer correctly once is a demo. **AgentOps** is keeping it correct, safe, affordable, and observable as it runs against real traffic. These skills are operational patterns extracted from one completed reference agent, each with a file you can read and a repeatable check or evidence path.

## When to use

- You have built an agent and now have to _operate_ it.
- You want to know which AgentOps skill covers a given concern.
- You want the worked, open-source reference behind these patterns.

## The patterns

Each pattern is a sibling skill, named rather than linked: installing this skill alone puts no siblings on disk, so a relative path would be a dead link. Install one with `npx skills add MLOps-Courses/agentops-open-course --skill <name>`, or read them all at https://github.com/MLOps-Courses/agentops-open-course/tree/main/skills

1. **`agentops-telemetry`** — trace, meter, and log the agent with OpenTelemetry; content off by default.
1. **`agent-guardrails`** — PII redaction, injection spotlighting, human approval on writes, a kill-switch.
1. **`agent-resilience`** — deadlines, bounded retries, a circuit breaker, and a validated model fallback.
1. **`agent-token-budget`** — per-session token ceilings and cost attribution.
1. **`agent-least-privilege`** — split into least-privilege specialists so injection has nothing to call.
1. **`agent-evaluation`** — deterministic validation gates plus model-backed trajectory, grounding, and cost evidence.
1. **`agent-incident-response`** — the detect→triage→mitigate→review→prevent loop for the agent as a workload.

## How they fit together

Build and instrument first (telemetry), harden the boundaries (guardrails, least privilege), bound the failure modes (resilience, token budget), collect behavior evidence (evaluation), and operate the running system (incident response) — feeding every real failure back into the smallest repeatable check or evidence path.

## The full course

These patterns are distilled from the **AgentOps Open Course**, a free, open-source course that builds, evaluates, secures, deploys, and operates one production-shaped agent with an open-source stack (Google ADK, agentgateway, kagent, MLflow, OpenTelemetry, Prometheus, Grafana, Ollama). Repository: `MLOps-Courses/agentops-open-course`. Site: https://agentops-open-course.fmind.dev/
