"""Regression fixtures for the course authoring contracts."""

# The repository executes this unittest module directly; assertions keep fixtures concise.

from __future__ import annotations

import json
import pathlib
import shutil
import tempfile
import unittest
from unittest import mock

from scripts import check_conventions, course_evidence  # ty: ignore[unresolved-import]


def copy_contract_files(root: pathlib.Path, relative_paths: tuple[str, ...]) -> None:
    """Copy current repository authorities so tests mutate realistic fixtures."""
    for relative in relative_paths:
        source = check_conventions.ROOT / relative
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)


def contract_pages(root: pathlib.Path, relative_paths: tuple[str, ...]) -> dict[pathlib.Path, str]:
    """Load copied Markdown pages using the same absolute-key shape as the gate."""
    return {root / relative: (root / relative).read_text(encoding="utf-8") for relative in relative_paths}


class SourceContractTests(unittest.TestCase):
    def test_maintainer_drift_contracts_cover_the_current_tree(self) -> None:
        pages = {
            page: page.read_text(encoding="utf-8") for page in check_conventions.ROOT.joinpath("content").rglob("*.md")
        }
        assert check_conventions.check_maintainer_drift_contracts(pages) == []

    def test_maintainer_drift_contract_rejects_hook_and_metric_changes(self) -> None:
        docs = (
            "content/1. Setup/1.0. System.md",
            "content/1. Setup/1.5. Workspace.md",
            "content/4. Quality/4.1. Linting.md",
            "content/4. Quality/4.3. Metrics.md",
            "content/8. Community/8.5. Contributions.md",
        )
        owners = ("lefthook.yml", "scripts/doctor.sh", ".github/workflows/ci.yml", "agents/python/src/agent/budget.py")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            budget = root / "agents/python/src/agent/budget.py"
            text = budget.read_text(encoding="utf-8")
            budget.write_text(text.replace('"agentops.tokens"', '"agentops.tokens.changed"', 1), encoding="utf-8")
            workspace = root / "content/1. Setup/1.5. Workspace.md"
            text = workspace.read_text(encoding="utf-8")
            workspace.write_text(text.replace("secure:staged; pre-push", "secure; pre-push", 1), encoding="utf-8")
            problems = check_conventions.check_maintainer_drift_contracts(contract_pages(root, docs), root=root)
        messages = {message for _, message in problems}
        assert any("lefthook task description drifted" in message for message in messages)
        assert any("custom metric inventory drifted" in message for message in messages)

    def test_exact_line_test_and_module_counts_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            page = root / "content/example.md"
            problems = check_conventions.check_exact_count_claims(
                {page: "Exactly 16 tests pass.\n```text\nExactly 4 tests pass.\n```\n"},
                root=root,
            )
        assert problems == [
            (
                "content/example.md",
                "line 1: replace brittle exact line/test/module count with derived or count-free evidence",
            )
        ]

    def test_repository_python_inventory_rejects_an_orphan_script(self) -> None:
        problems = check_conventions.compare_repository_python_inventory(
            {"scripts/owned.py"},
            {"scripts/owned.py", "scripts/foo.py"},
        )
        assert any("scripts/foo.py" in message for _, message in problems)

    def test_repository_python_inventory_covers_the_current_tree(self) -> None:
        assert check_conventions.check_repository_python_inventory() == []

    def test_checked_snippet_requires_one_existing_bounded_source_region(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / "infra/example.yaml"
            source.parent.mkdir(parents=True)
            source.write_text(
                "# --8<-- [start:trusted]\nvalue: 1\n# --8<-- [end:trusted]\n",
                encoding="utf-8",
            )
            text = '{{< include path="infra/example.yaml" region="trusted" lang="yaml" >}}\n'
            assert check_conventions.check_snippet_targets(pathlib.Path("content/example.md"), text, root=root) == []

            source.write_text("# --8<-- [start:trusted]\nvalue: 1\n", encoding="utf-8")
            problems = check_conventions.check_snippet_targets(pathlib.Path("content/example.md"), text, root=root)
        assert any("exactly one start and end marker" in message for _, message in problems)

    def test_source_snippet_ratchet_covers_every_non_python_source_format(self) -> None:
        pages = {
            check_conventions.ROOT / relative: check_conventions.ROOT.joinpath(relative).read_text(encoding="utf-8")
            for relative, _ in check_conventions.SOURCE_SNIPPET_SURFACES.values()
        }
        assert check_conventions.check_source_snippet_coverage(pages) == []

    def test_mlflow_point_version_copy_is_rejected_from_feedback_prose(self) -> None:
        feedback = check_conventions.ROOT / "content/7. Observability/7.4. Feedback.md"
        pages = {feedback: "The `agentops-mlflow` image (`99.99.99`) stores assessments.\n"}
        problems = check_conventions.check_source_versions(pages)
        assert any("MLflow version belongs" in message for _, message in problems)

    def test_changed_authoritative_pin_rejects_stale_copy(self) -> None:
        problems = check_conventions.compare_contract(
            "content/example.md",
            "tool pin",
            "2.0.0",
            "1.9.0",
        )
        assert problems == [("content/example.md", "tool pin drifted: expected '2.0.0', found '1.9.0'")]

    def test_changed_task_expansion_rejects_stale_command(self) -> None:
        problems = check_conventions.compare_contract(
            "content/example.md",
            "mise run web expansion",
            "uv run adk web src --port 8002",
            "uv run adk web src",
        )
        assert problems

    def test_changed_port_rejects_stale_documented_port(self) -> None:
        problems = check_conventions.compare_contract(
            "content/example.md",
            "ADK web port",
            "8002",
            "8000",
        )
        assert problems

    def test_changed_manifest_resource_rejects_stale_documented_name(self) -> None:
        problems = check_conventions.compare_contract(
            "content/example.md",
            "NetworkPolicy resource name",
            "otel-collector-ingress-v2",
            "otel-collector-ingress",
        )
        assert problems

    def test_real_python_owner_change_rejects_stale_support_prose(self) -> None:
        docs = (
            "content/1. Setup/1.0. System.md",
            "content/1. Setup/1.1. Python.md",
            "content/1. Setup/_index.md",
            "content/4. Quality/4.4. Evaluations.md",
        )
        owners = ("agents/python/pyproject.toml", "agents/python/mise.toml")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            manifest = root / "agents/python/pyproject.toml"
            text = manifest.read_text(encoding="utf-8")
            document = check_conventions.read_toml(manifest)
            project = document.get("project")
            assert isinstance(project, dict)
            requires_python = project.get("requires-python")
            assert isinstance(requires_python, str)
            owner_line = f'requires-python = "{requires_python}"'
            assert owner_line in text
            manifest.write_text(
                text.replace(owner_line, f'requires-python = "{requires_python},!=99.99.99"', 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_python_profile_contracts(contract_pages(root, docs), root=root)
        assert any("requires-python" in message for _, message in problems)

    def test_real_agentgateway_owner_change_rejects_every_declared_prose_copy(self) -> None:
        docs = tuple(
            path.relative_to(check_conventions.ROOT).as_posix()
            for path in check_conventions.ROOT.joinpath("content").rglob("*.md")
        )
        owners = (
            "pyproject.toml",
            "agents/python/pyproject.toml",
            "agents/python/Dockerfile",
            "infra/helmfile.yaml",
            "mise.toml",
            "scripts/install-helm-diff.sh",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            mise = root / "mise.toml"
            text = mise.read_text(encoding="utf-8")
            owner = '"github:agentgateway/agentgateway" = "1.4.1"'
            assert owner in text
            mise.write_text(text.replace(owner, '"github:agentgateway/agentgateway" = "9.9.9"', 1), encoding="utf-8")
            problems = check_conventions.check_source_versions(contract_pages(root, docs), root=root)
        assert any("agentgateway copy inventory drifted" in message for _, message in problems)

    def test_external_course_image_requires_a_digest(self) -> None:
        docs = tuple(
            path.relative_to(check_conventions.ROOT).as_posix()
            for path in check_conventions.ROOT.joinpath("content").rglob("*.md")
        )
        owners = (
            "pyproject.toml",
            "agents/python/pyproject.toml",
            "agents/python/Dockerfile",
            "infra/helmfile.yaml",
            "mise.toml",
            "scripts/install-helm-diff.sh",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            platform = root / "content/6. Platform/6.2. Platform Install.md"
            text = platform.read_text(encoding="utf-8")
            digest = "@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028"
            assert digest in text
            platform.write_text(text.replace(digest, "", 1), encoding="utf-8")
            problems = check_conventions.check_source_versions(contract_pages(root, docs), root=root)
        assert any("must include an immutable sha256 digest" in message for _, message in problems)

    def test_real_state_owner_change_rejects_stale_drill_result(self) -> None:
        docs = ("content/6. Platform/6.6. Platform Delivery.md",)
        owners = (
            "agents/python/src/agent/state.py",
            "infra/k8s/base/state-backup.yaml",
            "infra/scripts/backup-drill.sh",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            drill = root / "infra/scripts/backup-drill.sh"
            text = drill.read_text(encoding="utf-8")
            assert 'echo "drill passed:' in text
            drill.write_text(
                text.replace('echo "drill passed:', 'echo "replacement drill passed:', 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_state_course_contracts(contract_pages(root, docs), root=root)
        assert any("completion line drifted" in message for _, message in problems)

    def test_real_audit_owner_change_rejects_profile_mismatch(self) -> None:
        docs = (
            "content/8. Community/8.1. License.md",
            "content/4. Quality/4.1. Linting.md",
        )
        owners = ("scripts/check-licenses.sh", "scripts/check-vulnerabilities.sh")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            audit = root / "scripts/check-vulnerabilities.sh"
            text = audit.read_text(encoding="utf-8")
            assert 'audit_profile "agent evaluation"' in text
            audit.write_text(
                text.replace('audit_profile "agent evaluation"', 'audit_profile "agent extended evaluation"', 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_dependency_audit_course_contracts(
                contract_pages(root, docs),
                root=root,
            )
        assert any("same lock-owned set" in message for _, message in problems)

    def test_real_retrieval_owner_change_requires_new_provenance_field(self) -> None:
        docs = ("content/3. Capabilities/3.4. Memory.md",)
        owners = ("agents/python/src/agent/retrieval.py",)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            source = root / "agents/python/src/agent/retrieval.py"
            text = source.read_text(encoding="utf-8")
            needle = '        "chunk_count",\n    )'
            assert needle in text
            source.write_text(
                text.replace(needle, '        "chunk_count",\n        "runtime_version",\n    )', 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_retrieval_course_contracts(contract_pages(root, docs), root=root)
        assert any("`runtime_version`" in message for _, message in problems)

    def test_real_domain_owner_change_requires_capstone_review(self) -> None:
        docs = ("content/8. Community/8.7. Capstone.md",)
        owners = ("agents/python/src/agent/domain.py",)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            source = root / "agents/python/src/agent/domain.py"
            text = source.read_text(encoding="utf-8")
            needle = "    dependency_edges: tuple[tuple[str, str], ...]\n"
            assert needle in text
            source.write_text(
                text.replace(needle, needle + "    ownership_labels: tuple[str, ...]\n", 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_domain_course_contracts(contract_pages(root, docs), root=root)
        assert any("`ownership_labels`" in message for _, message in problems)

    def test_capacity_marker_change_rejects_a_stale_support_table(self) -> None:
        owners = ("SUPPORT.md", "scripts/doctor.sh")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, owners)
            support = root / "SUPPORT.md"
            text = support.read_text(encoding="utf-8")
            support.write_text(
                text.replace("total-ram-gib=14", "total-ram-gib=99", 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_capacity_course_contracts({}, root=root)
        assert any("99 GiB total RAM" in message for _, message in problems)

    def test_provider_examples_cannot_target_the_maintainer_gcp_project(self) -> None:
        docs = ("content/1. Setup/1.4. Providers.md",)
        owners = (".env.example",)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            env = root / owners[0]
            text = env.read_text(encoding="utf-8")
            env.write_text(text.replace("your-gcp-project-id", "agentops-open-course", 1), encoding="utf-8")
            problems = check_conventions.check_project_neutral_provider_contracts(
                contract_pages(root, docs),
                root=root,
            )
        assert any("maintainer-owned project" in message for _, message in problems)

    def test_release_workflow_cannot_drop_the_freshness_validator(self) -> None:
        docs = ("content/8. Community/8.2. Releases.md",)
        owners = (".github/workflows/release.yml",)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            workflow = root / owners[0]
            text = workflow.read_text(encoding="utf-8")
            workflow.write_text(
                text.replace("scripts/release_freshness.py", "scripts/removed_validator.py", 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_release_freshness_course_contracts(
                contract_pages(root, docs),
                root=root,
            )
        assert any("release_freshness.py" in message for _, message in problems)

    def test_release_workflow_cannot_replace_rendered_freshness_evidence_with_raw_markdown(self) -> None:
        docs = ("content/8. Community/8.2. Releases.md",)
        owners = (".github/workflows/release.yml",)
        for required, replacement in (
            ("application/vnd.github.full+json", "application/vnd.github+json"),
            ("gh api --method POST markdown --input -", "printf unsafe-template"),
        ):
            with self.subTest(required=required), tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                copy_contract_files(root, (*docs, *owners))
                workflow = root / owners[0]
                text = workflow.read_text(encoding="utf-8")
                workflow.write_text(text.replace(required, replacement, 1), encoding="utf-8")
                problems = check_conventions.check_release_freshness_course_contracts(
                    contract_pages(root, docs),
                    root=root,
                )
            assert any(required in message for _, message in problems)

    def test_release_page_cannot_hide_the_completed_checklist_requirement(self) -> None:
        docs = ("content/8. Community/8.2. Releases.md",)
        owners = (".github/workflows/release.yml",)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            page = root / docs[0]
            text = page.read_text(encoding="utf-8")
            page.write_text(text.replace("every checklist box checked", "a checklist", 1), encoding="utf-8")
            problems = check_conventions.check_release_freshness_course_contracts(
                contract_pages(root, docs),
                root=root,
            )
        assert any("every checklist box checked" in message for _, message in problems)

    def test_actions_artifacts_cannot_exceed_the_repository_retention_policy(self) -> None:
        docs = ("content/8. Community/8.2. Releases.md",)
        owners = (
            "AGENTS.md",
            ".github/workflows/eval.yml",
            ".github/workflows/freshness.yml",
            ".github/workflows/platform.yml",
            ".github/workflows/release.yml",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (*docs, *owners))
            workflow = root / ".github/workflows/freshness.yml"
            text = workflow.read_text(encoding="utf-8")
            workflow.write_text(text.replace("retention-days: 7", "retention-days: 8", 1), encoding="utf-8")
            problems = check_conventions.check_actions_artifact_retention_contracts(
                contract_pages(root, docs),
                root=root,
            )
        assert any("exceeds the 7-day policy" in message for _, message in problems)

    def test_release_preflight_handoff_survives_protected_environment_review(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/release.yml").read_text(encoding="utf-8")
        upload = workflow.split("      - name: Upload the exact preflight artifact\n", 1)[1].split("\n  publish:\n", 1)[
            0
        ]
        assert "retention-days: 7" in upload

    def test_release_reconcile_recovers_without_the_promotion_artifact(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/release.yml").read_text(encoding="utf-8")
        promote = workflow.split("\n  promote:\n", 1)[1].split("\n  seal:\n", 1)[0]
        promote_program = check_conventions.ROOT.joinpath("scripts/release-promote.sh").read_text(encoding="utf-8")
        verify = workflow.split("\n  verify:\n", 1)[1].split("\n  reconcile:\n", 1)[0]
        reconcile = workflow.split("\n  reconcile:\n", 1)[1].split("\n  release:\n", 1)[0]
        assert "--method DELETE" not in promote
        assert "run: ./scripts/release-promote.sh" in promote
        assert "--validate-index-only" in promote_program
        assert "--source-image" in promote_program
        assert "--validate-index-only" in verify
        assert "--source-image" in verify
        assert "needs: [publish, promote, seal, verify, release]" in reconcile
        assert "needs.publish.result == 'success'" in reconcile
        assert "needs.promote.result != 'success'" in reconcile
        assert 'pattern: "*-source-supply-chain"' in reconcile
        assert "scripts/release_reconcile.py" in reconcile
        assert "--source-image" in reconcile
        assert "--registry-absent" in reconcile
        assert "name: release-promotion" not in reconcile

    def test_release_uses_the_single_pinned_buildx_action(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/release.yml").read_text(encoding="utf-8")
        action = check_conventions.ROOT.joinpath(".github/actions/setup-buildx/action.yml").read_text(encoding="utf-8")
        assert workflow.count("name: Set up Docker Buildx") == 5
        assert workflow.count("uses: ./.github/actions/setup-buildx") == 5
        assert action.count("version: v0.36.0") == 1

    def test_release_reconcile_requires_the_exact_buildx_absence_error(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/release.yml").read_text(encoding="utf-8")
        reconcile = workflow.split("\n  reconcile:\n", 1)[1].split("\n  release:\n", 1)[0]
        assert 'expected_registry_absence="ERROR: ${image}:${VERSION}: not found"' in reconcile
        assert 'grep -Fqx -- "$expected_registry_absence" "$inspect_error"' in reconcile
        assert "grep -Eiq" not in reconcile

    def test_release_summary_and_eval_attempt_follow_the_authoritative_events(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/release.yml").read_text(encoding="utf-8")
        assert '--run-attempt "$eval_run_attempt"' in workflow
        release = workflow.split("\n  release:\n", 1)[1]
        assert release.index("--draft=false") < release.index('echo "## Published ${VERSION}"')

    def test_platform_network_probes_pin_the_curl_image_numeric_identity(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/platform.yml").read_text(encoding="utf-8")
        assert workflow.count("runAsUser: 100\n") == 3
        assert workflow.count("runAsGroup: 101\n") == 3
        assert workflow.count("CURL_IMAGE: curlimages/curl:8.21.0@sha256:") == 1
        assert workflow.count("image: ${CURL_IMAGE}") == 3
        assert workflow.count("kubernetes.io/metadata.name: kube-system") == 1
        assert workflow.count("{port: 53, protocol: UDP}") == 1
        assert workflow.count("{port: 53, protocol: TCP}") == 1

    def test_platform_privacy_canaries_survive_pii_redaction(self) -> None:
        workflow = check_conventions.ROOT.joinpath(".github/workflows/platform.yml").read_text(encoding="utf-8")
        drill = check_conventions.ROOT.joinpath("infra/scripts/platform-backup-drill.sh").read_text(encoding="utf-8")
        assert "BACKUP_EVIDENCE_MARKER: platform-backup-canary" in workflow
        assert "TELEMETRY_LOG_MARKER: platform-telemetry-log-canary" in workflow
        assert "TELEMETRY_SENTINEL: platform-private-content-canary" in workflow
        assert "def safe_container_state:" in workflow
        assert ".terminated | {exitCode, reason, signal, startedAt, finishedAt}" in workflow
        assert "run: ./infra/scripts/platform-backup-drill.sh" in workflow
        assert drill.count("wait_for_job agentops platform-state-") == 2
        assert "--for=condition=complete job/platform-state-" not in drill

    def test_gcp_runbook_rejects_a_root_scoped_tofu_command_without_chdir(self) -> None:
        relative = "infra/gcp/README.md"
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (relative,))
            readme = root / relative
            text = readme.read_text(encoding="utf-8")
            needle = "tofu -chdir=infra/gcp output -raw get_credentials_command"
            assert needle in text
            readme.write_text(text.replace(needle, "tofu output -raw get_credentials_command", 1), encoding="utf-8")
            problems = check_conventions.check_gcp_runbook(root=root)
        assert any("needs `-chdir=infra/gcp`" in message for _, message in problems)

    def test_skaffold_runbooks_reject_a_root_scoped_config_path(self) -> None:
        relative = "infra/gcp/README.md"
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (relative,))
            readme = root / relative
            text = readme.read_text(encoding="utf-8")
            needle = "--filename skaffold.yaml"
            assert needle in text
            readme.write_text(
                text.replace(needle, "--filename infra/skaffold.yaml", 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_skaffold_runbooks(root=root)
        assert any("run from `infra/`" in message for _, message in problems)

    def test_skaffold_runbooks_reject_working_directory_dependent_shorthand(self) -> None:
        relative = "infra/README.md"
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, (relative,))
            readme = root / relative
            text = readme.read_text(encoding="utf-8")
            needle = "skaffold delete --filename skaffold.yaml --profile local"
            assert needle in text
            readme.write_text(
                text.replace(needle, "skaffold delete -p local", 1),
                encoding="utf-8",
            )
            problems = check_conventions.check_skaffold_runbooks(root=root)
        assert any("run from `infra/`" in message for _, message in problems)

    def test_eval_runtime_cannot_drift_from_the_reviewed_cost_baseline(self) -> None:
        files = (".github/workflows/eval.yml", "agents/python/evals/cost_baseline.json")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, files)
            baseline = root / files[1]
            document = json.loads(baseline.read_text(encoding="utf-8"))
            document["ollama_version"] = "ollama version is 0.0.0"
            baseline.write_text(json.dumps(document), encoding="utf-8")
            problems = check_conventions.check_eval_runtime_baseline(root=root)
        assert any("scheduled Eval pins" in message for _, message in problems)

    def test_ci_install_profile_cannot_drift_from_linting_page(self) -> None:
        files = (
            ".github/workflows/ci.yml",
            "README.md",
            "content/_index.md",
            "content/1. Setup/1.0. System.md",
            "content/4. Quality/4.1. Linting.md",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, files)
            linting = root / "content/4. Quality/4.1. Linting.md"
            text = linting.read_text(encoding="utf-8")
            assert "install:validation" in text
            linting.write_text(text.replace("install:validation", "install:maintainer"), encoding="utf-8")
            problems = check_conventions.check_quickstarts(
                contract_pages(root, files[2:]),
                root=root,
            )
        assert any("CI prose must name" in message for _, message in problems)

    def test_outcome_matrix_cannot_exceed_the_linked_exercise(self) -> None:
        docs = (
            "content/0. Overview/0.0. Course.md",
            "content/4. Quality/4.4. Evaluations.md",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            copy_contract_files(root, docs)
            overview = root / docs[0]
            text = overview.read_text(encoding="utf-8")
            overview.write_text(
                text.replace(
                    "add one adversarial case and its deterministic validator",
                    "add a failing case, diagnose it, and repair the behavior",
                    1,
                ),
                encoding="utf-8",
            )
            problems = check_conventions.check_outcome_evidence_contracts(contract_pages(root, docs), root=root)
        assert {message for _, message in problems} == {
            "evaluation outcome promises 'diagnose' but the linked exercise does not require it",
            "evaluation outcome promises 'repair' but the linked exercise does not require it",
        }

    def test_course_evidence_rejects_untracked_source(self) -> None:
        with mock.patch.object(
            course_evidence,
            "git",
            return_value="?? agents/python/src/agent/local_only.py",
        ):
            caught = ""
            try:
                course_evidence.require_clean_revision()
            except ValueError as error:
                caught = str(error)
        assert "tracked or untracked source is dirty" in caught


class ExerciseContractTests(unittest.TestCase):
    def test_temporary_experiment_requires_dirty_preflight_and_cleanup(self) -> None:
        text = """## Your turn: what changes?

- **Mode**: `temporary experiment`
- **Goal**: Change one thing.
- **Files to touch**: `example.py`.
- **Preflight**: Start from a clean checkout.
- **Gate that proves completion**: The test is red.
- **Final state**: Restore the file.
"""
        problems = check_conventions.check_exercises(pathlib.Path("content/example.md"), text)
        assert len(problems) == 2

    def test_probabilistic_result_cannot_be_a_mandatory_red_state(self) -> None:
        text = """## Your turn: what changes?

- **Mode**: `inspect`
- **Goal**: Observe.
- **Files to touch**: None.
- **Preflight**: None.
- **Gate that proves completion**: It fails without the rule, but may pass.
- **Final state**: Clean.
"""
        problems = check_conventions.check_exercises(pathlib.Path("content/example.md"), text)
        assert any("probabilistic evidence" in message for _, message in problems)


class DiagramContractTests(unittest.TestCase):
    def test_changed_legacy_diagram_needs_adjacent_words(self) -> None:
        original = ["flowchart LR", "A --> B"]
        changed = """```mermaid
flowchart LR
A --> C
```
"""
        problems = check_conventions.check_diagram_alternatives(
            pathlib.Path("content/example.md"),
            changed,
            {check_conventions.sha256_lines(original)},
        )
        assert len(problems) == 1


class RenderedContractTests(unittest.TestCase):
    def test_local_links_must_remain_inside_the_published_site(self) -> None:
        document = """<!doctype html><html lang="en"><body><main><h1>Page</h1>
<a href="../../docs/assets/font.txt">Font license</a>
<a href="../assets/missing.txt">Missing asset</a>
</main></body></html>"""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            site = root / "site"
            page = site / "chapter/page.html"
            source_asset = root / "content/assets/font.txt"
            page.parent.mkdir(parents=True)
            source_asset.parent.mkdir(parents=True)
            source_asset.write_text("source-tree file", encoding="utf-8")
            page.write_text(document, encoding="utf-8")
            problems = check_conventions.check_rendered(site)
        messages = "\n".join(message for _, message in problems)
        assert "rendered link escapes the published site" in messages
        assert "rendered link target is missing from the published site" in messages

    def test_homepage_missing_metadata_and_404_recovery_fails(self) -> None:
        document = """<!doctype html><html lang="en"><head><meta name="description" content="x"></head>
<body><main><h1>Page</h1></main></body></html>"""
        with tempfile.TemporaryDirectory() as directory:
            site = pathlib.Path(directory)
            (site / "index.html").write_text(document, encoding="utf-8")
            (site / "404.html").write_text(document, encoding="utf-8")
            problems = check_conventions.check_rendered(site)
        messages = "\n".join(message for _, message in problems)
        assert "og:title" in messages
        assert "source-edit action" in messages
        assert "route back home" in messages


class UrlContractTests(unittest.TestCase):
    """The clean-URL contract that replaced docs/released-urls.json.

    Material published `0. Overview/0.0. Course.html`; Hugo publishes directory URLs derived
    from file names that carry spaces and dots. Every page states its own URL, so these are
    the tests that keep those strings and the file tree from drifting apart.
    """

    def test_page_url_slugs_spaces_and_dots(self) -> None:
        root = check_conventions.ROOT / "content"
        assert check_conventions.page_url(root / "0. Overview/0.7. Glossary.md") == "/0-overview/0-7-glossary/"
        assert check_conventions.page_url(root / "0. Overview/_index.md") == "/0-overview/"
        assert check_conventions.page_url(root / "_index.md") == "/"
        assert check_conventions.page_url(root / "6. Platform/6.7. Promotion and Rollback.md") == (
            "/6-platform/6-7-promotion-and-rollback/"
        )

    def test_every_page_declares_the_url_its_file_name_implies(self) -> None:
        for page in sorted(check_conventions.ROOT.joinpath("content").rglob("*.md")):
            relative = page.relative_to(check_conventions.ROOT)
            text = page.read_text(encoding="utf-8")
            assert check_conventions.check_page_urls(relative, text) == [], relative.as_posix()

    def test_a_wrong_url_is_rejected(self) -> None:
        page = pathlib.Path("content/0. Overview/0.7. Glossary.md")
        text = '---\ntitle: "0.7. Glossary"\ndescription: x\nurl: "/wrong/"\n---\n\n## Why?\n'
        problems = check_conventions.check_page_urls(page, text)
        assert any("front matter url must be" in message for _, message in problems)

    def test_a_markdown_h1_is_rejected(self) -> None:
        page = pathlib.Path("content/0. Overview/0.7. Glossary.md")
        text = (
            '---\ntitle: "0.7. Glossary"\ndescription: x\nurl: "/0-overview/0-7-glossary/"\n---\n'
            "\n# 0.7. Glossary\n\n## Why?\n"
        )
        problems = check_conventions.check_page_urls(page, text)
        assert any("would publish a second <h1>" in message for _, message in problems)


class NavigationContractTests(unittest.TestCase):
    """data/nav.yaml is what replaced MkDocs `strict: true` plus an explicit `nav:`."""

    def test_every_page_is_reachable_from_the_learning_path(self) -> None:
        pages = {
            page: page.read_text(encoding="utf-8") for page in check_conventions.ROOT.joinpath("content").rglob("*.md")
        }
        assert check_conventions.check_navigation(pages) == []

    def test_a_page_missing_from_the_navigation_is_rejected(self) -> None:
        pages = {
            page: page.read_text(encoding="utf-8") for page in check_conventions.ROOT.joinpath("content").rglob("*.md")
        }
        pages[check_conventions.ROOT / "content/9. Ghost/9.0. Ghost.md"] = "---\ndescription: x\n---\n"
        problems = check_conventions.check_navigation(pages)
        assert any("navigation omits pages" in message for _, message in problems)


class IncludeContractTests(unittest.TestCase):
    """The include shortcode replaced pymdownx.snippets; the fence rule inverted with it."""

    def test_an_include_inside_a_fence_is_rejected(self) -> None:
        text = '```python\n{{< include path="a.py" region="b" >}}\n```\n'
        problems = check_conventions.check_snippets(pathlib.Path("content/example.md"), text)
        assert any("must not sit inside a fenced code block" in message for _, message in problems)

    def test_retired_material_snippet_syntax_is_rejected(self) -> None:
        text = '--8<-- "agents/python/src/agent/model.py:build-model"\n'
        problems = check_conventions.check_snippets(pathlib.Path("content/example.md"), text)
        assert any("Material snippet syntax is retired" in message for _, message in problems)


if __name__ == "__main__":
    unittest.main()
