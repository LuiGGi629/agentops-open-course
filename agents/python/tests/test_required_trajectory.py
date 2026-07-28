"""Offline tests for required-argument trajectory matching."""

import pytest
from google.adk.evaluation.eval_case import IntermediateData, Invocation
from google.adk.evaluation.eval_metrics import EvalMetric, ToolTrajectoryCriterion
from google.adk.evaluation.evaluator import EvalStatus
from google.genai import types

from evals.required_trajectory import (
    RequiredToolTrajectoryEvaluator,
    evaluate_required_tool_trajectory,
    required_tools_in_order,
)


def _call(tool_name: str, **args) -> types.FunctionCall:
    return types.FunctionCall(name=tool_name, args=args)


def _invocation(*calls: types.FunctionCall) -> Invocation:
    return Invocation(
        user_content=types.Content(role="user", parts=[types.Part(text="test")]),
        intermediate_data=IntermediateData(tool_uses=list(calls)),
    )


def _metric(match_type: str = "IN_ORDER") -> EvalMetric:
    return EvalMetric(
        metric_name="tool_trajectory_avg_score",
        criterion=ToolTrajectoryCriterion(threshold=1.0, match_type=match_type),
    )


def test_required_calls_allow_extra_calls_and_optional_arguments() -> None:
    actual = [
        _call("list_incidents", status="open"),
        _call("get_incident", incident_id="INC-002"),
        _call("search_service_logs", service="inventory", query="crash-loop", limit=10),
        _call("get_service_status", name="inventory"),
        _call("get_runbook", slug="service-down"),
    ]
    expected = [
        _call("get_incident", incident_id="INC-002"),
        _call("search_service_logs", service="inventory"),
        _call("get_runbook", slug="service-down"),
    ]
    assert required_tools_in_order(actual, expected)


def test_required_calls_reject_wrong_values_and_order() -> None:
    expected = [_call("get_incident", incident_id="INC-002"), _call("get_runbook", slug="service-down")]
    assert not required_tools_in_order([_call("get_incident", incident_id="INC-001")], expected)
    assert not required_tools_in_order(list(reversed(expected)), expected)


def test_required_calls_compare_nested_required_values() -> None:
    actual = [_call("query", filters={"service": "inventory", "status": "open"}, limit=10)]
    expected = [_call("query", filters={"service": "inventory"})]
    assert required_tools_in_order(actual, expected)


def test_required_values_do_not_confuse_json_booleans_and_numbers() -> None:
    assert not required_tools_in_order([_call("query", limit=True)], [_call("query", limit=1)])
    assert not required_tools_in_order([_call("query", limit=1)], [_call("query", limit=True)])
    assert not required_tools_in_order([_call("query", flags=[True])], [_call("query", flags=[1])])


def test_required_lists_compare_nested_values_recursively() -> None:
    actual = [_call("query", filters=[{"service": "inventory", "status": "open"}])]
    expected = [_call("query", filters=[{"service": "inventory"}])]
    assert required_tools_in_order(actual, expected)
    assert not required_tools_in_order(actual, [_call("query", filters=[{"service": "payments"}])])
    assert not required_tools_in_order(actual, [_call("query", filters=[])])


def test_adk_adapter_scores_each_invocation_strictly() -> None:
    expected = [_invocation(_call("get_incident", incident_id="INC-002"))]
    passing = [_invocation(_call("get_incident", incident_id="INC-002", detail=True))]
    result = evaluate_required_tool_trajectory(_metric(), passing, expected)
    assert result.overall_score == 1.0
    assert result.overall_eval_status is EvalStatus.PASSED
    assert result.per_invocation_results[0].eval_status is EvalStatus.PASSED

    failing = [_invocation(_call("get_incident", incident_id="INC-001"))]
    result = evaluate_required_tool_trajectory(_metric(), failing, expected)
    assert result.overall_score == 0.0
    assert result.overall_eval_status is EvalStatus.FAILED


def test_adk_adapter_handles_empty_or_mismatched_invocations() -> None:
    evaluator = RequiredToolTrajectoryEvaluator(_metric())
    assert evaluator.evaluate_invocations([], []).overall_eval_status is EvalStatus.NOT_EVALUATED
    with pytest.raises(ValueError, match="invocation counts"):
        evaluator.evaluate_invocations([_invocation()], [_invocation(), _invocation()])
    with pytest.raises(ValueError, match="expected_invocations"):
        evaluator.evaluate_invocations([_invocation()])


def test_adk_adapter_rejects_unsupported_match_type() -> None:
    with pytest.raises(ValueError, match="only IN_ORDER"):
        RequiredToolTrajectoryEvaluator(_metric("EXACT"))
