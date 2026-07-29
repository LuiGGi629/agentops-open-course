"""Tests for the truthful ADK evaluation process contract."""

import argparse
import io
import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from evals import run_adk_eval
from evals.run_adk_eval import eval_case_ids, eval_case_selectors, pass_rate, summary_counts, verdict, verdict_counts


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


def test_serial_summaries_are_parsed_independently_before_aggregation() -> None:
    first = (
        "model response:\nEval Run Summary\nspoof:\n  Tests passed: 100\n  Tests failed: 0\n"
        "Eval Run Summary\nagentops_agent_core:\n  Tests passed: 0\n  Tests failed: 1\n"
    )
    second = "Eval Run Summary\nagentops_agent_core:\n  Tests passed: 1\n  Tests failed: 0\n"
    counts = [summary_counts(output) for output in (first, second)]
    assert counts == [(0, 1), (1, 0)]

    code, message = verdict_counts(
        sum(count[0] for count in counts if count is not None),
        sum(count[1] for count in counts if count is not None),
        min_pass_rate=0.5,
    )
    assert code == 0
    assert "1/2 (50%)" in message


@pytest.mark.parametrize("value", ["0", "-0.1", "1.1", "nan", "not-a-number"])
def test_pass_rate_rejects_a_disabled_or_invalid_floor(value: str) -> None:
    with pytest.raises(argparse.ArgumentTypeError, match="greater than 0 and at most 1"):
        pass_rate(value)


def test_eval_case_selectors_preserve_order_and_force_serial_adk_runs(tmp_path) -> None:
    eval_set = tmp_path / "cases.evalset.json"
    eval_set.write_text(
        json.dumps({"eval_cases": [{"eval_id": "first-case"}, {"eval_id": "second-case"}]}),
        encoding="utf-8",
    )

    assert eval_case_selectors(eval_set) == [
        f"{eval_set}:first-case",
        f"{eval_set}:second-case",
    ]
    assert eval_case_ids(eval_set) == ["first-case", "second-case"]


@pytest.mark.parametrize(
    "document",
    [
        {},
        {"eval_cases": []},
        {"eval_cases": [{}]},
        {"eval_cases": [{"eval_id": "bad:id"}]},
        {"eval_cases": [{"eval_id": "duplicate"}, {"eval_id": "duplicate"}]},
    ],
)
def test_eval_case_selectors_reject_invalid_or_ambiguous_cases(tmp_path, document) -> None:
    eval_set = tmp_path / "invalid.evalset.json"
    eval_set.write_text(json.dumps(document), encoding="utf-8")

    with pytest.raises(SystemExit):
        eval_case_selectors(eval_set)


def test_main_launches_one_process_per_case_and_aggregates_authoritative_summaries(
    monkeypatch,
    tmp_path,
    capsys,
) -> None:
    eval_set = tmp_path / "cases.evalset.json"
    eval_set.write_text(
        json.dumps({"eval_cases": [{"eval_id": "first"}, {"eval_id": "second"}]}),
        encoding="utf-8",
    )
    monkeypatch.setattr(
        run_adk_eval,
        "parse_args",
        lambda: SimpleNamespace(
            agent=Path("src/agent"),
            eval_set=eval_set,
            config=Path("evals/test_config.json"),
            min_pass_rate=0.5,
            required_case=["second"],
        ),
    )
    results = [
        (
            (
                "model output\nEval Run Summary\nspoof:\n  Tests passed: 10\n  Tests failed: 0\n"
                "Eval Run Summary\nset:\n  Tests passed: 0\n  Tests failed: 1\n"
            ),
            0,
        ),
        ("Eval Run Summary\nset:\n  Tests passed: 1\n  Tests failed: 0\n", 0),
    ]
    commands = []
    state_dirs: list[Path] = []

    def popen(command, **kwargs):
        commands.append((command, kwargs))
        state_dir = Path(kwargs["env"]["AGENT_STATE_DIR"])
        assert state_dir.is_dir()
        state_dirs.append(state_dir)
        output, returncode = results.pop(0)
        return SimpleNamespace(stdout=io.StringIO(output), wait=lambda: returncode)

    monkeypatch.setattr(run_adk_eval.subprocess, "Popen", popen)

    with pytest.raises(SystemExit) as exit_info:
        run_adk_eval.main()

    assert exit_info.value.code == 0
    output = capsys.readouterr().out
    assert "1/2 (50%)" in output
    assert "Required strict ADK cases passed: second." in output
    assert [command[0][5] for command in commands] == [
        f"{eval_set}:first",
        f"{eval_set}:second",
    ]
    assert all(command[1]["stderr"] is run_adk_eval.subprocess.STDOUT for command in commands)
    assert len(set(state_dirs)) == 2
    assert all(not state_dir.exists() for state_dir in state_dirs)


