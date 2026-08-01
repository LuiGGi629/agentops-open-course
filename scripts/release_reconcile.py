"""Validate release indexes and fail-closed reconciliation ownership."""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any, Final

_DIGEST: Final = re.compile(r"sha256:[0-9a-f]{64}")
_SHA: Final = re.compile(r"[0-9a-f]{40}")
_VERSION: Final = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+")
_DOCKER_INDEX_MEDIA_TYPE: Final = "application/vnd.docker.distribution.manifest.list.v2+json"
_OCI_INDEX_MEDIA_TYPE: Final = "application/vnd.oci.image.index.v1+json"
_INDEX_MEDIA_TYPES: Final = {_DOCKER_INDEX_MEDIA_TYPE, _OCI_INDEX_MEDIA_TYPE}


def _object(path: Path) -> dict[str, Any]:
    document = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(document, dict):
        raise ValueError(f"{path} must contain one JSON object")
    return document


def _package_versions(path: Path) -> list[dict[str, Any]]:
    """Return the package records from ``gh api --paginate --slurp`` output."""
    document = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(document, list):
        raise ValueError(f"{path} must contain a JSON array of package pages")
    records: list[dict[str, Any]] = []
    for page in document:
        if not isinstance(page, list):
            raise ValueError(f"{path} contains a non-array package page")
        for record in page:
            if not isinstance(record, dict):
                raise ValueError(f"{path} contains a non-object package version")
            records.append(record)
    return records


def _tags(record: dict[str, Any]) -> list[str]:
    metadata = record.get("metadata")
    container = metadata.get("container") if isinstance(metadata, dict) else None
    tags = container.get("tags") if isinstance(container, dict) else None
    if not isinstance(tags, list) or not all(isinstance(tag, str) for tag in tags):
        raise ValueError("package version has an invalid container tag inventory")
    return tags


def validate_release_index(
    index: dict[str, Any],
    source_image: dict[str, Any],
    *,
    version: str,
    sha: str,
    source_digest: str,
) -> dict[str, str]:
    """Prove that one version index wraps only the qualified source digest."""
    if not _VERSION.fullmatch(version):
        raise ValueError("release version must be a v-prefixed three-part version")
    if not _SHA.fullmatch(sha):
        raise ValueError("release source must be a full lowercase commit SHA")
    if not _DIGEST.fullmatch(source_digest):
        raise ValueError("release source digest must be an immutable SHA-256")

    media_type = index.get("mediaType")
    manifests = index.get("manifests")
    if media_type not in _INDEX_MEDIA_TYPES:
        raise ValueError("release tag does not resolve to an OCI or Docker index")
    if not isinstance(manifests, list) or len(manifests) != 1 or not isinstance(manifests[0], dict):
        raise ValueError("release index must contain exactly one source manifest")
    if manifests[0].get("digest") != source_digest:
        raise ValueError("release index does not contain the qualified source digest")

    config = source_image.get("config")
    labels = config.get("Labels") if isinstance(config, dict) else None
    if (
        not isinstance(labels, dict)
        or labels.get("org.opencontainers.image.revision") != sha
        or labels.get("org.opencontainers.image.version") != version
    ):
        raise ValueError("source image labels do not identify the qualified release")

    annotations = index.get("annotations")
    if media_type == _OCI_INDEX_MEDIA_TYPE:
        if (
            not isinstance(annotations, dict)
            or annotations.get("org.opencontainers.image.revision") != sha
            or annotations.get("org.opencontainers.image.version") != version
        ):
            raise ValueError("OCI release index annotations do not identify the qualified source")
    elif media_type == _DOCKER_INDEX_MEDIA_TYPE and annotations not in (None, {}):
        # Docker manifest lists cannot carry OCI index annotations. Their
        # authority is the exact signed child digest supplied by qualification.
        raise ValueError("Docker release index contains unsupported annotations")

    return {"state": "valid", "media_type": media_type}


