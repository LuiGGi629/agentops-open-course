"""Offline tests for the deterministic groundedness / citation-coverage logic."""

import pytest

from evals import groundedness_eval
from evals.groundedness_eval import claimed_entities, unsupported_claims


@pytest.fixture(autouse=True)
def ignore_retained_workflow_transcripts(monkeypatch) -> None:
    monkeypatch.delenv("AGENT_EVAL_OBSERVED_PATH", raising=False)


def test_claimed_entities_extracts_ids_services_and_runbooks() -> None:
    text = "INC-002 on payments is SEV1; see the cascade-failure runbook."
    assert claimed_entities(text) == {"inc-002", "sev1", "payments", "cascade-failure"}


def test_service_terms_match_whole_tokens_only() -> None:
    # "auth" must not fire on "authored"; "cache" must not fire on "cached-out".
    assert claimed_entities("The change was authored last week") == set()


def test_ambiguous_search_verb_is_not_a_service_claim() -> None:
    assert claimed_entities("I can search the logs for more evidence.") == set()
    assert claimed_entities("I can cache the result for reuse.") == set()


def test_ambiguous_service_status_is_still_a_claim() -> None:
    assert claimed_entities("Search appears degraded.") == {"search"}
    assert claimed_entities("Search has elevated errors.") == {"search"}
    assert claimed_entities("Cache is operational.") == {"cache"}


def test_ambiguous_service_is_still_checked_with_service_context() -> None:
    problems = unsupported_claims(
        ["The search service is degraded."],
        ['{"service": "inventory", "status": "healthy"}'],
        ["What is degraded?"],
    )
    assert problems == ["turn 1: answer claims 'search' with no supporting evidence"]


def test_ambiguous_service_accepts_canonical_nested_name_evidence() -> None:
    assert (
        unsupported_claims(
            ["The search service is degraded."],
            ['{"service": {"name": "search", "status": "degraded"}}'],
            ["What is degraded?"],
        )
        == []
    )


def test_grounded_answer_has_no_unsupported_claims() -> None:
    responses = ["INC-002 on payments is down."]
    evidence = ['{"id": "INC-002", "service": "payments", "status": "down"}']
    questions = ["What is happening with payments?"]
    assert unsupported_claims(responses, evidence, questions) == []


def test_entity_from_the_question_counts_as_grounded() -> None:
    # The user named the service; echoing it back is not a hallucination.
    responses = ["I could not find any incident for warehouse."]
    evidence = ["{}"]
    questions = ["What incidents affect warehouse?"]
    assert unsupported_claims(responses, evidence, questions) == []


def test_fabricated_incident_is_reported() -> None:
    responses = ["The root cause is INC-999, which I recommend resolving."]
    evidence = ['{"id": "INC-002"}']
    questions = ["Investigate INC-002."]
    problems = unsupported_claims(responses, evidence, questions)
    assert len(problems) == 1
    assert "inc-999" in problems[0]


def test_per_turn_grounding_is_independent() -> None:
    responses = ["INC-001 is open.", "INC-002 is resolved."]
    evidence = ['{"id": "INC-001"}', "{}"]  # turn 2 never retrieved INC-002
    questions = ["First?", "Second?"]
    problems = unsupported_claims(responses, evidence, questions)
    assert problems == ["turn 2: answer claims 'inc-002' with no supporting evidence"]


def test_measure_retains_the_transcript_needed_to_audit_a_failure(monkeypatch) -> None:
    monkeypatch.setattr(
        groundedness_eval,
        "_load_cases",
        lambda: [{"inputs": {"eval_id": "fabricated", "turns": ["Investigate INC-002."]}}],
    )
    monkeypatch.setattr(
        groundedness_eval,
        "ask",
        lambda _turns, _eval_id: {
            "responses": ["INC-999 caused it."],
            "evidence": ['{"id": "INC-002"}'],
            "provider_errors": [[]],
        },
    )
    observed = groundedness_eval.measure()["fabricated"]
    assert observed["questions"] == ["Investigate INC-002."]
    assert observed["responses"] == ["INC-999 caused it."]
    assert observed["evidence"] == ['{"id": "INC-002"}']
    assert observed["provider_errors"] == []
    assert observed["unsupported_claims"] == ["turn 1: answer claims 'inc-999' with no supporting evidence"]


def test_measure_retains_provider_failure_instead_of_reporting_vacuous_grounding(monkeypatch) -> None:
    monkeypatch.setattr(
        groundedness_eval,
        "_load_cases",
        lambda: [{"inputs": {"eval_id": "degraded", "turns": ["Investigate INC-002."]}}],
    )
    monkeypatch.setattr(
        groundedness_eval,
        "ask",
        lambda _turns, _eval_id: {
            "responses": ["The model provider is unavailable."],
            "evidence": [""],
            "provider_errors": [[{"code": "MODEL_UNAVAILABLE", "message": "Model request failed safely."}]],
        },
    )

    observed = groundedness_eval.measure()["degraded"]
    assert observed["provider_errors"] == ["turn 1: MODEL_UNAVAILABLE: Model request failed safely."]
    assert observed["unsupported_claims"] == []


def test_measure_reuses_the_exact_mlflow_transcript_when_configured(monkeypatch) -> None:
    monkeypatch.setenv("AGENT_EVAL_OBSERVED_PATH", "evals/model-observed.json")
    monkeypatch.setenv("EVAL_MODEL_DIGEST", "sha256:canonical")
    monkeypatch.setattr(
        groundedness_eval,
        "_load_cases",
        lambda: [{"inputs": {"eval_id": "lookup", "turns": ["Investigate INC-002."]}}],
    )

    def load(path, *, expected_cases, model_digest):
        assert str(path) == "evals/model-observed.json"
        assert expected_cases == [{"inputs": {"eval_id": "lookup", "turns": ["Investigate INC-002."]}}]
        assert model_digest == "sha256:canonical"
        return {
            "lookup": {
                "responses": ["INC-002 is open."],
                "evidence": ['{"id": "INC-002"}'],
                "provider_errors": [[]],
            }
        }

    monkeypatch.setattr(groundedness_eval, "load_model_observations", load)
    monkeypatch.setattr(
        groundedness_eval,
        "ask",
        lambda *_args: pytest.fail("retained evidence must avoid a new model call"),
    )

    observed = groundedness_eval.measure()["lookup"]
    assert observed["responses"] == ["INC-002 is open."]
    assert observed["unsupported_claims"] == []


def test_main_module_exposes_measure_and_main() -> None:
    # measure()/main() are model-backed (weekly lane); assert they are importable callables.
    assert callable(groundedness_eval.measure)
    assert callable(groundedness_eval.main)
