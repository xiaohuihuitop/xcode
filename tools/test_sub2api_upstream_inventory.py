import csv
import hashlib
import io
import json
import tempfile
import unittest
import zipfile
from pathlib import Path

from tools.sub2api_upstream_inventory import (
    classify_path,
    generate_inventory,
    compare_trees,
    migration_number,
    resolve_tag_commit,
    snapshot_upstream,
    validate_matrix,
    verify_target_commit,
    write_csv,
)


class InventoryTests(unittest.TestCase):
    def test_classify_path_separates_runtime_product_and_database(self):
        self.assertEqual(classify_path("backend/internal/pkg/apicompat/a.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/service/openai_gateway.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/subscription.go"), "productcore")
        self.assertEqual(classify_path("backend/migrations/228_channel_pricing_multipliers.sql"), "database")
        self.assertEqual(classify_path("frontend/src/views/admin/GroupsView.vue"), "frontend_product")

    def test_migration_number_handles_numeric_prefix_and_non_migration(self):
        self.assertEqual(migration_number("backend/migrations/228_channel.sql"), 228)
        self.assertEqual(migration_number("backend/migrations/225a_index.sql"), 225)
        self.assertIsNone(migration_number("backend/internal/service/openai.go"))

    def test_resolve_tag_commit_accepts_lightweight_and_annotated_tags(self):
        self.assertEqual(resolve_tag_commit({"object": {"type": "commit", "sha": "abc"}}), "abc")
        self.assertEqual(
            resolve_tag_commit({"object": {"type": "tag", "sha": "tagsha"}}, {"object": {"sha": "def"}}),
            "def",
        )

    def test_compare_trees_reports_official_only_current_only_and_different(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            official = root / "official"
            current = root / "current"
            official.mkdir()
            current.mkdir()
            (official / "same.go").write_text("same", encoding="utf-8")
            (current / "same.go").write_text("same", encoding="utf-8")
            (official / "changed.go").write_text("official", encoding="utf-8")
            (current / "changed.go").write_text("current", encoding="utf-8")
            (official / "new.go").write_text("new", encoding="utf-8")
            (current / "local.go").write_text("local", encoding="utf-8")
            states = {row["path"]: row["state"] for row in compare_trees(official, current)}
            self.assertEqual(states["same.go"], "same")
            self.assertEqual(states["changed.go"], "different")
            self.assertEqual(states["new.go"], "official_only")
            self.assertEqual(states["local.go"], "current_only")

    def test_validate_matrix_rejects_empty_disposition_and_uncovered_commit(self):
        commits = [
            {"sha": "a", "category": "runtime_protocol"},
            {"sha": "b", "category": "runtime_provider"},
        ]
        features = [{"id": "F001", "commits": "a", "disposition": "direct_sync", "phase": "2"}]
        with self.assertRaisesRegex(ValueError, "uncovered commits: b"):
            validate_matrix(commits, features)

    def test_snapshot_rejects_wrong_target_commit(self):
        with self.assertRaisesRegex(ValueError, "target commit mismatch"):
            verify_target_commit("wrong", "75f88be5f75c27771836b586f7de1503afa0e3bc")

    def test_write_csv_uses_stable_column_order(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "rows.csv"
            write_csv(output, [{"sha": "a", "category": "runtime_protocol"}], ["sha", "category"])
            self.assertEqual(output.read_text(encoding="utf-8").splitlines()[0], "sha,category")

    def test_snapshot_paginates_compare_and_records_verified_archive(self):
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as bundle:
            bundle.writestr("sub2api-0.1.179/backend/cmd/server/VERSION", "0.1.178\n")
            bundle.writestr("sub2api-0.1.179/backend/internal/pkg/apicompat/a.go", "package apicompat\n")

        calls = []

        def fetch_json(path):
            calls.append(path)
            if path == "/repos/Wei-Shaw/sub2api/releases/tags/v0.1.179":
                return {"published_at": "2026-08-20T00:00:00Z"}
            if path == "/repos/Wei-Shaw/sub2api/git/ref/tags/v0.1.179":
                return {"object": {"type": "commit", "sha": "target-sha"}}
            if path.endswith("page=1"):
                return {
                    "total_commits": 3,
                    "commits": [
                        {"sha": "a", "commit": {"author": {"date": "2026-08-01T00:00:00Z"}, "message": "fix(responses): one"}},
                        {"sha": "b", "commit": {"author": {"date": "2026-08-02T00:00:00Z"}, "message": "feat(glm): two"}},
                    ],
                }
            if path.endswith("page=2"):
                return {
                    "total_commits": 3,
                    "commits": [
                        {"sha": "c", "commit": {"author": {"date": "2026-08-03T00:00:00Z"}, "message": "docs: three"}},
                    ],
                }
            raise AssertionError(f"unexpected API path: {path}")

        with tempfile.TemporaryDirectory() as temp:
            cache = Path(temp)
            manifest = snapshot_upstream(
                repo="Wei-Shaw/sub2api",
                base="v0.1.169",
                target="v0.1.179",
                expected_commit="target-sha",
                cache_dir=cache,
                fetch_json=fetch_json,
                download_bytes=lambda _url: archive.getvalue(),
                generated_at="2026-08-23T00:00:00Z",
            )

            self.assertEqual(manifest["commit_count"], 3)
            self.assertEqual(manifest["target_commit"], "target-sha")
            self.assertEqual(manifest["official_version_file"], "0.1.178")
            self.assertEqual(manifest["archive_sha256"], hashlib.sha256(archive.getvalue()).hexdigest())
            self.assertEqual(len([path for path in calls if "/compare/" in path]), 2)
            self.assertTrue((cache / "raw" / "compare-page-001.json").is_file())
            self.assertTrue((cache / "raw" / "compare-page-002.json").is_file())
            self.assertEqual(
                (cache / manifest["source_root"] / "backend" / "cmd" / "server" / "VERSION").read_text(encoding="utf-8").strip(),
                "0.1.178",
            )

    def test_generate_writes_deterministic_metadata_commits_and_files(self):
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as bundle:
            bundle.writestr("sub2api-0.1.179/backend/cmd/server/VERSION", "0.1.178\n")
            bundle.writestr("sub2api-0.1.179/backend/internal/pkg/apicompat/a.go", "official\n")
            bundle.writestr("sub2api-0.1.179/frontend/dist/generated.js", "ignored\n")

        def fetch_json(path):
            if "/releases/tags/" in path:
                return {"published_at": "2026-08-20T00:00:00Z"}
            if "/git/ref/tags/" in path:
                return {"object": {"type": "commit", "sha": "target-sha"}}
            return {
                "total_commits": 2,
                "commits": [
                    {"sha": "a", "commit": {"author": {"date": "2026-08-01T00:00:00Z"}, "message": "fix(responses): protocol"}},
                    {"sha": "b", "commit": {"author": {"date": "2026-08-02T00:00:00Z"}, "message": "feat(glm): provider"}},
                ],
            }

        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            cache = root / "cache"
            current = root / "current"
            output = current / "docs" / "upstream" / "fixture"
            (current / "backend" / "internal" / "pkg" / "apicompat").mkdir(parents=True)
            (current / "backend" / "internal" / "pkg" / "apicompat" / "a.go").write_text("current\n", encoding="utf-8")
            (current / ".git").write_text("gitdir: elsewhere", encoding="utf-8")
            (current / "tools" / "__pycache__").mkdir(parents=True)
            (current / "tools" / "__pycache__" / "cache.pyc").write_bytes(b"ignored")

            snapshot_upstream(
                repo="Wei-Shaw/sub2api",
                base="v0.1.169",
                target="v0.1.179",
                expected_commit="target-sha",
                cache_dir=cache,
                fetch_json=fetch_json,
                download_bytes=lambda _url: archive.getvalue(),
                generated_at="2026-08-23T00:00:00Z",
            )
            (cache / "raw" / "compare-page-999.json").write_text(
                json.dumps(
                    {
                        "total_commits": 1,
                        "commits": [
                            {"sha": "stale", "commit": {"author": {"date": "2020-01-01T00:00:00Z"}, "message": "stale"}}
                        ],
                    }
                ),
                encoding="utf-8",
            )
            generate_inventory(cache, current, output)
            first = {path.name: path.read_bytes() for path in output.iterdir()}
            generate_inventory(cache, current, output)
            second = {path.name: path.read_bytes() for path in output.iterdir()}

            self.assertEqual(first, second)
            metadata = json.loads((output / "metadata.json").read_text(encoding="utf-8"))
            self.assertEqual(metadata["target_commit"], "target-sha")
            self.assertEqual(metadata["commit_count"], 2)
            with (output / "commits.csv").open(encoding="utf-8", newline="") as handle:
                commits = list(csv.DictReader(handle))
            self.assertEqual([row["category"] for row in commits], ["runtime_protocol", "runtime_provider"])
            with (output / "files.csv").open(encoding="utf-8", newline="") as handle:
                paths = {row["path"] for row in csv.DictReader(handle)}
            self.assertNotIn(".git", paths)
            self.assertNotIn("tools/__pycache__/cache.pyc", paths)
            self.assertNotIn("frontend/dist/generated.js", paths)
            self.assertFalse(any(path.startswith("docs/upstream/fixture/") for path in paths))


if __name__ == "__main__":
    unittest.main()
