"""Verify the exact state restored by the scheduled platform drill."""

from __future__ import annotations

import hashlib
import json
import pathlib
import sqlite3
import sys
from contextlib import closing

from platform_backup_mutate import evidence_checks  # ty: ignore[unresolved-import]


def verify(snapshot: pathlib.Path, state: pathlib.Path, expected_commit: str, evidence: dict[str, str]) -> None:
    """Check provenance, inventory, hashes, SQLite integrity, cleanup, and restored rows."""
    manifest = json.loads((snapshot / "manifest.json").read_text(encoding="utf-8"))
    if manifest["source"]["commit"] != expected_commit:
        raise RuntimeError("restored snapshot commit does not match the workflow revision")
    expected = {item["filename"]: item for item in manifest["databases"]}
    if not {"incidents.db", "memory.db", "runtime.db"} <= set(expected):
        raise RuntimeError("snapshot is missing a required persistent database")
    if {path.name for path in state.glob("*.db")} != set(expected):
        raise RuntimeError("restored state database inventory does not match the manifest")

    for name, item in expected.items():
        database = state / name
        if hashlib.sha256(database.read_bytes()).hexdigest() != item["sha256"]:
            raise RuntimeError(f"restored {name} hash does not match its manifest")
        with closing(sqlite3.connect(database)) as connection:
            if connection.execute("PRAGMA integrity_check").fetchone() != ("ok",):
                raise RuntimeError(f"restored {name} failed SQLite integrity_check")
            tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_schema WHERE type = 'table'")}
        if "ci_restore_sentinel" in tables:
            raise RuntimeError(f"restore retained the destructive sentinel in {name}")

    for name, statement, parameters in evidence_checks(evidence):
        uri = (state / name).resolve().as_uri() + "?mode=ro"
        with closing(sqlite3.connect(uri, uri=True)) as connection:
            if connection.execute(statement, parameters).fetchone() != (1,):
                raise RuntimeError(f"restore did not recover {name} evidence")


def main() -> None:
    """Load the snapshot, state, revision, and evidence paths from the restore Job."""
    snapshot = pathlib.Path(sys.argv[1])
    state = pathlib.Path(sys.argv[2])
    expected_commit = sys.argv[3]
    evidence = json.loads(pathlib.Path(sys.argv[4]).read_text(encoding="utf-8"))
    verify(snapshot, state, expected_commit, evidence)


if __name__ == "__main__":
    main()
