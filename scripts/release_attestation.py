"""Verify that one policy-valid DSSE attestation matches the release SBOM."""

from __future__ import annotations

import argparse
import base64
import json
import pathlib
import sys
from collections.abc import Sequence


def predicates(document: object) -> list[object]:
    """Decode every DSSE payload and return its predicate."""
    envelopes = document if isinstance(document, list) else [document]
    found: list[object] = []
    for envelope in envelopes:
        if not isinstance(envelope, dict):
            raise ValueError("every verified attestation needs one base64 payload")
        payload = envelope.get("payload")
        if not isinstance(payload, str):
            raise ValueError("every verified attestation needs one base64 payload")
        statement = json.loads(base64.b64decode(payload, validate=True))
        if not isinstance(statement, dict) or "predicate" not in statement:
            raise ValueError("every verified attestation payload needs a predicate")
        found.append(statement["predicate"])
    return found


def verify(document: object, sbom: object) -> int:
    """Return the attestation count when at least one predicate equals the SBOM."""
    found = predicates(document)
    if not found:
        raise ValueError("no policy-valid SPDX attestations were returned")
    if sbom not in found:
        raise ValueError("no policy-valid SPDX attestation matches the release SBOM asset")
    return len(found)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    """Parse explicit artifact paths."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--attestations", type=pathlib.Path, required=True)
    parser.add_argument("--sbom", type=pathlib.Path, required=True)
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    """Verify the artifacts and print the reviewed envelope count."""
    args = parse_args(argv)
    attestations = json.loads(args.attestations.read_text(encoding="utf-8"))
    sbom = json.loads(args.sbom.read_text(encoding="utf-8"))
    count = verify(attestations, sbom)
    sys.stdout.write(f"verified {count} policy-valid SPDX attestation envelope(s)\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
