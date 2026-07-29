"""Strict ordered tool trajectories with required-argument subset matching.

ADK's built-in ``IN_ORDER`` matcher allows extra calls but compares each
argument dictionary exactly. That makes an otherwise valid call fail when a
model supplies optional arguments such as a search query or result limit.
This metric keeps the strict ordered contract while treating the evalset's
arguments as the required subset of each actual call.
"""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, ClassVar

from google.adk.evaluation.eval_case import ConversationScenario, Invocation, get_all_tool_calls
from google.adk.evaluation.eval_metrics import EvalMetric, ToolTrajectoryCriterion
from google.adk.evaluation.evaluator import EvalStatus, EvaluationResult, Evaluator, PerInvocationResult
from google.genai import types


def contains_required(actual: Any, required: Any) -> bool:
    """Return whether ``actual`` contains every recursively required value.

    An expected empty string accepts an omitted actual key because ADK records
    only model-supplied arguments and the corresponding function default is
    empty. Supplying any non-empty value still fails that explicit contract.
    """
    if isinstance(required, list):
        return (
            isinstance(actual, list)
            and len(actual) == len(required)
            and all(
                contains_required(actual_item, required_item)
                for actual_item, required_item in zip(actual, required, strict=True)
            )
        )
    if not isinstance(required, Mapping):
        if isinstance(actual, bool) or isinstance(required, bool):
            return type(actual) is type(required) and actual == required
        return actual == required
    if not isinstance(actual, Mapping):
        return False
    for key, value in required.items():
        if key not in actual:
            if isinstance(value, str) and value == "":
                continue
            return False
        if not contains_required(actual[key], value):
            return False
    return True


def required_tools_in_order(
    actual_calls: list[types.FunctionCall],
    expected_calls: list[types.FunctionCall],
) -> bool:
    """Return whether every required call occurs in order with matching required arguments."""
    remaining = iter(actual_calls)
    return all(
        any(
            actual.name == expected.name and contains_required(actual.args or {}, expected.args or {})
            for actual in remaining
        )
        for expected in expected_calls
    )


class RequiredToolTrajectoryEvaluator(Evaluator):
    """ADK evaluator adapter for strict required-call subset semantics."""

    criterion_type: ClassVar[type[ToolTrajectoryCriterion]] = ToolTrajectoryCriterion

    def __init__(self, eval_metric: EvalMetric):
        if eval_metric.criterion is None:
            raise ValueError("required tool trajectory needs a criterion")
        criterion = self.criterion_type.model_validate(eval_metric.criterion.model_dump())
        if criterion.match_type is not ToolTrajectoryCriterion.MatchType.IN_ORDER:
            raise ValueError("required tool trajectory supports only IN_ORDER matching")
        self._threshold = criterion.threshold

    def evaluate_invocations(
        self,
        actual_invocations: list[Invocation],
        expected_invocations: list[Invocation] | None = None,
        conversation_scenario: ConversationScenario | None = None,
    ) -> EvaluationResult:
        """Score every invocation, then apply the configured strict threshold."""
        del conversation_scenario
        if expected_invocations is None:
            raise ValueError("expected_invocations is needed by this metric")
        if len(actual_invocations) != len(expected_invocations):
            raise ValueError("actual and expected invocation counts must match")

        per_invocation: list[PerInvocationResult] = []
        for actual, expected in zip(actual_invocations, expected_invocations, strict=False):
            matches = required_tools_in_order(
                get_all_tool_calls(actual.intermediate_data),
                get_all_tool_calls(expected.intermediate_data),
            )
            score = float(matches)
            per_invocation.append(
                PerInvocationResult(
                    actual_invocation=actual,
                    expected_invocation=expected,
                    score=score,
                    eval_status=self._status(score),
                )
            )

        if not per_invocation:
            return EvaluationResult()
        overall = sum(item.score or 0.0 for item in per_invocation) / len(per_invocation)
        return EvaluationResult(
            overall_score=overall,
            overall_eval_status=self._status(overall),
            per_invocation_results=per_invocation,
        )

    def _status(self, score: float) -> EvalStatus:
        return EvalStatus.PASSED if score >= self._threshold else EvalStatus.FAILED


def evaluate_required_tool_trajectory(
    eval_metric: EvalMetric,
    actual_invocations: list[Invocation],
    expected_invocations: list[Invocation] | None,
    conversation_scenario: ConversationScenario | None = None,
) -> EvaluationResult:
    """Entry point loaded by ADK's custom-metric registry."""
    return RequiredToolTrajectoryEvaluator(eval_metric).evaluate_invocations(
        actual_invocations,
        expected_invocations,
        conversation_scenario,
    )
