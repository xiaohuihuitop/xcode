import tempfile
import unittest
from pathlib import Path

from tools.sub2api_upstream_inventory import (
    classify_path,
    compare_trees,
    migration_number,
    resolve_tag_commit,
    validate_matrix,
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


if __name__ == "__main__":
    unittest.main()
