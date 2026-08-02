"""Unit tests for the bounded read-only incident workflow (Ch. 3.5)."""

import hashlib

import pytest

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


def test_no_step_wires_its_own_policy_callbacks() -> None:
    """Policy is the app's job, not each step's.

    Asserting the *absence* of per-agent wiring is what keeps the guarantee honest: if a step
    ever re-attaches its own callbacks, the app-level plugin would run twice (double-counting
    tokens, double-redacting) and the duplication this refactor removed would creep back.
    """
    for step in _STEPS:
        assert step.model
        assert step.before_model_callback is None
        assert step.after_model_callback is None
        assert step.before_tool_callback is None
        assert step.after_tool_callback is None
        assert step.on_model_error_callback is None
        assert step.on_tool_error_callback is None


@pytest.mark.parametrize(
    ("step", "expected"),
    [
        (plan, "290cfa7d7b04aa3321ec27f0089fc1771fc5ac739c98b32f3dc25f08cddf9129"),
        (investigate, "05b2f9cfa2ba5882b819eb3eda715e28215e40cd36a5b821442d07e52c0bc27f"),
        (evidence_review, "506d4bf2bc6a5ca7061e87f81132ad3b0186a8edb67519235c3683472ff4125c"),
        (recommend, "60f917ec16911ac8a5cb48c57ffdf3ed6f8ca7ee85c9afbd30790ad2d7b9e2a1"),
    ],
)
def test_workflow_prompt_contracts(step, expected: str) -> None:
    digest = hashlib.sha256(str(step.instruction).encode()).hexdigest()
    assert digest == expected, "prompt changed — re-run `mise run eval` and update the hash after reviewing"


def test_workflow_eval_dependencies_remain_explicit() -> None:
    assert "plan controls which evidence to collect; it is not evidence" in str(investigate.instruction)
    assert "unfiltered service logs with search_service_logs" in str(evidence_review.instruction)
    assert "never call either action" in str(recommend.instruction)
