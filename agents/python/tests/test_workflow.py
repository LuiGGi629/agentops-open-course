"""Unit tests for the bounded read-only incident workflow (Ch. 3.5)."""

from agent.budget import enforce_token_budget, record_token_usage
from agent.compaction import compact_history
from agent.guardrails import handle_model_error, handle_tool_error, secure_tool_output
from agent.pii import redact_request_pii, redact_response_pii
from agent.workflow import evidence_review, investigate, plan, recommend, triage_workflow

_STEPS = (plan, investigate, evidence_review, recommend)
_WRITE_TOOLS = {"restart_service", "resolve_incident", "save_incident_note"}


def _tool_names(agent) -> set[str]:
    return {getattr(tool, "name", None) or getattr(tool, "__name__", "") for tool in agent.tools}


def test_workflow_chains_four_steps_in_order() -> None:
    assert triage_workflow.name == "triage_workflow"
    assert [step.name for step in _STEPS] == ["plan", "investigate", "evidence_review", "recommend"]
    source, *steps = triage_workflow.edges[0]
    assert source == "START"
    assert steps == list(_STEPS)


def test_workflow_is_read_only_and_evidence_grounded() -> None:
    assert plan.tools == []
    for step in _STEPS:
        assert _tool_names(step).isdisjoint(_WRITE_TOOLS)
    for step in (investigate, evidence_review, recommend):
        assert step.tools


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
    assert "missing or conflicting evidence" in str(evidence_review.instruction)
    assert "exact incident id, service, runbook slug" in str(evidence_review.instruction)
    assert "at most four key observations" in str(evidence_review.instruction)
    assert "at most three runbook-backed next steps" in str(recommend.instruction)
    assert "preserving the exact incident and service" in str(recommend.instruction)
    assert "never call either action" in str(recommend.instruction)
