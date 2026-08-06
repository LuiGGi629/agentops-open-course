"""Offline format and agreement tests for the labeled judge calibration set."""

from pathlib import Path
from types import SimpleNamespace

from evals import judge_calibration

_SET = Path(__file__).parents[1] / "evals" / "judge-calibration.json"


def test_calibration_set_is_balanced_and_well_formed() -> None:
    cases = judge_calibration.load_cases(_SET)
    assert len(cases) == 12
    assert {case["category"] for case in cases} == {"good", "bad", "hallucinated"}
    assert {case["expected_pass"] for case in cases} == {True, False}


def test_agreement_counts_labeled_verdicts() -> None:
    cases = judge_calibration.load_cases(_SET)
    expected_by_answer = {case["answer"]: case["expected_pass"] for case in cases}

    def judge(*, outputs, **_kwargs):
        return SimpleNamespace(value=expected_by_answer[outputs["responses"][0]])

    matches, total = judge_calibration.agreement(cases, judge)
    assert (matches, total) == (12, 12)
