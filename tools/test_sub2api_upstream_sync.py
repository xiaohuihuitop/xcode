import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools.sub2api_upstream_inventory import classify_path, translate_migration_number
from tools.sub2api_upstream_sync import (
    apply_sync_plan,
    build_file_plan,
    build_sync_plan,
    build_sync_plan_document,
    target_path_for_path,
    validate_sync_plan_document,
)


class SyncPlanTests(unittest.TestCase):
    def test_plan_uses_immutable_tag_and_sha(self):
        plan = build_sync_plan(
            "v0.1.179",
            "v0.1.180",
            "0123456789abcdef0123456789abcdef01234567",
        )
        self.assertEqual(
            plan.target,
            "v0.1.180@0123456789abcdef0123456789abcdef01234567",
        )

    def test_plan_rejects_moving_branch(self):
        with self.assertRaisesRegex(ValueError, "immutable tag"):
            build_sync_plan(
                "v0.1.179",
                "main",
                "0123456789abcdef0123456789abcdef01234567",
            )

    def test_productcore_paths_are_never_direct_sync(self):
        self.assertEqual(
            classify_path("backend/internal/service/subscription.go"),
            "productcore",
        )

    def test_runtime_migration_mapping_stays_in_reserved_range(self):
        self.assertEqual(translate_migration_number(217), 8017)

    def test_cli_runs_from_repository_root(self):
        root = Path(__file__).resolve().parents[1]
        result = subprocess.run(
            [sys.executable, "-B", "tools/sub2api_upstream_sync.py", "--help"],
            cwd=root,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Build and validate", result.stdout)

    def test_file_plan_keeps_adapter_and_productcore_out_of_direct_sync(self):
        rows = [
            {
                "path": "backend/internal/runtime/sub2api/upstream/openai.go",
                "state": "official_only",
                "category": "runtime_provider",
                "official_sha256": "a" * 64,
                "current_sha256": "",
                "migration_number": "",
            },
            {
                "path": "backend/internal/runtime/sub2api/openai_executor.go",
                "state": "different",
                "category": "runtime_provider",
                "official_sha256": "b" * 64,
                "current_sha256": "c" * 64,
                "migration_number": "",
            },
            {
                "path": "backend/internal/service/subscription.go",
                "state": "different",
                "category": "productcore",
                "official_sha256": "d" * 64,
                "current_sha256": "e" * 64,
                "migration_number": "",
            },
            {
                "path": "backend/migrations/217_provider_quota.sql",
                "state": "official_only",
                "category": "database",
                "official_sha256": "f" * 64,
                "current_sha256": "",
                "migration_number": "217",
            },
        ]
        plan = build_file_plan(rows)
        dispositions = {row["path"]: row["disposition"] for row in plan}
        self.assertEqual(
            dispositions["backend/internal/runtime/sub2api/upstream/openai.go"],
            "direct_sync",
        )
        self.assertEqual(
            dispositions["backend/internal/runtime/sub2api/openai_executor.go"],
            "adapter_port",
        )
        self.assertEqual(
            dispositions["backend/internal/service/subscription.go"],
            "productcore_mapping",
        )
        self.assertEqual(
            dispositions["backend/migrations/217_provider_quota.sql"],
            "not_runtime",
        )

    def test_direct_sync_path_gets_explicit_official_runtime_target(self):
        self.assertEqual(
            target_path_for_path("backend/internal/pkg/apicompat/responses.go"),
            "backend/internal/runtime/sub2api/upstream/apicompat/responses.go",
        )

    def test_generated_direct_sync_candidates_require_manual_approval(self):
        plan = build_file_plan(
            [
                {
                    "path": "backend/internal/pkg/apicompat/responses.go",
                    "state": "different",
                    "category": "runtime_protocol",
                    "official_sha256": "a" * 64,
                    "current_sha256": "b" * 64,
                    "migration_number": "",
                }
            ]
        )
        self.assertEqual(plan[0]["source_path"], "backend/internal/pkg/apicompat/responses.go")
        self.assertEqual(
            plan[0]["target_path"],
            "backend/internal/runtime/sub2api/upstream/apicompat/responses.go",
        )
        self.assertFalse(plan[0]["approved"])

    def test_validate_sync_plan_requires_complete_approval_metadata(self):
        with self.assertRaisesRegex(ValueError, "source_root"):
            validate_sync_plan_document(
                {
                    "base_tag": "v0.1.1",
                    "target_tag": "v0.1.2",
                    "target_commit": "a" * 40,
                    "files": [],
                }
            )

    def test_validate_sync_plan_rejects_paths_missing_from_inventory(self):
        plan = {
            "base_tag": "v0.1.1",
            "target_tag": "v0.1.2",
            "target_commit": "a" * 40,
            "source_root": "C:/snapshot",
            "files": [
                {
                    "path": "backend/internal/runtime/sub2api/upstream/openai.go",
                    "source_path": "backend/internal/runtime/sub2api/upstream/openai.go",
                    "target_path": "backend/internal/runtime/sub2api/upstream/openai.go",
                    "state": "official_only",
                    "category": "runtime_provider",
                    "official_sha256": "a" * 64,
                    "current_sha256": "",
                    "migration_number": "",
                    "disposition": "direct_sync",
                    "action": "candidate",
                    "approved": False,
                }
            ],
        }
        with self.assertRaisesRegex(ValueError, "missing from inventory"):
            validate_sync_plan_document(
                plan,
                [
                    {
                        "path": "backend/internal/runtime/sub2api/upstream/other.go",
                        "state": "official_only",
                        "category": "runtime_provider",
                        "official_sha256": "b" * 64,
                        "current_sha256": "",
                        "migration_number": "",
                    }
                ],
            )

    def test_validate_sync_plan_rejects_absolute_windows_paths(self):
        with self.assertRaisesRegex(ValueError, "safe repository-relative path"):
            validate_sync_plan_document(
                {
                    "base_tag": "v0.1.1",
                    "target_tag": "v0.1.2",
                    "target_commit": "a" * 40,
                    "source_root": "C:/snapshot",
                    "files": [
                        {
                            "path": "C:/outside.go",
                            "source_path": "C:/outside.go",
                            "target_path": "",
                            "state": "current_only",
                            "category": "runtime_provider",
                            "official_sha256": "",
                            "current_sha256": "a" * 64,
                            "migration_number": "",
                            "disposition": "adapter_port",
                            "action": "preserve",
                            "approved": False,
                        }
                    ],
                }
            )

    def test_apply_sync_plan_copies_only_approved_runtime_zone_files(self):
        with self.subTest("approved file"):
            from pathlib import Path
            from tempfile import TemporaryDirectory

            with TemporaryDirectory() as temp:
                root = Path(temp)
                source = root / "source"
                worktree = root / "worktree"
                source_relative = Path("backend/internal/pkg/apicompat/openai.go")
                target_relative = Path("backend/internal/runtime/sub2api/upstream/apicompat/openai.go")
                (source / source_relative.parent).mkdir(parents=True)
                (source / source_relative).write_text("package upstream\n", encoding="utf-8")
                plan = {
                    "base_tag": "v0.1.1",
                    "target_tag": "v0.1.2",
                    "target_commit": "a" * 40,
                    "source_root": str(source),
                    "files": [
                        {
                            "path": source_relative.as_posix(),
                            "source_path": source_relative.as_posix(),
                            "target_path": target_relative.as_posix(),
                            "action": "candidate",
                            "disposition": "direct_sync",
                            "state": "official_only",
                            "approved": True,
                        }
                    ],
                }
                apply_sync_plan(plan, worktree)
                self.assertEqual(
                    (worktree / target_relative).read_text(encoding="utf-8"),
                    "package upstream\n",
                )

    def test_apply_sync_plan_rejects_unapproved_direct_sync_candidates(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source_relative = Path("backend/internal/pkg/apicompat/openai.go")
            plan = {
                "base_tag": "v0.1.1",
                "target_tag": "v0.1.2",
                "target_commit": "a" * 40,
                "source_root": str(root),
                "files": [
                    {
                        "path": source_relative.as_posix(),
                        "source_path": source_relative.as_posix(),
                        "target_path": "backend/internal/runtime/sub2api/upstream/apicompat/openai.go",
                        "action": "candidate",
                        "disposition": "direct_sync",
                        "state": "different",
                        "approved": False,
                    }
                ],
            }
            with self.assertRaisesRegex(ValueError, "requires explicit approval"):
                apply_sync_plan(plan, root / "worktree")

    def test_apply_sync_plan_copies_approved_unchanged_runtime_baseline(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source_relative = Path("backend/internal/pkg/apicompat/openai.go")
            target_relative = Path("backend/internal/runtime/sub2api/upstream/apicompat/openai.go")
            (root / source_relative.parent).mkdir(parents=True)
            (root / source_relative).write_text("package upstream\n", encoding="utf-8")
            plan = {
                "base_tag": "v0.1.1",
                "target_tag": "v0.1.2",
                "target_commit": "a" * 40,
                "source_root": str(root),
                "files": [
                    {
                        "path": source_relative.as_posix(),
                        "source_path": source_relative.as_posix(),
                        "target_path": target_relative.as_posix(),
                        "action": "unchanged",
                        "disposition": "direct_sync",
                        "state": "same",
                        "approved": True,
                    }
                ],
            }
            apply_sync_plan(plan, root / "worktree")
            self.assertEqual(
                (root / "worktree" / target_relative).read_text(encoding="utf-8"),
                "package upstream\n",
            )

    def test_apply_sync_plan_skips_adapter_candidates(self):
        from pathlib import Path
        from tempfile import TemporaryDirectory

        with TemporaryDirectory() as temp:
            plan = {
                "base_tag": "v0.1.1",
                "target_tag": "v0.1.2",
                "target_commit": "a" * 40,
                "source_root": temp,
                "files": [
                    {
                        "path": "backend/internal/runtime/sub2api/openai_executor.go",
                        "action": "candidate",
                        "disposition": "adapter_port",
                        "state": "different",
                        "approved": False,
                    }
                ],
            }
            apply_sync_plan(plan, Path(temp) / "worktree")
            self.assertFalse((Path(temp) / "worktree").exists())

    def test_build_and_validate_sync_plan_from_local_snapshot(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            cache = root / "cache"
            source = cache / "source" / "official"
            current = root / "current"
            output = current / "docs" / "upstream" / "fixture"
            runtime_file = source / "backend" / "internal" / "runtime" / "sub2api" / "upstream" / "openai.go"
            runtime_file.parent.mkdir(parents=True)
            runtime_file.write_text("package upstream\n", encoding="utf-8")
            (current / "backend" / "internal" / "runtime" / "sub2api" / "upstream").mkdir(parents=True)
            (current / "backend" / "internal" / "runtime" / "sub2api" / "upstream" / "openai.go").write_text(
                "package current\n",
                encoding="utf-8",
            )
            (cache / "raw").mkdir(parents=True)
            (cache / "raw" / "compare-page-001.json").write_text(
                json.dumps(
                    {
                        "total_commits": 1,
                        "commits": [
                            {
                                "sha": "a" * 40,
                                "commit": {
                                    "author": {"date": "2026-08-24T00:00:00Z"},
                                    "message": "fix(runtime): sync official runtime",
                                },
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            (cache / "snapshot.json").write_text(
                json.dumps(
                    {
                        "repo": "Wei-Shaw/sub2api",
                        "base_tag": "v0.1.179",
                        "target_tag": "v0.1.180",
                        "target_commit": "b" * 40,
                        "release_published_at": "2026-08-24T00:00:00Z",
                        "archive_sha256": "c" * 64,
                        "official_version_file": "0.1.180",
                        "commit_count": 1,
                        "compare_page_count": 1,
                        "source_root": "source/official",
                        "generated_at": "2026-08-24T00:00:00Z",
                    }
                ),
                encoding="utf-8",
            )

            document = build_sync_plan_document(cache, current, output)
            self.assertEqual(document["target"], "v0.1.180@" + "b" * 40)
            self.assertEqual(document["files"][0]["disposition"], "direct_sync")
            self.assertIn("source_root", document)
            self.assertFalse(document["files"][0]["approved"])
            validate_sync_plan_document(document)


if __name__ == "__main__":
    unittest.main()
