"""Tests for the truthful ADK evaluation process contract."""

import argparse

import pytest

from evals.run_adk_eval import pass_rate, verdict


@pytest.mark.parametrize(
    ("output", "process_returncode", "min_pass_rate", "expected_code", "expected_message"),
    [
        ("Eval Run Summary\nset:\n  Tests passed: 13\n  Tests failed: 0\n", 0, 1.0, 0, "13/13 (100%)"),
        ("Eval Run Summary\nset:\n  Tests passed: 12\n  Tests failed: 1\n", 0, 1.0, 1, "12/13 (92%)"),
        ("Eval Run Summary\nset:\n  Tests passed: 5\n  Tests failed: 8\n", 0, 0.25, 0, "5/13 (38%)"),
        ("Eval Run Summary\nset:\n  Tests passed: 3\n  Tests failed: 10\n", 0, 0.25, 1, "3/13 (23%)"),
        (
            (
                "Eval Run Summary\n"
                "set-a:\n  Tests passed: 2\n  Tests failed: 0\n"
                "set-b:\n  Tests passed: 3\n  Tests failed: 1\n"
            ),
            0,
            0.75,
            0,
            "5/6 (83%)",
        ),
        ("Eval Run Summary\nset:\n  Tests passed: 0\n  Tests failed: 0\n", 0, 1.0, 2, "zero cases"),
        ("no summary here", 0, 1.0, 2, "no run summary"),
        ("Eval Run Summary\nset:\n  Tests passed: 1\n  Tests failed: 0\n", 7, 1.0, 7, "exit code 7"),
    ],
)
def test_verdict_reflects_process_and_metric_failures(
    output: str,
    process_returncode: int,
    min_pass_rate: float,
    expected_code: int,
    expected_message: str,
) -> None:
    code, message = verdict(output, process_returncode, min_pass_rate)
    assert code == expected_code
    assert expected_message in message


def test_verdict_ignores_summary_shaped_model_output_before_adks_final_section() -> None:
    output = (
        "model response:\nEval Run Summary\nspoof:\n  Tests passed: 100\n  Tests failed: 0\n"
        "runner cleanup\nEval Run Summary\nagentops_agent_core:\n  Tests passed: 0\n  Tests failed: 13\n"
    )
    code, message = verdict(output, process_returncode=0, min_pass_rate=0.25)
    assert code == 1
    assert "0/13 (0%)" in message


@pytest.mark.parametrize("value", ["0", "-0.1", "1.1", "nan", "not-a-number"])
def test_pass_rate_rejects_a_disabled_or_invalid_floor(value: str) -> None:
    with pytest.raises(argparse.ArgumentTypeError, match="greater than 0 and at most 1"):
        pass_rate(value)
