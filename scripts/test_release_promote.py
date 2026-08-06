"""Focused tests for the release promotion registry-state classifier."""

from __future__ import annotations

import json
import os
import pathlib
import shutil
import stat
import subprocess
import tempfile
import unittest


class ReleasePromoteTests(unittest.TestCase):
    def run_classifier(self, versions: object) -> subprocess.CompletedProcess[str]:
        """Source the shell library with a deterministic fake GitHub API."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            gh = root / "gh"
            gh.write_text("#!/usr/bin/env bash\nprintf '%s\\n' \"$GH_FIXTURE\"\n", encoding="utf-8")
            gh.chmod(gh.stat().st_mode | stat.S_IXUSR)
            env = {
                **os.environ,
                "GH_FIXTURE": json.dumps(versions),
                "GITHUB_REPOSITORY": "MLOps-Courses/agentops-open-course",
                "PATH": f"{root}:{os.environ['PATH']}",
                "VERSION": "v1.0.0",
            }
            bash = shutil.which("bash")
            assert bash is not None
            # The executable and command are repository-owned constants; only JSON fixture data
            # reaches the fake gh process through a dedicated environment variable.
            return subprocess.run(  # noqa: S603
                [bash, "-c", "source scripts/release-promote.sh; version_digest_for_tag agent"],
                cwd=pathlib.Path(__file__).resolve().parent.parent,
                env=env,
                check=False,
                capture_output=True,
                text=True,
            )

    def test_absent_tag_is_recoverable(self) -> None:
        result = self.run_classifier([[]])
        assert result.returncode == 0
        assert result.stdout == "\n"

    def test_exclusive_tag_returns_its_digest(self) -> None:
        digest = "sha256:" + "a" * 64
        result = self.run_classifier([[{"name": digest, "metadata": {"container": {"tags": ["v1.0.0"]}}}]])
        assert result.returncode == 0
        assert result.stdout.strip() == digest

    def test_tag_sharing_a_package_version_is_rejected(self) -> None:
        result = self.run_classifier(
            [[{"name": "sha256:bad", "metadata": {"container": {"tags": ["latest", "v1.0.0"]}}}]]
        )
        assert result.returncode != 0
