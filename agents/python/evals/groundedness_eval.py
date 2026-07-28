"""Groundedness / citation-coverage evidence for the AgentOps Agent (Chapter 4.4).

The ``response_facts`` scorer (Chapter 4.4) checks that an answer *contains* the
right domain facts. It cannot catch the opposite failure: an answer that also
names an entity the agent never retrieved — a hallucinated incident id, a
service it never queried, a runbook it never opened. That entity may even exist
in the seed, so a correctness check against ground truth would pass it; what
makes it ungrounded is that *this turn's evidence* never mentioned it.

This scorer is deterministic. For each turn it extracts identifier-shaped
incident/severity claims plus names from the course's fixed service and runbook
vocabulary. It requires each recognized entity to appear in the grounding
context: the tool responses the agent received that turn, plus the user's own
question (you may always echo the asker). A recognized entity in the answer
that is in neither is reported as an unsupported claim.

Like the cost baseline, it is model-backed evidence, not a merge gate — it runs
the agent, so it belongs in the weekly ``eval.yml`` workflow, not ``ci.yml``.
The scoring logic itself is pure and unit-tested offline with fixed transcripts.
"""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

try:  # pytest imports this as ``evals.groundedness_eval``; the CLI runs it with ``evals/`` on sys.path[0]
    from evals.mlflow_eval import _SERVICE_TERMS, _load_cases, ask
except ModuleNotFoundError:  # pragma: no cover - script-invocation fallback
    from mlflow_eval import _SERVICE_TERMS, _load_cases, ask  # ty: ignore[unresolved-import]

# Runbook slugs shipped under agents/data/runbooks; a runbook the answer cites
# must have surfaced in a tool response (get_runbook / search_runbooks) that turn.
_RUNBOOK_SLUGS = frozenset(
    {
        "cascade-failure",
        "deployment-rollback",
        "disk-full",
        "elevated-errors",
        "high-latency",
        "memory-leak",
        "service-down",
    }
)
# Patterns for the identifier-shaped entities an answer can fabricate.
_ID_PATTERNS = (r"inc-\d+", r"sev\d+")
_AMBIGUOUS_SERVICE_TERMS = frozenset({"cache", "search"})
_OBSERVED = Path(__file__).parent / "ground-observed.json"


def _word_matches(text: str, term: str) -> bool:
    """Whole-token, case-insensitive membership (so ``auth`` != ``author``)."""
    return re.search(rf"(?<![\w-]){re.escape(term)}(?![\w-])", text, re.IGNORECASE) is not None


def _claims_service(text: str, term: str) -> bool:
    """Match a service name without treating an ambiguous verb as an entity."""
    if not _word_matches(text, term):
        return False
    if term not in _AMBIGUOUS_SERVICE_TERMS:
        return True
    escaped = re.escape(term)
    state = r"(?:up|down|healthy|unhealthy|degraded|unknown|operational|unavailable|failing|failed)"
    service_contexts = (
        rf"\b{escaped}\s+service\b",
        rf"\bservice(?:\s+named)?\s+{escaped}\b",
        rf"\b{escaped}\s+(?:is|was|remains|appears|looks)\s+(?:\w+\s+){{0,2}}{state}\b",
        rf"\b{escaped}\s+(?:has|had|reports?)\s+(?:elevated\s+)?(?:errors?|latency|failures?|timeouts?)\b",
        rf"""["']service["']\s*:\s*["']{escaped}["']""",
        rf"""["']name["']\s*:\s*["']{escaped}["']""",
    )
    return any(re.search(pattern, text, re.IGNORECASE) for pattern in service_contexts)


def claimed_entities(text: str) -> set[str]:
    """Return recognized ids, severities, and known course service/runbook names."""
    lowered = text.lower()
    entities: set[str] = set()
    for pattern in _ID_PATTERNS:
        entities.update(re.findall(pattern, lowered))
    for term in _SERVICE_TERMS:
        if _claims_service(lowered, term):
            entities.add(term)
    for term in _RUNBOOK_SLUGS:
        if _word_matches(lowered, term):
            entities.add(term)
    return entities


# --8<-- [start:unsupported-claims]
def unsupported_claims(responses: list[str], evidence: list[str], questions: list[str]) -> list[str]:
    """Return one message per recognized entity absent from that turn's grounding context.

    The grounding context is the tool responses received that turn plus the user's
    own question — an answer may always restate what it retrieved or what it was
    asked. Anything else the answer names was invented.
    """
    problems: list[str] = []
    for index, response in enumerate(responses):
        question = questions[index] if index < len(questions) else ""
        turn_evidence = evidence[index] if index < len(evidence) else ""
        grounding = f"{question} {turn_evidence}"
        problems.extend(
            f"turn {index + 1}: answer claims {entity!r} with no supporting evidence"
            for entity in sorted(claimed_entities(response))
            if not (
                _claims_service(grounding, entity)
                if entity in _AMBIGUOUS_SERVICE_TERMS
                else _word_matches(grounding, entity)
            )
        )
    return problems


# --8<-- [end:unsupported-claims]


def measure() -> dict[str, dict[str, Any]]:
    """Run each case and retain the transcript needed to audit every claim."""
    observed: dict[str, dict[str, Any]] = {}
    for case in _load_cases():
        inputs: dict[str, Any] = case["inputs"]
        eval_id = inputs["eval_id"]
        result = ask(inputs["turns"], eval_id)
        observed[eval_id] = {
            "questions": inputs["turns"],
            "responses": result["responses"],
            "evidence": result["evidence"],
            "unsupported_claims": unsupported_claims(result["responses"], result["evidence"], inputs["turns"]),
        }
    return observed


def main() -> None:
    """Measure grounding for every case and fail on any unsupported claim."""
    observed = measure()
    _OBSERVED.write_text(json.dumps(observed, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    problems: list[str] = []
    for eval_id in sorted(observed):
        claims = observed[eval_id]["unsupported_claims"]
        status = "ok" if not claims else f"{len(claims)} unsupported"
        print(f"  {eval_id}: {status}")  # noqa: T201 - CLI output
        problems.extend(f"{eval_id} {claim}" for claim in claims)
    if problems:
        raise SystemExit("Ungrounded answers:\n  " + "\n  ".join(problems))
    print("\nEvery recognized entity was grounded in that turn's evidence or question.")  # noqa: T201


if __name__ == "__main__":
    main()
