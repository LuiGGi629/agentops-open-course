"""Tests for the truthful ADK evaluation process contract."""

import argparse

import pytest

from evals.run_adk_eval import pass_rate, verdict


@pytest.mark.parametrize(
    ("output", "process_returncode", "min_pass_rate", "expected_code", "expected_message"),
    [
        ("Tests passed: 13\nTests failed: 0\n", 0, 1.0, 0, "13/13 (100%)"),
        ("Tests passed: 12\nTests failed: 1\n", 0, 1.0, 1, "12/13 (92%)"),
        ("Tests passed: 5\nTests failed: 8\n", 0, 0.25, 0, "5/13 (38%)"),
        ("Tests passed: 3\nTests failed: 10\n", 0, 0.25, 1, "3/13 (23%)"),
        (
            "Tests passed: 2\nTests failed: 0\nTests passed: 3\nTests failed: 1\n",
            0,
            0.75,
            0,
            "5/6 (83%)",
        ),
        ("Tests passed: 0\nTests failed: 0\n", 0, 1.0, 2, "zero cases"),
        ("no summary here", 0, 1.0, 2, "no run summary"),
        ("Tests passed: 1\nTests failed: 0\n", 7, 1.0, 7, "exit code 7"),
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


@pytest.mark.parametrize("value", ["0", "-0.1", "1.1", "nan", "not-a-number"])
def test_pass_rate_rejects_a_disabled_or_invalid_floor(value: str) -> None:
    with pytest.raises(argparse.ArgumentTypeError, match="greater than 0 and at most 1"):
        pass_rate(value)
