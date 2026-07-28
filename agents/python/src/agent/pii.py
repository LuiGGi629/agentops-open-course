"""PII redaction guardrail — Microsoft Presidio (Chapter 4.5).

Presidio detects and redacts personally identifiable information (emails, phone numbers, IP
addresses, names, …) **fully locally** — no API, no account, no key (MIT licensed). Common
credential shapes are masked before Presidio runs. This module wires callbacks at the outbound
model, inbound model, and tool-output boundaries, preventing detected sensitive text from
crossing them. Session ingestion happens earlier; trace and log safety therefore also relies on
telemetry content capture being disabled. The engines are built lazily (``functools.cache``) on
first use.
"""

from __future__ import annotations

import re
from functools import cache
from typing import Any, cast

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from presidio_analyzer import AnalyzerEngine, RecognizerResult
from presidio_analyzer.nlp_engine import NlpEngineProvider
from presidio_analyzer.predefined_recognizers import EmailRecognizer
from presidio_analyzer.recognizer_registry import RecognizerRegistry
from presidio_anonymizer import AnonymizerEngine
from tldextract import TLDExtract

# Use the small English spaCy model (pinned in pyproject.toml). The default Presidio engine would
# otherwise download the 400 MB ``en_core_web_lg`` at first run — this keeps the agent light and offline.
_NLP_CONFIG = {"nlp_engine_name": "spacy", "models": [{"lang_code": "en", "model_name": "en_core_web_sm"}]}
# Audit notes need a deliberately narrow stable policy so dates and generic IDs
# remain useful evidence while concrete personal data is still removed.
_PERSISTED_PII_ENTITIES = [
    "CREDIT_CARD",
    "CRYPTO",
    "EMAIL_ADDRESS",
    "IBAN_CODE",
    "IP_ADDRESS",
    "LOCATION",
    "MEDICAL_LICENSE",
    "PERSON",
    "PHONE_NUMBER",
    "US_BANK_NUMBER",
    "US_DRIVER_LICENSE",
    "US_ITIN",
    "US_PASSPORT",
    "US_SSN",
]
# Model and tool boundaries retain Presidio's broader default coverage. Only its
# ``ORGANIZATION`` class is excluded because it misclassifies identifiers such
# as ``INC-002`` and would corrupt the arguments the agent needs.
_BOUNDARY_PII_ENTITIES = [
    *_PERSISTED_PII_ENTITIES,
    "AGE",
    "DATE_TIME",
    "EMAIL",
    "ID",
    "MAC_ADDRESS",
    "NRP",
    "UK_NHS",
    "URL",
]
_CONTAINER_ENTITIES = frozenset({"EMAIL_ADDRESS", "URL"})
_DOMAIN_SAFE_TOKEN = re.compile(
    r"(?<![\w-])(?:"
    r"INC-\d+|"
    r"SEV\d+|"
    r"p\d{2,3}|"
    r"v\d+(?:\.\d+)+(?:-\d+)?|"
    r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})"
    r")(?![\w-])",
    re.IGNORECASE,
)
_CREDENTIAL_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (
        re.compile(
            r"\b(?P<label>(?:[A-Za-z][A-Za-z0-9]*_)*"
            r"(?:api[_-]?key|access[_-]?token|auth[_-]?token|password|secret|token))"
            r"\s*[:=]\s*(?:\"[^\"\r\n]*\"|'[^'\r\n]*'|[^\s,;]+)",
            re.IGNORECASE,
        ),
        r"\g<label>=<SECRET>",
    ),
    (
        re.compile(r"\bbearer\s+[A-Za-z0-9._~+/=-]{8,}", re.IGNORECASE),
        "Bearer <SECRET>",
    ),
    (
        re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9_-]{16,}|AIza[A-Za-z0-9_-]{20,})\b"),
        "<SECRET>",
    ),
)


def _redact_credentials(text: str) -> str:
    """Mask common credential forms before any text crosses a trust boundary."""
    for pattern, replacement in _CREDENTIAL_PATTERNS:
        text = pattern.sub(replacement, text)
    return text


class _OfflineEmailRecognizer(EmailRecognizer):
    """Validate email domains against tldextract's bundled snapshot only."""

    _extract = TLDExtract(suffix_list_urls=(), cache_dir=None)

    def validate_result(self, pattern_text: str) -> bool:
        return self._extract(pattern_text).fqdn != ""


@cache
def _analyzer() -> AnalyzerEngine:
    """Build the Presidio analyzer once, backed by the small spaCy model."""
    nlp_engine = NlpEngineProvider(nlp_configuration=_NLP_CONFIG).create_engine()
    registry = RecognizerRegistry(supported_languages=["en"])
    registry.load_predefined_recognizers(languages=["en"], nlp_engine=nlp_engine)
    registry.remove_recognizer(EmailRecognizer.__name__, language="en")
    registry.add_recognizer(_OfflineEmailRecognizer())
    return AnalyzerEngine(registry=registry, nlp_engine=nlp_engine, supported_languages=["en"])


@cache
def _anonymizer() -> AnonymizerEngine:
    """Build the Presidio anonymizer once (replaces spans with ``<ENTITY_TYPE>`` placeholders)."""
    return AnonymizerEngine()


