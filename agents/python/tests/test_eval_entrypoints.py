"""Offline contracts for the small model-backed evaluation entry points."""

import json
import sys
import warnings
from contextlib import nullcontext
from importlib import import_module
from pathlib import Path
from types import SimpleNamespace

import pytest

from evals import (
    cost_eval,
    governed_adk_eval,
    groundedness_eval,
    judge_calibration,
    mlflow_eval,
    retrieval_eval,
    run_adk_eval,
)


def test_retrieval_helpers_derive_cases_and_ranked_slugs(monkeypatch) -> None:
    incident = SimpleNamespace(title="Timeout", summary="Requests wait", runbook="latency")
    monkeypatch.setattr(retrieval_eval.data, "list_incidents", lambda: [incident])
    monkeypatch.setattr(retrieval_eval, "search_runbooks", lambda query, limit: {"runbooks": [{"slug": query[:limit]}]})
    monkeypatch.setattr(retrieval_eval, "semantic_search", lambda query, limit: [{"slug": f"{query}:{limit}"}])

    assert retrieval_eval.cases() == [("Timeout. Requests wait", "latency")]
    assert retrieval_eval.keyword_slugs("abc", 2) == ["ab"]
    assert retrieval_eval.semantic_slugs("abc", 2) == ["abc:2"]
    assert retrieval_eval.hit_rate(lambda _query, _k: ["latency"], 1) == 1.0
    assert retrieval_eval.hit_rate(lambda _query, _k: [], 1) == 0.0


def test_retrieval_main_logs_provenance_and_all_comparisons(monkeypatch, capsys) -> None:
    metrics: dict[str, float] = {}
    params: dict[str, str] = {}
    tags: dict[str, str] = {}
    monkeypatch.setattr(retrieval_eval.mlflow, "set_tracking_uri", lambda value: tags.setdefault("uri", value))
    monkeypatch.setattr(retrieval_eval.mlflow, "set_experiment", lambda value: tags.setdefault("experiment", value))
    monkeypatch.setattr(retrieval_eval.mlflow, "start_run", lambda **_kwargs: nullcontext())
    monkeypatch.setattr(retrieval_eval.mlflow, "log_metrics", metrics.update)
    monkeypatch.setattr(retrieval_eval.mlflow, "log_params", params.update)
    monkeypatch.setattr(retrieval_eval.mlflow, "set_tag", tags.__setitem__)
    monkeypatch.setattr(retrieval_eval, "isolated_state", lambda _prefix: nullcontext())
    monkeypatch.setattr(retrieval_eval, "index_provenance", lambda: {"model": "embedding"})
    monkeypatch.setattr(retrieval_eval, "cases", lambda: [("question", "answer")])

    def rate(retrieve, k):
        values = {
            ("keyword_slugs", 1): 0.5,
            ("semantic_slugs", 1): 0.75,
            ("keyword_slugs", 3): 1.0,
            ("semantic_slugs", 3): 1.0,
        }
        return values[(retrieve.__name__, k)]

    monkeypatch.setattr(retrieval_eval, "hit_rate", rate)
    retrieval_eval.main()

    assert metrics == {
        "keyword_hit_rate_at_1": 0.5,
        "semantic_hit_rate_at_1": 0.75,
        "keyword_hit_rate_at_3": 1.0,
        "semantic_hit_rate_at_3": 1.0,
    }
    assert params == {"index_model": "embedding"}
    assert tags["eval"] == "retrieval-quality"
    output = capsys.readouterr().out
    assert "semantic beats" in output
    assert "semantic matches" in output


def test_report_main_runs_three_governed_samples_in_isolated_state(monkeypatch) -> None:
    # Importing ADK's evaluator reaches a pinned Vertex dependency that declares
    # deprecated surfaces. Keep those upstream warnings local while
    # exercising our entry point; no repository warning is ignored globally.
    with warnings.catch_warnings():
        warnings.simplefilter("ignore")
        report_eval = import_module("evals.report_eval")
    calls: list[object] = []

    async def evaluate(**kwargs):
        calls.append(kwargs)

    monkeypatch.setattr(report_eval, "require_attributable_runtime", lambda: calls.append("runtime"))
    monkeypatch.setattr(report_eval, "install_app_policy", lambda: calls.append("policy"))
    monkeypatch.setattr(report_eval, "isolated_state", lambda prefix: nullcontext(calls.append(prefix)))
    monkeypatch.setattr(report_eval.AgentEvaluator, "evaluate", evaluate)
    monkeypatch.setattr(
        report_eval.DEFAULT_METRIC_EVALUATOR_REGISTRY,
        "register_evaluator",
        lambda metric, evaluator: calls.append((metric, evaluator)),
    )

    report_eval.main()

    assert calls[:2] == ["runtime", "policy"]
    assert calls[-1] == {
        "agent_module": "agent.structured_report.agent",
        "eval_dataset_file_path_or_dir": "evals/triage-report.evalset.json",
        "num_runs": 3,
    }