def validate_reconcile_target(
    package_versions: list[dict[str, Any]],
    *,
    version: str,
    sha: str,
    source_digest: str,
    index: dict[str, Any] | None = None,
    source_image: dict[str, Any] | None = None,
    resolved_digest: str | None = None,
    registry_absent: bool = False,
) -> dict[str, str | int]:
    """Return one exact package version to delete, or prove that no version tag exists."""
    if not _VERSION.fullmatch(version):
        raise ValueError("release version must be a v-prefixed three-part version")
    if not _SHA.fullmatch(sha):
        raise ValueError("release source must be a full lowercase commit SHA")
    if not _DIGEST.fullmatch(source_digest):
        raise ValueError("release source digest must be an immutable SHA-256")

    if (index is None) != (resolved_digest is None):
        raise ValueError("registry index and resolved digest must be supplied together")
    if (index is None) != (source_image is None):
        raise ValueError("registry index and source image must be supplied together")
    registry_present = index is not None or resolved_digest is not None
    if registry_present and registry_absent:
        raise ValueError("registry tag cannot be both present and absent")

    matching = [record for record in package_versions if version in _tags(record)]
    if not matching:
        if registry_present:
            raise ValueError("registry tag has no uniquely owned package version")
        if not registry_absent:
            raise ValueError("registry tag absence was not proven")
        return {"state": "absent"}
    if len(matching) != 1:
        raise ValueError("more than one package version carries the release tag")

    record = matching[0]
    if _tags(record) != [version]:
        raise ValueError("release package version carries another tag")
    version_id = record.get("id")
    if isinstance(version_id, bool) or not isinstance(version_id, int) or version_id < 1:
        raise ValueError("release package version has no positive numeric id")
    package_digest = record.get("name")
    if not isinstance(package_digest, str) or not _DIGEST.fullmatch(package_digest):
        raise ValueError("release package version has no immutable index digest")
    if index is None or source_image is None or resolved_digest is None:
        raise ValueError("owned package version does not resolve to complete registry evidence")
    if not _DIGEST.fullmatch(resolved_digest) or resolved_digest != package_digest:
        raise ValueError("registry and package API disagree on the release index digest")

    validate_release_index(
        index,
        source_image,
        version=version,
        sha=sha,
        source_digest=source_digest,
    )

    return {"state": "owned", "version_id": version_id, "digest": package_digest}


def main(argv: Sequence[str] | None = None) -> None:
    """Validate downloaded package/index documents and print minimized ownership evidence."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--versions", type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--source-digest", required=True)
    parser.add_argument("--index", type=Path)
    parser.add_argument("--source-image", type=Path)
    parser.add_argument("--resolved-digest")
    parser.add_argument("--registry-absent", action="store_true")
    parser.add_argument("--validate-index-only", action="store_true")
    arguments = parser.parse_args(argv)
    try:
        if arguments.validate_index_only:
            if arguments.index is None or arguments.source_image is None:
                parser.error("--validate-index-only requires --index and --source-image")
            if arguments.versions is not None or arguments.resolved_digest is not None or arguments.registry_absent:
                parser.error("--validate-index-only cannot reconcile package state")
            result = validate_release_index(
                _object(arguments.index),
                _object(arguments.source_image),
                version=arguments.version,
                sha=arguments.sha,
                source_digest=arguments.source_digest,
            )
        else:
            if arguments.versions is None:
                parser.error("reconciliation requires --versions")
            result = validate_reconcile_target(
                _package_versions(arguments.versions),
                version=arguments.version,
                sha=arguments.sha,
                source_digest=arguments.source_digest,
                index=_object(arguments.index) if arguments.index is not None else None,
                source_image=_object(arguments.source_image) if arguments.source_image is not None else None,
                resolved_digest=arguments.resolved_digest,
                registry_absent=arguments.registry_absent,
            )
    except (OSError, ValueError, json.JSONDecodeError) as error:
        raise SystemExit(f"error: {error}") from error
    sys.stdout.write(json.dumps(result, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
