"""Side-by-side prompt A/B evaluation for the AgentOps Agent (Chapter 4.4).

Version-pinning and rollback (Chapter 7.0) let you *choose* a prompt version;
this tool tells you which one to choose. It runs the committed eval set through
the four deterministic scorers under two prompt versions and prints a per-scorer
pass-rate table with the delta, so a prompt change is a measured decision, not a
vibe. It is the runnable form of the "compare prompt versions" workflow that
Chapter 7.0 (Reproducibility) describes.

Each version runs in its own subprocess with ``AGENT_PROMPT_URI`` set, because
the agent binds its instruction once at import — a fresh interpreter is the clean
way to evaluate a different pinned version. It is model-backed and intentionally
on-demand, outside the deterministic ``ci.yml`` workflow.

    uv run python evals/prompt_ab.py \
      prompts:/agentops-agent-instruction/1 prompts:/agentops-agent-instruction/2
"""

from __future__ import annotations

import json
import os
import subprocess
import sys

try:  # pytest imports this as ``evals.prompt_ab``; the CLI runs it with ``evals/`` on sys.path[0]
    from evals.mlflow_eval import (
        _load_cases,
        ask,
        complete_conversation,
        response_facts,
        tool_policy,
        tool_trajectory,
    )
except ModuleNotFoundError:  # pragma: no cover - script-invocation fallback
    from mlflow_eval import (  # ty: ignore[unresolved-import]
        _load_cases,
        ask,
        complete_conversation,
        response_facts,
        tool_policy,
        tool_trajectory,
    )

DETERMINISTIC_SCORERS = {
    "tool_trajectory": tool_trajectory,
    "complete_conversation": complete_conversation,
    "response_facts": response_facts,
    "tool_policy": tool_policy,
}

# The child prints its scores on a marked line so the parent can find them even
# if a library logs to stdout — never assume the JSON is the last line printed.
_SCORE_MARKER = "__PROMPT_AB_SCORES__"


def score_configured_prompt() -> dict[str, float]:  # pragma: no cover - model-backed, weekly lane
    """Run the eval set under the currently-configured prompt and return pass rates."""
    cases = _load_cases()
    totals = dict.fromkeys(DETERMINISTIC_SCORERS, 0.0)
    for case in cases:
        outputs = ask(case["inputs"]["turns"], case["inputs"]["eval_id"])
        for name, score in DETERMINISTIC_SCORERS.items():
            totals[name] += 1.0 if score(outputs=outputs, expectations=case["expectations"]) else 0.0
    return {name: total / len(cases) for name, total in totals.items()}


def format_comparison(
    baseline_label: str,
    baseline_scores: dict[str, float],
    candidate_label: str,
    candidate_scores: dict[str, float],
) -> str:
    """Render pass rates with an explicit ``candidate - baseline`` delta."""
    lines = [f"{'scorer':<22} {baseline_label:>12} {candidate_label:>12} {'delta':>8}"]
    for name in DETERMINISTIC_SCORERS:
        baseline = baseline_scores.get(name, 0.0)
        candidate = candidate_scores.get(name, 0.0)
        lines.append(f"{name:<22} {baseline:>12.2f} {candidate:>12.2f} {candidate - baseline:>+8.2f}")
    return "\n".join(lines)


def _score_pinned_prompt(prompt_uri: str) -> dict[str, float]:  # pragma: no cover - spawns a model-backed child
    """Score one prompt version in a fresh interpreter with it pinned."""
    environment = {**os.environ, "AGENT_PROMPT_URI": prompt_uri}
    try:
        completed = subprocess.run(  # noqa: S603 - fixed argv, no shell
            [sys.executable, __file__, "--score"],
            env=environment,
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as error:
        # Surface the child's stderr; otherwise the operator sees a bare
        # non-zero exit with no cause (a model timeout, an import error, ...).
        raise RuntimeError(
            f"Scoring child for {prompt_uri!r} failed (exit {error.returncode}):\n{error.stderr}"
        ) from error
    for line in reversed(completed.stdout.splitlines()):
        if line.startswith(_SCORE_MARKER):
            return json.loads(line[len(_SCORE_MARKER) :])
    raise RuntimeError(f"Scoring child for {prompt_uri!r} produced no scores line.\nstdout:\n{completed.stdout}")


def main() -> None:  # pragma: no cover - CLI entrypoint (model-backed)
    """Score the current prompt (``--score``) or compare two pinned versions."""
    args = sys.argv[1:]
    if args == ["--score"]:
        # Marked, machine-readable child output; the parent scans for this line.
        print(f"{_SCORE_MARKER}{json.dumps(score_configured_prompt())}")  # noqa: T201
        return
    if len(args) != 2:
        raise SystemExit("Usage: prompt_ab.py <baseline-prompt-uri> <candidate-prompt-uri>")
    baseline_uri, candidate_uri = args
    baseline_scores = _score_pinned_prompt(baseline_uri)
    candidate_scores = _score_pinned_prompt(candidate_uri)
    print(  # noqa: T201 - CLI output
        format_comparison(baseline_uri, baseline_scores, candidate_uri, candidate_scores)
    )


if __name__ == "__main__":
    main()
