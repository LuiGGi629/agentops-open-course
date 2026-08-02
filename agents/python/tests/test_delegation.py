"""Unit tests for multi-agent delegation and per-agent least privilege (Ch. 3.6)."""

import hashlib

import pytest

from agent.delegation import coordinator_agent, diagnosis_agent, remediation_agent

_WRITE_TOOLS = {"restart_service", "resolve_incident"}
_READ_TOOLS = {"list_incidents", "get_incident", "get_service_status", "search_service_logs"}
_KNOWLEDGE = {"get_runbook", "search_runbooks"}


def _tool_names(agent) -> set[str]:
    return {getattr(tool, "name", None) or getattr(tool, "__name__", "") for tool in agent.tools}


def test_coordinator_delegates_to_both_specialists() -> None:
    assert coordinator_agent.name == "coordinator_agent"
    assert coordinator_agent.sub_agents == [diagnosis_agent, remediation_agent]


def test_diagnosis_specialist_is_grounded() -> None:
    assert diagnosis_agent.name == "diagnosis_agent"
    assert _tool_names(diagnosis_agent) >= _READ_TOOLS


def test_delegation_respects_tool_boundaries() -> None:
    """Least privilege by construction: each specialist physically lacks the other's tools."""
    diagnosis_tools = _tool_names(diagnosis_agent)
    remediation_tools = _tool_names(remediation_agent)
    # The diagnosis agent cannot invoke write actions — it does not hold them.
    assert diagnosis_tools & _WRITE_TOOLS == set()
    # The remediation agent cannot read raw logs or runbooks — it does not hold them.
    assert remediation_tools & (_READ_TOOLS | _KNOWLEDGE) == set()
    assert remediation_tools == _WRITE_TOOLS
    # The coordinator itself holds no write tools either: acting requires delegation.
    assert _tool_names(coordinator_agent) & _WRITE_TOOLS == set()


def test_remediation_actions_still_require_confirmation() -> None:
    """Least privilege does not replace HITL: the guarded actions stay guarded."""
    for tool in remediation_agent.tools:
        assert getattr(tool, "_require_confirmation", None) is True  # the HITL contract


@pytest.mark.parametrize(
    ("agent", "expected"),
    [
        (coordinator_agent, "b560e1a88ad0db21d6d4c75844576f82f98b3725692be68fcfdbcd24a0b41ffb"),
        (diagnosis_agent, "d60847d5b91668cffb2d307e684dc02a390120bdfed9622dea4f9cbea5eb6b29"),
        (remediation_agent, "9e079bc4f25ba9d07426d48159e28f59afb1307abe3a15f5ea5243f2977d3cff"),
    ],
)
def test_delegation_prompt_contracts(agent, expected: str) -> None:
    digest = hashlib.sha256(str(agent.instruction).encode()).hexdigest()
    assert digest == expected, "prompt changed — re-run `mise run eval` and update the hash after reviewing"


def test_coordinator_eval_dependencies_remain_explicit() -> None:
    remediation_instruction = str(remediation_agent.instruction)
    coordinator_instruction = str(coordinator_agent.instruction)
    diagnosis_instruction = str(diagnosis_agent.instruction)
    assert "never claim service recovery from the action response" in remediation_instruction
    assert "ADK creates its confirmation request" in remediation_instruction
    assert "delegate back to diagnosis_agent" in coordinator_instruction
    assert "post-action verification" in diagnosis_instruction
