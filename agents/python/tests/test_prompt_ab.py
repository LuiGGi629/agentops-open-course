"""Offline tests for the prompt A/B comparison formatting."""

import sys

from evals import prompt_ab
from evals.prompt_ab import format_comparison


def test_format_comparison_reports_candidate_minus_baseline_delta() -> None:
    baseline = {
        "tool_trajectory": 1.0,
        "complete_conversation": 1.0,
        "response_facts": 0.8,
        "tool_policy": 1.0,
    }
    candidate = {
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


def test_format_comparison_shows_regression_as_negative_delta() -> None:
    baseline = dict.fromkeys(prompt_ab.DETERMINISTIC_SCORERS, 1.0)
    candidate = {**baseline, "tool_policy": 0.5}
    table = format_comparison("baseline", baseline, "candidate", candidate)
    policy_row = next(line for line in table.splitlines() if line.startswith("tool_policy"))
    assert "-0.50" in policy_row


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
