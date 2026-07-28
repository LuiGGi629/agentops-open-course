"""Unit tests for the bounded read-only incident workflow (Ch. 3.5)."""

from agent.budget import enforce_token_budget, record_token_usage
from agent.compaction import compact_history
from agent.guardrails import handle_model_error, handle_tool_error, secure_tool_output
from agent.pii import redact_request_pii, redact_response_pii
from agent.workflow import evidence_review, investigate, plan, recommend, triage_workflow

_STEPS = (plan, investigate, evidence_review, recommend)
_WRITE_TOOLS = {"restart_service", "resolve_incident", "save_incident_note"}


def _tool_names(agent) -> list[str]:
    return [getattr(tool, "name", None) or getattr(tool, "__name__", "") for tool in agent.tools]


def test_workflow_chains_four_steps_in_order() -> None:
    assert triage_workflow.name == "triage_workflow"
    assert [step.name for step in _STEPS] == ["plan", "investigate", "evidence_review", "recommend"]
    source, *steps = triage_workflow.edges[0]
    assert source == "START"
    assert steps == list(_STEPS)


def test_workflow_uses_stage_specific_read_only_tools() -> None:
    assert _tool_names(plan) == []
    assert _tool_names(investigate) == [
        "get_incident",
        "get_service_status",
        "search_service_logs",
        "get_runbook",
        "list_incidents",
    ]
    assert _tool_names(evidence_review) == [
        "get_incident",
        "get_service_status",
        "search_service_logs",
        "get_runbook",
    ]
    assert _tool_names(recommend) == ["get_runbook"]
    for step in _STEPS:
        assert set(_tool_names(step)).isdisjoint(_WRITE_TOOLS)


def test_each_step_keeps_the_runtime_safety_callbacks() -> None:
    for step in _STEPS:
        assert step.model
        assert step.before_model_callback == [enforce_token_budget, compact_history, redact_request_pii]
        assert step.after_model_callback == [record_token_usage, redact_response_pii]
        assert step.after_tool_callback is secure_tool_output
        assert step.on_model_error_callback is handle_model_error
        assert step.on_tool_error_callback is handle_tool_error


def test_instructions_bound_planning_review_and_recommendation() -> None:
    assert "at most four bullets" in str(plan.instruction)
    assert "Do not invent symptoms, causes, systems, time windows, hypotheses" in str(plan.instruction)
    assert "exact runbook linked by the incident record" in str(plan.instruction)
    assert "plan controls which evidence to collect; it is not evidence" in str(investigate.instruction)
    assert "filtering by the exact named service when present" in str(investigate.instruction)
    assert "no query filter" in str(investigate.instruction)
    assert "substitute fuzzy runbook search" in str(investigate.instruction)
    assert "Preserve the exact incident id, service, and runbook slug in the handoff" in str(investigate.instruction)
    assert "observed service status, unfiltered log evidence" in str(investigate.instruction)
    assert "claims, not source truth" in str(evidence_review.instruction)
    assert "unfiltered service logs with search_service_logs" in str(evidence_review.instruction)
    assert "missing or conflicting evidence" in str(evidence_review.instruction)
    assert "exact incident id, service, runbook slug" in str(evidence_review.instruction)
    assert "at most four key observations" in str(evidence_review.instruction)
    assert "never return an opaque error" in str(evidence_review.instruction)
    assert "at most three runbook-backed next steps" in str(recommend.instruction)
    assert "preserving the exact incident and service" in str(recommend.instruction)
    assert "never discover an alternative" in str(recommend.instruction)
    assert "never call either action" in str(recommend.instruction)