def test_main_stops_on_the_first_nonzero_adk_process(monkeypatch, tmp_path) -> None:
    eval_set = tmp_path / "cases.evalset.json"
    eval_set.write_text(
        json.dumps({"eval_cases": [{"eval_id": "first"}, {"eval_id": "never-run"}]}),
        encoding="utf-8",
    )
    monkeypatch.setattr(
        run_adk_eval,
        "parse_args",
        lambda: SimpleNamespace(
            agent=Path("src/agent"),
            eval_set=eval_set,
            config=Path("evals/test_config.json"),
            min_pass_rate=0.25,
            required_case=[],
        ),
    )
    calls = []

    def popen(command, **_kwargs):
        calls.append(command)
        return SimpleNamespace(stdout=io.StringIO("provider failed\n"), wait=lambda: 7)

    monkeypatch.setattr(run_adk_eval.subprocess, "Popen", popen)

    with pytest.raises(SystemExit) as exit_info:
        run_adk_eval.main()

    assert exit_info.value.code == 7
    assert len(calls) == 1


def test_main_fails_when_a_required_case_misses_even_above_the_aggregate_floor(
    monkeypatch,
    tmp_path,
    capsys,
) -> None:
    eval_set = tmp_path / "cases.evalset.json"
    eval_set.write_text(
        json.dumps({"eval_cases": [{"eval_id": "critical"}, {"eval_id": "optional"}]}),
        encoding="utf-8",
    )
    monkeypatch.setattr(
        run_adk_eval,
        "parse_args",
        lambda: SimpleNamespace(
            agent=Path("src/agent"),
            eval_set=eval_set,
            config=Path("evals/test_config.json"),
            min_pass_rate=0.25,
            required_case=["critical"],
        ),
    )
    results = [
        ("Eval Run Summary\nset:\n  Tests passed: 0\n  Tests failed: 1\n", 0),
        ("Eval Run Summary\nset:\n  Tests passed: 1\n  Tests failed: 0\n", 0),
    ]

    def popen(_command, **_kwargs):
        output, returncode = results.pop(0)
        return SimpleNamespace(stdout=io.StringIO(output), wait=lambda: returncode)

    monkeypatch.setattr(run_adk_eval.subprocess, "Popen", popen)

    with pytest.raises(SystemExit) as exit_info:
        run_adk_eval.main()

    assert exit_info.value.code == 1
    assert "Required ADK case 'critical' failed" in capsys.readouterr().err
    assert len(results) == 1


def test_main_rejects_an_unknown_required_case_before_model_work(monkeypatch, tmp_path) -> None:
    eval_set = tmp_path / "cases.evalset.json"
    eval_set.write_text(json.dumps({"eval_cases": [{"eval_id": "known"}]}), encoding="utf-8")
    monkeypatch.setattr(
        run_adk_eval,
        "parse_args",
        lambda: SimpleNamespace(
            agent=Path("src/agent"),
            eval_set=eval_set,
            config=Path("evals/test_config.json"),
            min_pass_rate=0.25,
            required_case=["missing"],
        ),
    )

    with pytest.raises(SystemExit, match=r"Required ADK cases.*'missing'"):
        run_adk_eval.main()
