"""Offline tests for the prompt A/B comparison formatting."""

import sys

import pytest

from evals import prompt_ab
from evals.prompt_ab import format_comparison


def test_format_comparison_reports_candidate_minus_baseline_delta() -> None:
    baseline = {
        "provider_available": 1.0,
        "tool_trajectory": 1.0,
        "complete_conversation": 1.0,
        "response_facts": 0.8,
        "tool_policy": 1.0,
    }
    candidate = {
        "provider_available": 1.0,
        "tool_trajectory": 1.0,
        "complete_conversation": 1.0,
        "response_facts": 1.0,
        "tool_policy": 1.0,
    }
    table = format_comparison("baseline", baseline, "candidate", candidate)
    lines = table.splitlines()
    assert lines[0].split() == ["scorer", "baseline", "candidate", "delta"]
    assert len(lines) == 1 + len(prompt_ab.DETERMINISTIC_SCORERS)
    response_facts_row = next(line for line in lines if line.startswith("response_facts"))
    assert "+0.20" in response_facts_row


def test_score_configured_prompt_includes_provider_availability(monkeypatch) -> None:
    cases = [
        {
            "inputs": {"eval_id": "healthy", "turns": ["status?"]},
            "expectations": {
                "expected_tools": [[]],
                "expected_responses": ["ok"],
                "response_contracts": [
                    {
                        "required_terms": [],
                        "absent_entities": [],
                        "negated_terms": [],
                        "claims": [],
                    }
                ],
            },
        },
        {
            "inputs": {"eval_id": "unavailable", "turns": ["status?"]},
            "expectations": {
                "expected_tools": [[]],
                "expected_responses": ["unavailable"],
                "response_contracts": [
                    {
                        "required_terms": [],
                        "absent_entities": [],
                        "negated_terms": [],
                        "claims": [],
                    }
                ],
            },
        },
    ]
    monkeypatch.setattr(prompt_ab, "_load_cases", lambda: cases)
    monkeypatch.setattr(
        prompt_ab,
        "ask",
        lambda _turns, eval_id: {
            "responses": ["ok"],
            "tools": [[]],
            "provider_errors": (
                [[]] if eval_id == "healthy" else [[{"code": "MODEL_UNAVAILABLE", "message": "failed"}]]
            ),
        },
    )

    scores = prompt_ab.score_configured_prompt()

    assert scores["provider_available"] == 0.5
    assert scores["complete_conversation"] == 1.0


def test_format_comparison_shows_regression_as_negative_delta() -> None:
    baseline = dict.fromkeys(prompt_ab.DETERMINISTIC_SCORERS, 1.0)
    candidate = {**baseline, "tool_policy": 0.5}
    table = format_comparison("baseline", baseline, "candidate", candidate)
    policy_row = next(line for line in table.splitlines() if line.startswith("tool_policy"))
    assert "-0.50" in policy_row


def test_prompt_comparison_rejects_a_mutable_alias_before_spawning() -> None:
    with pytest.raises(SystemExit, match="immutable numeric prompt version"):
        prompt_ab._score_pinned_prompt("prompts:/agentops-agent-instruction@latest")  # noqa: SLF001


def test_main_scores_baseline_before_candidate(monkeypatch, capsys) -> None:
    calls: list[str] = []
    scores = dict.fromkeys(prompt_ab.DETERMINISTIC_SCORERS, 1.0)
    monkeypatch.setattr(prompt_ab, "_score_pinned_prompt", lambda uri: calls.append(uri) or scores)
    monkeypatch.setattr(sys, "argv", ["prompt_ab.py", "prompts:/instruction/1", "prompts:/instruction/2"])

    prompt_ab.main()

    assert calls == ["prompts:/instruction/1", "prompts:/instruction/2"]
    assert capsys.readouterr().out.splitlines()[0].split() == [
        "scorer",
        "prompts:/instruction/1",
        "prompts:/instruction/2",
        "delta",
    ]