def _analyze(text: str, entities: list[str]) -> list[RecognizerResult]:
    """Detect configured PII without corrupting exact operational evidence tokens."""
    container_entities = [entity for entity in entities if entity in _CONTAINER_ENTITIES]
    containers = (
        _analyzer().analyze(text=text, language="en", entities=container_entities) if container_entities else []
    )
    protected_spans = [
        match.span()
        for match in _DOMAIN_SAFE_TOKEN.finditer(text)
        if not any(match.start() < result.end and result.start < match.end() for result in containers)
    ]
    analysis_text = list(text)
    for start, end in protected_spans:
        # A same-length neutral token preserves every result offset. Underscores
        # keep adjacent names tokenized separately; whitespace can make spaCy
        # extend a PERSON span across the protected position.
        analysis_text[start:end] = "_" * (end - start)

    remaining_entities = [entity for entity in entities if entity not in _CONTAINER_ENTITIES]
    results = _analyzer().analyze(text="".join(analysis_text), language="en", entities=remaining_entities)
    residual = [
        result
        for result in results
        if not any(
            result.start < protected_end and protected_start < result.end
            for protected_start, protected_end in protected_spans
        )
        and not any(result.start < container.end and container.start < result.end for container in containers)
    ]
    return [*containers, *residual]


def redact_pii(text: str) -> str:
    """Return ``text`` with detected PII and credentials replaced by placeholders.

    Fully local and deterministic; a string with no PII is returned unchanged. This is a pure
    function, so it is trivially unit-testable ([4.2. Testing](../../../../docs/4.%20Quality/4.2.%20Testing.md)).
    """
    if not text.strip():
        return text
    redacted = _redact_credentials(text)
    results = _analyze(redacted, _BOUNDARY_PII_ENTITIES)
    if not results:
        return redacted
    # presidio_analyzer and presidio_anonymizer each declare their own RecognizerResult class; they are
    # runtime-compatible (this is Presidio's canonical usage) but nominally distinct to the type checker.
    return _anonymizer().anonymize(text=redacted, analyzer_results=cast("Any", results)).text


def redact_persisted_text(text: str) -> str:
    """Redact PII and obvious credentials before free text reaches local state.

    Audit evidence benefits from preserving domain identifiers such as
    ``INC-002``, so the persisted-text policy excludes Presidio's broad
    ``ORGANIZATION`` recognizer while retaining concrete personal-data classes.
    Credential tripwires cover common labeled secrets and token prefixes.
    """
    redacted = _redact_credentials(text)
    results = _analyze(redacted, _PERSISTED_PII_ENTITIES)
    if not results:
        return redacted
    return _anonymizer().anonymize(text=redacted, analyzer_results=cast("Any", results)).text


def redact_persisted_value(value: Any) -> Any:
    """Recursively apply the local persistence policy to structured values."""
    if isinstance(value, str):
        return redact_persisted_text(value)
    if isinstance(value, dict):
        return {key: redact_persisted_value(item) for key, item in value.items()}
    if isinstance(value, list):
        return [redact_persisted_value(item) for item in value]
    if isinstance(value, tuple):
        return tuple(redact_persisted_value(item) for item in value)
    return value


def _redact_value(value: Any) -> Any:
    """Recursively redact strings in tool arguments and structured results."""
    if isinstance(value, str):
        return redact_pii(value)
    if isinstance(value, dict):
        return {key: _redact_value(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_redact_value(item) for item in value]
    if isinstance(value, tuple):
        return tuple(_redact_value(item) for item in value)
    return value


def redact_request_pii(callback_context: CallbackContext, llm_request: LlmRequest) -> None:
    """``before_model_callback``: mask PII in every text part before the request leaves the process.

    Returning ``None`` lets the now-redacted request proceed. Mutating the request in place guarantees
    that detected PII is masked at the provider boundary; telemetry content capture is controlled separately.
    """
    del callback_context  # part of the ADK callback signature; unused here
    for content in llm_request.contents:
        for part in content.parts or []:
            if part.text:
                part.text = redact_pii(part.text)
            if part.function_call and part.function_call.args:
                part.function_call.args = _redact_value(dict(part.function_call.args))
            if part.function_response and part.function_response.response:
                part.function_response.response = _redact_value(dict(part.function_response.response))
    # Returning None (implicitly) lets the now-redacted request proceed to the model.


def redact_response_pii(callback_context: CallbackContext, llm_response: LlmResponse) -> LlmResponse | None:
    """``after_model_callback``: redact text and tool arguments before caller/tool use."""
    del callback_context
    changed = False
    if llm_response.content:
        for part in llm_response.content.parts or []:
            if part.text:
                redacted = redact_pii(part.text)
                changed = changed or redacted != part.text
                part.text = redacted
            if part.function_call and part.function_call.args:
                redacted_args = _redact_value(dict(part.function_call.args))
                changed = changed or redacted_args != part.function_call.args
                part.function_call.args = redacted_args
    return llm_response if changed else None


def redact_tool_output_pii(
    tool: BaseTool,
    args: dict[str, Any],
    tool_context: ToolContext,
    tool_response: dict[str, Any],
) -> dict[str, Any] | None:
    """``after_tool_callback``: redact structured tool output before reuse by the model."""
    del tool, args, tool_context
    redacted = _redact_value(tool_response)
    return redacted if redacted != tool_response else None