def _write_calibration(path: Path, cases: list[dict[str, object]]) -> Path:
    path.write_text(json.dumps({"cases": cases}), encoding="utf-8")
    return path


def _valid_calibration_cases() -> list[dict[str, object]]:
    return [
        {
            "id": f"{category}-{index}",
            "category": category,
            "question": "Question",
            "reference_answer": "Reference",
            "answer": "Answer",
            "expected_pass": category == "good",
        }
        for category in ("good", "bad", "hallucinated")
        for index in range(4)
    ]


@pytest.mark.parametrize(
    ("mutate", "message"),
    [
        (lambda cases: cases.pop(), "at least 12"),
        (lambda cases: cases.__setitem__(0, "bad"), "must be an object"),
        (lambda cases: cases[1].__setitem__("id", cases[0]["id"]), "unique"),
        (lambda cases: cases[0].__setitem__("category", "unknown"), "invalid category"),
        (lambda cases: cases[0].__setitem__("question", ""), "non-empty question"),
        (lambda cases: cases[0].__setitem__("expected_pass", "yes"), "boolean expected_pass"),
        (lambda cases: cases[0].__setitem__("category", "bad"), "must balance"),
    ],
)
def test_judge_calibration_rejects_malformed_or_unbalanced_sets(tmp_path, mutate, message) -> None:
    cases = _valid_calibration_cases()
    mutate(cases)
    path = _write_calibration(tmp_path / "set.json", cases)
    with pytest.raises(ValueError, match=message):
        judge_calibration.load_cases(path)


def test_judge_calibration_main_validates_and_enforces_live_agreement(monkeypatch, tmp_path, capsys) -> None:
    path = _write_calibration(tmp_path / "set.json", _valid_calibration_cases())
    assert judge_calibration.main(["--set", str(path), "--validate-only"]) == 0
    assert "validated 12" in capsys.readouterr().out

    with pytest.raises(ValueError, match="greater than 0"):
        judge_calibration.main(["--set", str(path), "--min-agreement", "0"])
    with pytest.raises(ValueError, match="MLFLOW_JUDGE_MODEL"):
        judge_calibration.main(["--set", str(path)])

    monkeypatch.setenv("MLFLOW_JUDGE_MODEL", "judge")
    monkeypatch.setenv("MLFLOW_JUDGE_BASE_URL", "http://gateway/v1")
    monkeypatch.setenv("MLFLOW_JUDGE_API_KEY", "marker")
    verdicts = iter([True] * 9 + [False] * 3)
    monkeypatch.setattr(
        judge_calibration,
        "_gateway_judge",
        lambda *_args: lambda **_kwargs: SimpleNamespace(value=next(verdicts)),
    )
    assert judge_calibration.main(["--set", str(path), "--min-agreement", "0.75"]) == 1
    assert "agreement" in capsys.readouterr().out


def test_judge_calibration_rejects_a_non_boolean_judge_verdict(tmp_path) -> None:
    cases = judge_calibration.load_cases(_write_calibration(tmp_path / "set.json", _valid_calibration_cases()))
    with pytest.raises(ValueError, match="non-boolean"):
        judge_calibration.agreement(cases, lambda **_kwargs: SimpleNamespace(value="pass"))


def test_groundedness_main_writes_auditable_success_and_failure(monkeypatch, tmp_path, capsys) -> None:
    observed = tmp_path / "observed.json"
    monkeypatch.setattr(groundedness_eval, "_OBSERVED", observed)
    monkeypatch.setattr(
        groundedness_eval,
        "measure",
        lambda: {"ok": {"provider_errors": [], "unsupported_claims": []}},
    )
    groundedness_eval.main()
    assert json.loads(observed.read_text(encoding="utf-8"))["ok"]["unsupported_claims"] == []
    assert "Every recognized entity" in capsys.readouterr().out

    monkeypatch.setattr(
        groundedness_eval,
        "measure",
        lambda: {
            "broken": {
                "provider_errors": ["turn 1: unavailable"],
                "unsupported_claims": ["turn 1: invented"],
            }
        },
    )
    with pytest.raises(SystemExit, match="broken turn 1: unavailable"):
        groundedness_eval.main()


def test_governed_eval_main_installs_policy_before_adk_cli(monkeypatch) -> None:
    from google.adk.cli import cli_tools_click

    calls: list[str] = []
    monkeypatch.setattr(governed_adk_eval, "install_app_policy", lambda: calls.append("policy"))
    monkeypatch.setattr(cli_tools_click, "main", lambda: calls.append("cli"))
    governed_adk_eval.main()
    assert calls == ["policy", "cli"]


