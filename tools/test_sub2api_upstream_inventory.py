import csv
import hashlib
import io
import json
import tempfile
import unittest
import zipfile
from pathlib import Path

from tools.sub2api_upstream_inventory import (
    classify_commit,
    classify_path,
    database_rows_from_markdown,
    feature_rows_from_markdown,
    generate_inventory,
    compare_trees,
    migration_number,
    resolve_tag_commit,
    snapshot_upstream,
    validate_matrix,
    validate_database_impact,
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
        self.assertEqual(classify_path("backend/ent/account.go"), "database")
        self.assertEqual(classify_path("backend/internal/pkg/antigravity/client.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/pkg/websearch/provider.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/applicationgateway/gateway.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/gatewayruntime/exchange.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/runtimebridge/local.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/pkg/runtimebridge/v1/bridge.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/service/account_header_override.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/account.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/gateway_forward_as_responses.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/service/batch_image_provider_gemini.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/batch_image_processor.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/service/batch_image.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/service/scheduler_cache.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/billing_service.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/platform.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/user.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/proxy.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/sub2api_account_runtime.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/service/sub2api_pricing_adapter.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/sub2api_product_usage_finalizer.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/ops_service.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/service/wire.go"), "infrastructure")
        self.assertEqual(classify_path("backend/internal/handler/sub2api_runtime_adapter.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/handler/runtime_ingress.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/handler/admin/account_handler.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/handler/user_handler.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/handler/wire.go"), "infrastructure")
        self.assertEqual(classify_path("backend/internal/repository/openai_oauth_service.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/repository/account_repo.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/repository/batch_image_repo.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/repository/user_repo.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/repository/usage_log_repo.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/repository/db_pool.go"), "infrastructure")
        self.assertEqual(classify_path("backend/internal/server/routes/gateway.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/server/routes/user.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/server/middleware/recovery.go"), "infrastructure")
        self.assertEqual(classify_path("backend/internal/securityaudit/coordinator.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/domain/openai_messages_dispatch.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/domain/model_pricing.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/model/error_passthrough_rule.go"), "runtime_protocol")
        self.assertEqual(classify_path("backend/internal/model/tls_fingerprint_profile.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/platform/liveattestation/attestation.go"), "runtime_provider")
        self.assertEqual(classify_path("backend/internal/productcore/authorizer.go"), "productcore")
        self.assertEqual(classify_path("backend/internal/pkg/httpclient/pool.go"), "infrastructure")
        self.assertEqual(classify_path("backend/resources/model-pricing/default.json"), "productcore")
        self.assertEqual(classify_path("assets/partners/logos/example.svg"), "documentation")
        self.assertEqual(classify_path("openspec/config.yaml"), "documentation")
        self.assertEqual(classify_path("skills/sub2api-admin/SKILL.md"), "documentation")
        self.assertEqual(classify_path("backend/go.mod"), "infrastructure")
        self.assertEqual(classify_path("backend/cmd/server/main.go"), "infrastructure")
        self.assertEqual(classify_path("tools/check_pnpm_audit_exceptions.py"), "infrastructure")

    def test_classify_commit_separates_merges_runtime_and_maintenance(self):
        self.assertEqual(classify_commit("Merge pull request #5876 from wucm667/fix/issue-5872"), "merge")
        self.assertEqual(classify_commit("fix(images): decode data URLs during task offload"), "runtime_protocol")
        self.assertEqual(classify_commit("chore: update sponsors"), "documentation")
        self.assertEqual(classify_commit("feat: ops 错误详情弹窗支持自定义时间区间"), "productcore")
        self.assertEqual(classify_commit("fix(ui): adapt native form controls to dark mode via color-scheme"), "frontend_product")
        self.assertEqual(classify_commit("fix: stop scheduler work after request cancellation"), "runtime_provider")
        self.assertEqual(
            classify_commit("fix(gateway): 流内降载错误恢复 pre-output failover 并对客户端改写为可重试错误码"),
            "runtime_protocol",
        )
        self.assertEqual(classify_commit("完善大文件备份分卷上传与恢复"), "infrastructure")
        self.assertEqual(classify_commit("测试：同步长上下文计费断言"), "test")

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

    def test_feature_matrix_parser_feeds_uncovered_commit_validation(self):
        markdown = """
| ID | 官方功能 | 官方提交 | 归宿 | 阶段 |
| --- | --- | --- | --- | --- |
| F001 | protocol | a | direct_sync | 2 |
"""
        features = feature_rows_from_markdown(markdown)
        self.assertEqual(features[0]["id"], "F001")
        commits = [
            {"sha": "a", "category": "runtime_protocol"},
            {"sha": "b", "category": "runtime_provider"},
        ]
        with self.assertRaisesRegex(ValueError, "uncovered commits: b"):
            validate_matrix(commits, features)

    def test_database_impact_rejects_missing_migration_and_direct_sql(self):
        files = [
            {"path": "backend/migrations/217_one.sql", "migration_number": "217"},
            {"path": "backend/migrations/218_two.sql", "migration_number": "218"},
        ]
        missing_markdown = """
| 官方迁移 | 处理方式 |
| --- | --- |
| `217_one.sql` | productcore_mapping |
"""
        with self.assertRaisesRegex(ValueError, "missing migration mappings: 218_two.sql"):
            validate_database_impact(files, database_rows_from_markdown(missing_markdown))

        direct_markdown = """
| 官方迁移 | 处理方式 |
| --- | --- |
| `217_one.sql` | productcore_mapping |
| `218_two.sql` | direct_sql |
"""
        with self.assertRaisesRegex(ValueError, "direct_sql is forbidden: 218_two.sql"):
            validate_database_impact(files, database_rows_from_markdown(direct_markdown))

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
