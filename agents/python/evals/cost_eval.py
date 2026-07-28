"""Token/cost regression evidence for the AgentOps Agent (Chapters 4.4 and 7.3).

A prompt or model change can keep every behavioral scorer green while quietly
doubling the tokens or model calls a case costs. The trajectory scorers match
`IN_ORDER` and deliberately tolerate extra reads (Chapter 4.4), so they never
catch that waste. This script runs each committed eval case, records its token
and model-call usage, and compares it against a committed baseline; a case that
grows beyond the tolerance is reported as a regression.

It is model-backed evidence, not a merge gate — like the other live evals it
belongs in the weekly `eval.yml` workflow (Chapter 4.3), not `ci.yml`. No token
counts are committed until you measure them: run `--update` to (re)generate
`cost_baseline.json` from real measurements on your configured model, review the
diff, and commit it. Set `AGENT_COST_TOLERANCE` (default 0.25) to tune strictness.
"""

from __future__ import annotations

import json
import math
import os
import sys
from http.client import HTTPConnection, HTTPException
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

try:  # pytest imports this as ``evals.cost_eval``; the CLI runs it with ``evals/`` on sys.path[0]
    from evals.mlflow_eval import _load_cases, ask
except ModuleNotFoundError:  # pragma: no cover - script-invocation fallback
    from mlflow_eval import _load_cases, ask  # ty: ignore[unresolved-import]

from agent.config import settings

_BASELINE = Path(__file__).parent / "cost_baseline.json"
_OBSERVED = Path(__file__).parent / "cost-observed.json"
_METRICS = ("total_tokens", "model_calls")
_DEFAULT_TOLERANCE = 0.25


def regressions(
    observed: dict[str, dict[str, int]],
    baseline: dict[str, dict[str, int]],
    tolerance: float = _DEFAULT_TOLERANCE,
) -> list[str]:
    """Return one message per case/metric that exceeds its baseline by > tolerance.

    A missing case (renamed or removed) is not a regression; a new case with no
    baseline is reported by ``main`` as "record a baseline", not here. A
    non-positive baseline is unusable evidence and is reported for replacement.
    """
    lines: list[str] = []
    for eval_id in sorted(baseline):
        current = observed.get(eval_id)
        if current is None:
            continue
        for metric in _METRICS:
            base_value = baseline[eval_id].get(metric, 0)
            now = current.get(metric, 0)
            if isinstance(base_value, bool) or not isinstance(base_value, int) or base_value <= 0:
                lines.append(f"{eval_id} {metric}: baseline {base_value!r} is not comparable; regenerate and review it")
                continue
            allowed = base_value * (1 + tolerance)
            if now > allowed:
                lines.append(
                    f"{eval_id} {metric}: {now} > {allowed:g} (baseline {base_value}, +{tolerance:.0%} tolerance)"
                )
    return lines


def measure() -> dict[str, dict[str, int]]:
    """Run every committed eval case and return its per-case usage totals."""
    observed: dict[str, dict[str, int]] = {}
    for case in _load_cases():
        inputs: dict[str, Any] = case["inputs"]
        eval_id = inputs["eval_id"]
        usage = ask(inputs["turns"], eval_id).get("usage")
        observed[eval_id] = _usage_cases(
            {eval_id: usage},
            source="Measured model usage",
        )[eval_id]
    return observed


def _direct_ollama_endpoint() -> tuple[str, int] | None:
    """Return the local Ollama host/port only for the direct default topology."""
    if str(settings.model_provider) != "openai-compatible" or not settings.openai_base_url:
        return None
    parsed = urlsplit(settings.openai_base_url)
    if (
        parsed.scheme != "http"
        or parsed.hostname not in {"127.0.0.1", "localhost"}
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path.rstrip("/") != "/v1"
    ):
        return None
    try:
        port = parsed.port
    except ValueError:
        return None
    if port != 11434:
        return None
    return parsed.hostname, port


def _model_digest() -> str | None:
    """Resolve an explicit digest, or discover direct local Ollama without remote calls."""
    if explicit := os.environ.get("EVAL_MODEL_DIGEST"):
        return explicit
    endpoint = _direct_ollama_endpoint()
    if endpoint is None:
        return None
    connection = HTTPConnection(*endpoint, timeout=2)
    try:
        # HTTPConnection talks to the validated loopback endpoint directly and
        # never honors HTTP_PROXY/HTTPS_PROXY from the learner's shell.
        connection.request("GET", "/api/tags", headers={"Accept": "application/json"})
        response = connection.getresponse()
        if response.status != 200:
            return None
        document = json.load(response)
    except (OSError, HTTPException, TypeError, ValueError, json.JSONDecodeError):
        return None
    finally:
        connection.close()
    models = document.get("models") if isinstance(document, dict) else None
    if not isinstance(models, list):
        return None
    for candidate in models:
        if not isinstance(candidate, dict):
            continue
        if candidate.get("name") != settings.model and candidate.get("model") != settings.model:
            continue
        digest = candidate.get("digest")
        return digest if isinstance(digest, str) and digest else None
    return None


