"""Calibrate the optional agentgateway judge against labeled answers."""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import Counter
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

from evals.mlflow_eval import _gateway_judge

_DEFAULT_SET = Path(__file__).with_name("judge-calibration.json")
_CATEGORIES = {"good", "bad", "hallucinated"}


def load_cases(path: Path) -> list[dict[str, Any]]:
    """Load a unique, category-balanced calibration set with strict scalar fields."""
    document = json.loads(path.read_text(encoding="utf-8"))
    cases = document.get("cases") if isinstance(document, dict) else None
    if not isinstance(cases, list) or len(cases) < 12:
        raise ValueError("judge calibration needs at least 12 labeled cases")
    ids: set[str] = set()
    categories: Counter[str] = Counter()
    for case in cases:
        if not isinstance(case, dict):
            raise ValueError("every judge calibration case must be an object")
        case_id = case.get("id")
        category = case.get("category")
        if not isinstance(case_id, str) or not case_id or case_id in ids:
            raise ValueError("judge calibration ids must be non-empty and unique")
        ids.add(case_id)
        if category not in _CATEGORIES:
            raise ValueError(f"judge calibration case {case_id!r} has an invalid category")
        categories[category] += 1
        for field in ("question", "reference_answer", "answer"):
            if not isinstance(case.get(field), str) or not case[field].strip():
                raise ValueError(f"judge calibration case {case_id!r} needs non-empty {field}")
        if not isinstance(case.get("expected_pass"), bool):
            raise ValueError(f"judge calibration case {case_id!r} needs boolean expected_pass")
    if set(categories) != _CATEGORIES or len(set(categories.values())) != 1:
        raise ValueError("judge calibration must balance good, bad, and hallucinated answers")
    return cases


def agreement(cases: list[dict[str, Any]], judge: Callable[..., Any]) -> tuple[int, int]:
    """Return judge-label agreements without hiding per-case disagreement."""
    matches = 0
    for case in cases:
        feedback = judge(
            inputs={"turns": [case["question"]]},
            outputs={"responses": [case["answer"]]},
            expectations={"expected_responses": [case["reference_answer"]]},
        )
        predicted = feedback.value
        if not isinstance(predicted, bool):
            raise ValueError(f"judge returned a non-boolean verdict for {case['id']!r}")
        matched = predicted is case["expected_pass"]
        matches += int(matched)
        sys.stdout.write(f"{case['id']}: expected={case['expected_pass']} predicted={predicted} match={matched}\n")
    return matches, len(cases)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    """Parse the set, validation mode, and agreement floor."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--set", type=Path, default=_DEFAULT_SET)
    parser.add_argument("--validate-only", action="store_true")
    parser.add_argument("--min-agreement", type=float, default=0.75)
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    """Validate offline or call the configured judge and enforce agreement."""
    args = parse_args(argv)
    if not 0 < args.min_agreement <= 1:
        raise ValueError("--min-agreement must be greater than 0 and at most 1")
    cases = load_cases(args.set)
    if args.validate_only:
        sys.stdout.write(f"validated {len(cases)} judge calibration cases\n")
        return 0
    model = os.environ.get("MLFLOW_JUDGE_MODEL")
    base_url = os.environ.get("MLFLOW_JUDGE_BASE_URL")
    api_key = os.environ.get("MLFLOW_JUDGE_API_KEY")
    if not model or not base_url or not api_key:
        raise ValueError("MLFLOW_JUDGE_MODEL, MLFLOW_JUDGE_BASE_URL, and MLFLOW_JUDGE_API_KEY are required")
    matches, total = agreement(cases, _gateway_judge(model, base_url, api_key))
    rate = matches / total
    sys.stdout.write(f"judge calibration agreement: {matches}/{total} ({rate:.0%}); floor: {args.min_agreement:.0%}\n")
    return 0 if rate >= args.min_agreement else 1


if __name__ == "__main__":
    raise SystemExit(main())