def test_governed_eval_rejects_a_missing_root_agent() -> None:
    with pytest.raises(RuntimeError, match="did not provide"):
        governed_adk_eval._governed_runner()  # noqa: SLF001


def test_run_adk_eval_parser_accepts_repeat_and_required_cases(monkeypatch, tmp_path) -> None:
    eval_set = tmp_path / "set.json"
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "run_adk_eval.py",
            str(eval_set),
            "--repeat",
            "3",
            "--min-pass-rate",
            "0.5",
            "--required-case",
            "critical",
            "--print-detailed-results",
        ],
    )
    args = run_adk_eval.parse_args()
    assert (args.eval_set, args.repeat, args.min_pass_rate, args.required_case, args.print_detailed_results) == (
        eval_set,
        3,
        0.5,
        ["critical"],
        True,
    )


def test_run_adk_eval_rejects_invalid_json_and_incomplete_summaries(tmp_path) -> None:
    invalid = tmp_path / "invalid.json"
    invalid.write_text("{", encoding="utf-8")
    with pytest.raises(SystemExit, match="unreadable or invalid JSON"):
        run_adk_eval.eval_case_ids(invalid)
    assert run_adk_eval.summary_counts("Eval Run Summary\nno per-set counts") is None


def test_cost_main_records_then_compares_a_baseline_without_model_work(monkeypatch, tmp_path, capsys) -> None:
    baseline = tmp_path / "baseline.json"
    observed_path = tmp_path / "observed.json"
    usage = {"case": {"total_tokens": 10, "model_calls": 1}}
    identity = {"model": "configured"}
    measurement = {"cases": usage, **identity}
    monkeypatch.setattr(cost_eval, "_BASELINE", baseline)
    monkeypatch.setattr(cost_eval, "_OBSERVED", observed_path)
    monkeypatch.setattr(cost_eval, "_model_digest", lambda: None)
    monkeypatch.setattr(cost_eval, "_current_identity", lambda _digest: identity)
    monkeypatch.setattr(cost_eval, "measure", lambda: usage)
    monkeypatch.setattr(cost_eval, "_usage_cases", lambda value, **_kwargs: value)
    monkeypatch.setattr(cost_eval, "_measurement", lambda _value, _identity: measurement)
    monkeypatch.setattr(cost_eval.sys, "argv", ["cost_eval.py"])

    cost_eval.main()
    assert baseline.exists()
    assert "No baseline found" in capsys.readouterr().out

    monkeypatch.setattr(cost_eval, "_baseline_cases", lambda *_args, **_kwargs: usage)
    monkeypatch.setattr(cost_eval, "regressions", lambda *_args: [])
    cost_eval.main()
    assert "No token/model-call regression" in capsys.readouterr().out


def test_mlflow_boundary_helpers_reject_malformed_evidence(monkeypatch, tmp_path) -> None:
    monkeypatch.setenv("AGENT_EVAL_MIN_SCORE", "invalid")
    with pytest.raises(SystemExit, match="must be a number"):
        mlflow_eval._min_scores()  # noqa: SLF001
    monkeypatch.setenv("AGENT_EVAL_MIN_SCORE", "2")
    with pytest.raises(SystemExit, match="between 0 and 1"):
        mlflow_eval._min_scores()  # noqa: SLF001
    monkeypatch.delenv("AGENT_EVAL_MIN_SCORE")

    assert mlflow_eval._claim_is_satisfied("answer", {}) is False  # noqa: SLF001
    assert mlflow_eval._confirmation_pause_response({"name": "other"}) is None  # noqa: SLF001
    assert mlflow_eval._confirmation_pause_response({"name": "adk_request_confirmation", "args": []}) is None  # noqa: SLF001
    assert mlflow_eval.provider_error_messages({"provider_errors": ["bad", ["bad"], [{"code": ""}]]}) == [
        "turn 1: provider error evidence is malformed",
        "turn 2: provider error evidence is malformed",
        "turn 3: provider error evidence is malformed",
    ]
    assert mlflow_eval.response_facts(outputs={"responses": []}, expectations={"response_contracts": {}}) is False
    assert mlflow_eval.tool_policy(outputs={"tools": {}}, expectations={"expected_tools": []}) is False

    path = tmp_path / "observed.json"
    monkeypatch.setenv("GITHUB_SHA", "revision")
    path.write_text("{", encoding="utf-8")
    with pytest.raises(SystemExit, match="unreadable or invalid JSON"):
        mlflow_eval.load_model_observations(path, expected_cases=[], model_digest="digest")
