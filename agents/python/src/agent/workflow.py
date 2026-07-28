"""A bounded plan → investigate → evidence review → recommend workflow (Chapter 3.5).

Where ``root_agent`` decides its own steps, a ``Workflow`` makes the operating loop explicit.
Each node receives its predecessor's output as input, and the fixed edge bounds the topology to
four read-only stages. ``Workflow`` is the ADK 2.x graph runtime — it supersedes the classic
``SequentialAgent`` / ``ParallelAgent`` / ``LoopAgent`` (now deprecated) and also expresses
parallel, looping, and dynamic DAGs.
"""

from __future__ import annotations

from google.adk import Agent, Workflow
from google.adk.agents.llm_agent import ToolUnion

from .budget import enforce_token_budget, record_token_usage
from .compaction import compact_history
from .guardrails import handle_model_error, handle_tool_error, secure_tool_output
from .memory import KNOWLEDGE_TOOLS
from .model import build_model
from .pii import redact_request_pii, redact_response_pii
from .telemetry import setup_telemetry
from .tools import ALL_TOOLS

setup_telemetry()

# Every tool in this workflow is read-only. Guarded actions remain on the interactive root agent.
_READ_TOOLS: list[ToolUnion] = [*ALL_TOOLS, *KNOWLEDGE_TOOLS]

# 1) Plan: turn the request into a small, observable investigation contract.
plan = Agent(
    model=build_model(),
    name="plan",
    description="Defines a concise evidence plan for one incident investigation.",
    instruction=(
        "Turn the request into an investigation plan with at most four bullets. Preserve the exact "
        "incident or service named by the user. If none is named, plan to select the most urgent "
        "unresolved incident. State the target, the checks to run, the evidence that would support "
        "recovery, and the condition for stopping or escalating. Do not diagnose or recommend yet."
    ),
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# 2) Investigate: collect the incident, service, logs, and runbook evidence named by the plan.
investigate = Agent(
    model=build_model(),
    name="investigate",
    description="Collects read-only evidence for the planned incident investigation.",
    instruction=(
        "Execute the investigation plan for one incident. If the plan has no incident id, use "
        "list_incidents to select the most urgent unresolved incident (lowest SEV number). Read its "
        "details, service status, relevant logs, and runbook. Return only concise observed evidence "
        "with source names; label any inference and stop plainly when required evidence is missing."
    ),
    tools=_READ_TOOLS,
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# 3) Evidence review: challenge the investigation before advice is produced.
evidence_review = Agent(
    model=build_model(),
    name="evidence_review",
    description="Checks whether incident evidence supports a safe recommendation.",
    instruction=(
        "Review the investigation evidence against the incident, service status, logs, and runbook. "
        "Re-read a source when needed to resolve one material gap. Separate observations from "
        "inferences and name missing or conflicting evidence. Return a compact handoff containing "
        "the exact incident id, service, runbook slug, at most four key observations, remaining "
        "gaps, and a supported, insufficient, or conflicting verdict with one short reason. "
        "Do not recommend or take an action."
    ),
    tools=_READ_TOOLS,
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# 4) Recommend: propose a bounded next step only after evidence review.
recommend = Agent(
    model=build_model(),
    name="recommend",
    description="Recommends concrete, runbook-backed remediation.",
    instruction=(
        "Recommend only what the evidence review supports. If its verdict is insufficient or "
        "conflicting, ask for the missing check instead of proposing a write. Otherwise give at "
        "most three runbook-backed next steps, the expected recovery evidence, and a rollback or "
        "stop condition, preserving the exact incident and service from the handoff. Flag "
        "restart_service or resolve_incident as requiring human approval; never call either "
        "action. Cite the handed-off runbook slug."
    ),
    tools=KNOWLEDGE_TOOLS,
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# The graph is intentionally linear: each output becomes the next node's input.
triage_workflow = Workflow(
    name="triage_workflow",
    description="Runs a bounded plan → investigate → evidence review → recommend loop.",
    edges=[("START", plan, investigate, evidence_review, recommend)],
)
