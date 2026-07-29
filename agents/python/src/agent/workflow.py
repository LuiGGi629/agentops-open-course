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
from .memory import GET_RUNBOOK_TOOL
from .model import build_generation_config, build_model
from .pii import redact_request_pii, redact_response_pii
from .telemetry import setup_telemetry
from .tools import GET_INCIDENT_TOOL, GET_SERVICE_STATUS_TOOL, LIST_INCIDENTS_TOOL, SEARCH_SERVICE_LOGS_TOOL

setup_telemetry()

# Each stage receives only the exact reads it needs. Review may verify named
# sources, and recommendation may reload the handed-off runbook; neither can
# reopen discovery and drift to a different target.
_INVESTIGATION_TOOLS: list[ToolUnion] = [
    GET_INCIDENT_TOOL,
    GET_SERVICE_STATUS_TOOL,
    SEARCH_SERVICE_LOGS_TOOL,
    GET_RUNBOOK_TOOL,
    LIST_INCIDENTS_TOOL,
]
_REVIEW_TOOLS: list[ToolUnion] = [
    GET_INCIDENT_TOOL,
    GET_SERVICE_STATUS_TOOL,
    SEARCH_SERVICE_LOGS_TOOL,
    GET_RUNBOOK_TOOL,
]

# 1) Plan: turn the request into a small, observable investigation contract.
plan = Agent(
    model=build_model(),
    generate_content_config=build_generation_config(),
    name="plan",
    description="Defines a concise evidence plan for one incident investigation.",
    instruction=(
        "Turn the request into an investigation plan with at most four bullets. Preserve the exact "
        "incident or service named by the user. If none is named, plan to select the most urgent "
        "unresolved incident. Use only this evidence frame: incident record, affected service "
        "status, service logs, and the exact runbook linked by the incident record. Do not invent "
        "symptoms, causes, systems, time windows, hypotheses, or recovery facts absent from the "
        "request. Do not diagnose or recommend. State the condition for stopping or escalating."
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
    generate_content_config=build_generation_config(),
    name="investigate",
    description="Collects read-only evidence for the planned incident investigation.",
    instruction=(
        "The plan controls which evidence to collect; it is not evidence. Execute it for one "
        "incident. If it has no incident id, call list_incidents, filtering by the exact named "
        "service when present, and select the most urgent unresolved incident (lowest SEV number). "
        "Call get_incident for the exact id. Derive the service and runbook slug only from that "
        "record, then call get_service_status with the exact service, search_service_logs with that "
        "service and no query filter, and get_runbook with the exact linked slug, in that order. Do "
        "not introduce another domain or substitute fuzzy runbook search. Return only concise "
        "observed evidence with source names; label any inference and stop plainly when required "
        "evidence is missing. Preserve the exact incident id, service, and runbook slug in the "
        "handoff, together with observed service status, unfiltered log evidence, and relevant "
        "runbook guidance."
    ),
    tools=_INVESTIGATION_TOOLS,
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# 3) Evidence review: challenge the investigation before advice is produced.
evidence_review = Agent(
    model=build_model(),
    generate_content_config=build_generation_config(),
    name="evidence_review",
    description="Checks whether incident evidence supports a safe recommendation.",
    instruction=(
        "Treat the handed-off investigation as claims, not source truth. When needed to resolve one "
        "material gap, re-read only its exact incident with get_incident, service with "
        "get_service_status, unfiltered service logs with search_service_logs, or runbook slug with "
        "get_runbook; never discover or invent a replacement. Separate observations from "
        "inferences and name missing or conflicting evidence. Return a compact handoff containing "
        "the exact incident id, service, runbook slug, at most four key observations, remaining "
        "gaps, and a supported, insufficient, or conflicting verdict with one short reason. If an "
        "exact identifier or required source is absent, return an explicit insufficient verdict "
        "and name it; never return an opaque error. Do not recommend or take an action."
    ),
    tools=_REVIEW_TOOLS,
    before_model_callback=[enforce_token_budget, compact_history, redact_request_pii],
    after_model_callback=[record_token_usage, redact_response_pii],
    after_tool_callback=secure_tool_output,
    on_model_error_callback=handle_model_error,
    on_tool_error_callback=handle_tool_error,
)

# 4) Recommend: propose a bounded next step only after evidence review.
recommend = Agent(
    model=build_model(),
    generate_content_config=build_generation_config(),
    name="recommend",
    description="Recommends concrete, runbook-backed remediation.",
    instruction=(
        "Use only the evidence-review handoff and the exact runbook slug it names; never discover "
        "an alternative. Recommend only what its verdict supports. If the verdict is insufficient "
        "or conflicting, ask for the missing check instead of proposing a write. Otherwise call "
        "get_runbook with the handed-off slug, then give at most three runbook-backed next steps, "
        "the expected recovery evidence, and a rollback or stop condition, preserving the exact "
        "incident and service from the handoff. Flag restart_service or resolve_incident as "
        "requiring human approval; never call either action. Cite the handed-off runbook slug."
    ),
    tools=[GET_RUNBOOK_TOOL],
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