def _measurement(observed: dict[str, dict[str, int]], model_digest: str | None) -> dict[str, Any]:
    """Attach the model identity required to interpret usage measurements."""
    return {
        "schema_version": 1,
        "model_provider": str(settings.model_provider),
        "model": settings.model,
        "model_digest": model_digest,
        "cases": observed,
    }


def _write_json(path: Path, value: dict[str, Any]) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _read_json(path: Path) -> Any:
    """Read one evidence document with an actionable failure at the boundary."""
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise SystemExit(f"{path.name} is unreadable or invalid JSON: {error}") from None


def _usage_cases(value: Any, *, source: str) -> dict[str, dict[str, int]]:
    """Validate that each case has comparable positive model usage."""
    if not isinstance(value, dict) or not value:
        raise SystemExit(f"{source} must contain at least one case with positive integer usage.")
    validated: dict[str, dict[str, int]] = {}
    for eval_id, metrics in value.items():
        if not isinstance(eval_id, str) or not eval_id or not isinstance(metrics, dict):
            raise SystemExit(f"{source} contains an invalid case; regenerate it from real model usage.")
        case: dict[str, int] = {}
        for metric in _METRICS:
            measurement = metrics.get(metric)
            if isinstance(measurement, bool) or not isinstance(measurement, int) or measurement <= 0:
                raise SystemExit(
                    f"{source} case {eval_id!r} needs a positive integer {metric}; "
                    "regenerate it from model usage metadata."
                )
            case[metric] = measurement
        validated[eval_id] = case
    return validated


def _baseline_cases(document: Any, *, model_digest: str | None) -> dict[str, dict[str, int]]:
    """Validate baseline identity and return its per-case measurements."""
    if (
        not isinstance(document, dict)
        or type(document.get("schema_version")) is not int
        or document["schema_version"] != 1
        or not isinstance(document.get("cases"), dict)
    ):
        raise SystemExit("cost_baseline.json has an unsupported shape; regenerate it with --update.")
    baseline_model = document.get("model")
    if baseline_model != settings.model:
        raise SystemExit(
            f"Cost baseline targets model {baseline_model!r}, not {settings.model!r}; "
            "inspect cost-observed.json or record a model-specific baseline."
        )
    baseline_provider = document.get("model_provider")
    current_provider = str(settings.model_provider)
    if baseline_provider != current_provider:
        raise SystemExit(
            f"Cost baseline targets provider {baseline_provider!r}, not {current_provider!r}; "
            "record a provider-specific baseline."
        )
    baseline_digest = document.get("model_digest")
    if baseline_digest is not None and (not isinstance(baseline_digest, str) or not baseline_digest):
        raise SystemExit("cost_baseline.json has an unsupported model digest; regenerate it with --update.")
    if baseline_digest != model_digest:
        raise SystemExit(
            f"Cost baseline model digest {baseline_digest!r} does not match {model_digest!r}; "
            "review the model change and regenerate with --update."
        )
    return _usage_cases(document["cases"], source="cost_baseline.json")


def _tolerance(raw: str | None) -> float:
    """Parse a finite, non-negative cost-growth tolerance."""
    try:
        tolerance = float(raw) if raw else _DEFAULT_TOLERANCE
    except ValueError:
        raise SystemExit(f"AGENT_COST_TOLERANCE must be a finite non-negative number, got {raw!r}.") from None
    if not math.isfinite(tolerance) or tolerance < 0:
        raise SystemExit(f"AGENT_COST_TOLERANCE must be a finite non-negative number, got {raw!r}.")
    return tolerance


def main() -> None:
    """Measure per-case usage, then record or compare against the baseline."""
    update = "--update" in sys.argv[1:]
    model_digest = _model_digest()
    baseline: dict[str, dict[str, int]] | None = None
    tolerance = _DEFAULT_TOLERANCE
    if not update and _BASELINE.exists():
        baseline = _baseline_cases(
            _read_json(_BASELINE),
            model_digest=model_digest,
        )
        tolerance = _tolerance(os.environ.get("AGENT_COST_TOLERANCE"))

    observed = _usage_cases(measure(), source="Measured model usage")
    measurement = _measurement(observed, model_digest)
    _write_json(_OBSERVED, measurement)
    for eval_id in sorted(observed):
        usage = observed[eval_id]
        print(f"  {eval_id}: {usage['total_tokens']} tokens, {usage['model_calls']} model calls")  # noqa: T201

    if update or not _BASELINE.exists():
        _write_json(_BASELINE, measurement)
        reason = "Updated" if update else "No baseline found; recorded"
        print(f"\n{reason} {_BASELINE.name} from this run's measurements. Review the diff and commit it.")  # noqa: T201
        return

    if baseline is None:  # defensive: the existing-baseline branch above owns comparison
        raise RuntimeError("cost baseline was not loaded")
    missing_cases = sorted(set(observed) - set(baseline))
    problems = [
        *(f"{eval_id}: no reviewed baseline; rerun with --update" for eval_id in missing_cases),
        *regressions(observed, baseline, tolerance),
    ]
    if problems:
        raise SystemExit("Cost regression against cost_baseline.json:\n  " + "\n  ".join(problems))
    print(f"\nNo token/model-call regression beyond {tolerance:.0%} against {_BASELINE.name}.")  # noqa: T201


if __name__ == "__main__":
    main()
