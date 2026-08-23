#!/usr/bin/env python3
import argparse
import csv
import hashlib
import json
import os
import re
import sys
import urllib.parse
import urllib.request
import zipfile
from datetime import datetime, timezone
from io import BytesIO
from pathlib import Path


DISPOSITIONS = {
    "direct_sync",
    "adapter_port",
    "xcode_equivalent",
    "productcore_mapping",
    "not_runtime",
}

RUNTIME_PROTOCOL_PREFIXES = (
    "backend/internal/pkg/apicompat/",
    "backend/internal/pkg/openai/",
    "backend/internal/pkg/xai/",
    "backend/internal/applicationgateway/",
    "backend/internal/gatewayruntime/",
    "backend/internal/runtimebridge/",
    "backend/pkg/runtimebridge/",
    "backend/internal/handler/runtime_",
    "backend/internal/service/gateway_",
    "backend/internal/server/routes/gateway.go",
)
RUNTIME_PROVIDER_PREFIXES = (
    "backend/internal/runtime/",
    "backend/internal/handler/openai_",
    "backend/internal/handler/grok_",
    "backend/internal/handler/gemini_",
    "backend/internal/service/openai_",
    "backend/internal/service/grok_",
    "backend/internal/service/gemini_",
    "backend/internal/service/antigravity_",
    "backend/internal/handler/sub2api_",
    "backend/internal/pkg/antigravity/",
    "backend/internal/pkg/anthropicfp/",
    "backend/internal/pkg/claude/",
    "backend/internal/pkg/gemini/",
    "backend/internal/pkg/geminicli/",
    "backend/internal/pkg/googleapi/",
    "backend/internal/pkg/oauth/",
    "backend/internal/pkg/openai_compat/",
    "backend/internal/pkg/tlsfingerprint/",
    "backend/internal/pkg/websearch/",
    "backend/internal/repository/openai_",
    "backend/internal/service/account_",
)
PRODUCT_MARKERS = (
    "/subscription",
    "/api_key",
    "/payment",
    "/pricing",
    "/group",
    "/channel",
    "/redeem",
)
COMMIT_PROTOCOL_MARKERS = (
    "apicompat",
    "chat completion",
    "chatcompletion",
    "input token",
    "image",
    "message",
    "protocol",
    "reasoning",
    "response",
    "sse",
    "stream",
    "tool call",
    "websocket",
)
COMMIT_PROVIDER_MARKERS = (
    "account",
    "antigravity",
    "codex",
    "deepseek",
    "gemini",
    "glm",
    "grok",
    "kimi",
    "oauth",
    "ollama",
    "openai",
    "provider",
    "quota",
    "zhipu",
)
COMMIT_PRODUCT_MARKERS = (
    "api key",
    "billing",
    "channel",
    "group",
    "payment",
    "pricing",
    "redeem",
    "subscription",
    "user",
)
IGNORED_PARTS = {
    ".git",
    ".cache",
    ".mypy_cache",
    ".pytest_cache",
    "__pycache__",
    "coverage",
    "node_modules",
}
IGNORED_SUFFIXES = {
    ".a",
    ".dll",
    ".dylib",
    ".exe",
    ".gz",
    ".o",
    ".obj",
    ".pyc",
    ".so",
    ".tar",
    ".tgz",
    ".zip",
}
METADATA_FIELDS = (
    "repo",
    "base_tag",
    "target_tag",
    "target_commit",
    "release_published_at",
    "archive_sha256",
    "official_version_file",
    "commit_count",
    "generated_at",
)
COMMIT_FIELDS = ("sha", "date", "subject", "category", "files_known")
FILE_FIELDS = (
    "path",
    "state",
    "category",
    "official_sha256",
    "current_sha256",
    "migration_number",
)
REVIEWED_COMMIT_SUBJECTS = {
    "runtime_protocol": {
        "修复工具输出图片桥接",
        "fix(anthropic): recognize classifier requests with extra system entries",
        "fix(gateway): 流内降载错误恢复 pre-output failover 并对客户端改写为可重试错误码",
        "fix(claude): strip cache control from deferred tools",
        "fix(claude): support top-level deferred tools",
        "feat: 新增独立 /x_search，走原生 x_search 并沿用搜索计费",
        "fix(gateway): align passthrough model discovery",
    },
    "runtime_provider": {
        "feat(admin): 为账号批量删除增加并发限制",
        "feat(admin): 支持按筛选结果全选账号",
        "fix: stop scheduler work after request cancellation",
        "fix(ratelimit): 守卫按端点来源门控，并与冷却键对齐模型口径",
        "fix(admin): empty web-search config on missing setting and reset dialog scroll",
        "fix(settings): cache unset scheduling thresholds",
        "fix(review): 处理 PR #5666 四个阻断项（B1-B4）",
        "fix(fingerprint): align credential-face identity with the real client and de-drift models version",
        "fix(composite): keep CN rollout on fully supported paths",
        "feat(proxy): allow configurable probe targets",
        "fix(config): register proxy probe URLs default",
        "fix(proxy): validate configurable probe targets",
        "fix(proxy): format validated probe targets",
    },
    "productcore": {
        "feat: ops 错误详情弹窗支持自定义时间区间",
        "fix(dashboard): include cache tokens in token card breakdown",
        "fix(ops): show neutral SLA card when window has no requests",
        "feat(security-audit): add narrow blocking audit scope",
        "fix(email): unify SMTP connection path between send and test-connection",
        "feat(moderation): route content moderation through configurable proxy server",
        "fix(admin-ui): wire require_force into the refund dialog",
        "fix(auth): prevent refresh token rotation races",
        "修复模型广场图片模型价格展示与实收口径不一致",
        "新增腾讯天御验证码认证门禁",
        "人机验证增加阿里云验证码 2.0",
        "fix(ops): preserve custom range in error lists",
        "fix: 完善腾讯验证码区域适配与 CSP 白名单",
        "fix: 修复腾讯验证码票据过期与区域切换",
        "fix(ops): 系统日志落库失败后退避重试，避免拖垮数据库连接池",
        "fix(risk-control): block prompts when risk control backend fails",
        "修复邮箱域名注册额度策略",
        "Revert \"fix(risk-control): 风控后端异常时阻断提示词请求\"",
        "fix(audit): scope cyber policy events",
        "feat: 分组支持逐模型定价，并可关闭长上下文阶梯",
        "fix: Realtime 仅在观察到音频后计费，并修正标志位求值顺序",
        "feat: 优化分组用量统计",
        "fix(ops): avoid single-insert fallback after batch failure",
        "fix: skip expiry reminders without SMTP config",
        "功能：支持渠道模型分时倍率定价",
        "chore: remove leftover Sora references after platform removal",
        "perf(usage): aggregate stats in one scan",
        "渠道定价：持久化服务层级与区间倍率",
        "计费：应用渠道倍率与上下文区间价格",
        "计费：识别并记录 Anthropic Fast 请求",
        "fix(ops): exclude model configuration errors from SLA",
    },
    "frontend_product": {
        "fix(ui): adapt native form controls to dark mode via color-scheme",
        "优化模型广场UI:筛选栏换行对齐、模型排序与表格留白",
        "feat(home): add compact home page preset to avoid abuse classification",
        "fix(usage): restore request ID column visibility",
        "fix: 优化运营监控内存容量显示",
        "fix: 账号徽章与用量格按实时档位展示，避免账单滞后误判",
        "fix: 账号页自动刷新偏好改为模块初始化时恢复",
        "fix(admin): show category labels in ops error distribution legend",
        "feat(monitor-ui): 配额模式表单、用量快照视图与 8 平台支持",
        "feat(ui): Select 组件支持可选远程搜索（remote/loading props + search 事件）",
        "前端：配置渠道倍率并精简长上下文开关",
        "前端：修正账号长上下文开关门控",
        "fix(admin): 补全平台筛选选项",
    },
    "infrastructure": {
        "完善大文件备份分卷上传与恢复",
        "fix(lint): use require.NotNil for staticcheck SA5011",
        "fix(lint): resolve remaining nil dereference warnings",
        "fix(backup): 定时备份加 leader 锁，避免多实例重复备份",
        "fix(lint): 补齐 setting_public.go 与配额 fetcher 测试的 gofmt 对齐",
        "fix(repo): tolerate ErrTxStarted for tx-bound clients and harden test stubs",
    },
    "test": {
        "chore: remove unrelated test refactors",
        "修复：隔离分组用量仓储测试时区",
        "测试：同步长上下文计费断言",
    },
}
SERVICE_PATH_RULES = (
    (
        "runtime_provider",
        (
            "account.go",
            "anthropic_",
            "batch_image_provider",
            "bedrock_",
            "claude_",
            "cn_provider_",
            "codex_",
            "composite_",
            "credential",
            "digest_session_",
            "geminicli_",
            "oauth_",
            "ollama_",
            "proxy_",
            "proxy.go",
            "quota_",
            "ratelimit_",
            "refresh_",
            "scheduled_test_",
            "scheduler_",
            "session_",
            "shadow_",
            "sub2api_account_",
            "temp_unsched",
            "tls_",
            "token_",
            "upstream_",
            "vertex_",
        ),
    ),
    (
        "runtime_protocol",
        (
            "batch_image.go",
            "batch_image_",
            "concurrency_",
            "deferred_",
            "error_passthrough_",
            "header_",
            "http_upstream_",
            "image_generation_",
            "image_output_",
            "image_storage",
            "image_task",
            "model_mapping_",
            "model_not_found_",
            "model_rate_limit",
            "request_metadata",
            "response_header_",
            "rpm_cache",
            "sse_",
            "thinking_",
        ),
    ),
    (
        "productcore",
        (
            "admin_",
            "affiliate_",
            "aliyun_",
            "announcement",
            "audit_",
            "auth_",
            "backup_",
            "balance_",
            "billing_",
            "content_moderation",
            "crs_",
            "custom_",
            "dashboard_",
            "data_management_",
            "email_",
            "global_balance_",
            "idempotency",
            "identity_",
            "image_billing_",
            "invalid_auth_",
            "media_price_",
            "metadata_userid",
            "model_platform_",
            "model_pricing_",
            "notification_",
            "notify_",
            "ops_",
            "parse_integral_",
            "passkey",
            "platform_",
            "platform.go",
            "product_",
            "profit_",
            "promo_",
            "registration_",
            "setting",
            "sub2api_pricing_",
            "sub2api_product_",
            "system_operation_",
            "tencent_",
            "totp_",
            "turnstile_",
            "usage_",
            "user_",
            "user.go",
            "video_billing",
            "websearch_config",
        ),
    ),
    (
        "infrastructure",
        (
            "domain_constants",
            "internal500_",
            "leader_lock",
            "prompts/",
            "slice_helpers",
            "sql_errors",
            "timing_wheel_",
            "update_",
            "wire.go",
        ),
    ),
)
HANDLER_PATH_RULES = (
    (
        "runtime_provider",
        (
            "admin/account_",
            "admin/antigravity_",
            "admin/cn_provider_",
            "admin/gemini_",
            "admin/grok_",
            "admin/openai_",
            "admin/proxy_",
            "admin/scheduled_test_",
            "admin/tls_",
            "composite_platform",
            "quotaview/",
        ),
    ),
    (
        "runtime_protocol",
        (
            "batch_image_",
            "concurrency_",
            "endpoint.go",
            "failover_",
            "gateway_handler",
            "gateway_helper",
            "gateway_web_search",
            "image_",
            "no_account_",
            "request_body_",
            "stream_",
        ),
    ),
    (
        "productcore",
        (
            "admin/",
            "announcement_",
            "auth_",
            "available_",
            "content_moderation_",
            "dto/",
            "gateway_key_billing",
            "idempotency_",
            "model_plaza_",
            "model_target_",
            "ops_",
            "passkey_",
            "security_audit_",
            "setting_",
            "totp_",
            "usage_",
            "user_",
        ),
    ),
    ("infrastructure", ("handler.go", "logging.go", "page_handler.go", "wire.go")),
)
REPOSITORY_PATH_RULES = (
    (
        "runtime_provider",
        (
            "account_",
            "claude_",
            "concurrency_",
            "gemini_",
            "geminicli_",
            "grok_",
            "http_upstream",
            "openai_",
            "proxy_",
            "refresh_token_",
            "req_client_",
            "rpm_",
            "scheduled_test_",
            "scheduler_",
            "session_limit_",
            "temp_unsched_",
            "timeout_",
            "tls_",
        ),
    ),
    (
        "runtime_protocol",
        (
            "batch_image_",
            "error_passthrough_",
            "gateway_cache",
            "image_storage_",
            "image_task_",
        ),
    ),
    (
        "productcore",
        (
            "affiliate_",
            "aliyun_",
            "announcement_",
            "audit_",
            "auth_cache_",
            "billing_",
            "composite_model_",
            "content_moderation_",
            "custom_group_",
            "dashboard_",
            "email_",
            "idempotency_",
            "identity_",
            "internal500_",
            "leader_lock_",
            "model_pricing_",
            "ops_",
            "passkey_",
            "platform_",
            "promo_",
            "security_secret_",
            "setting_",
            "simple_mode_",
            "tencent_",
            "totp_",
            "turnstile_",
            "usage_",
            "user_",
        ),
    ),
    (
        "infrastructure",
        (
            "aes_",
            "backup_",
            "db_pool",
            "ent.go",
            "error_translate",
            "github_release_",
            "migrations_",
            "pagination",
            "postgres_",
            "redis.go",
            "s3_",
            "server_timing_",
            "sql_",
            "update_cache",
            "wire.go",
        ),
    ),
)


def classify_path(path: str) -> str:
    normalized = path.replace("\\", "/")
    if normalized.startswith("backend/migrations/") or normalized.startswith("backend/ent/"):
        return "database"
    if normalized.startswith("frontend/"):
        return "frontend_product"
    if normalized.startswith(RUNTIME_PROTOCOL_PREFIXES):
        return "runtime_protocol"
    if normalized.startswith(RUNTIME_PROVIDER_PREFIXES):
        return "runtime_provider"
    for prefix, rules in (
        ("backend/internal/service/", SERVICE_PATH_RULES),
        ("backend/internal/handler/", HANDLER_PATH_RULES),
        ("backend/internal/repository/", REPOSITORY_PATH_RULES),
    ):
        if normalized.startswith(prefix):
            relative = normalized.removeprefix(prefix)
            for category, markers in rules:
                if relative.startswith(markers):
                    return category
            break
    if normalized.startswith("backend/internal/domain/"):
        if normalized.endswith(("openai_messages_dispatch.go", "reasoning_effort.go")):
            return "runtime_protocol"
        return "productcore"
    if normalized.startswith("backend/internal/model/error_passthrough_"):
        return "runtime_protocol"
    if normalized.startswith("backend/internal/model/tls_fingerprint_"):
        return "runtime_provider"
    if normalized.startswith("backend/internal/platform/liveattestation/"):
        return "runtime_provider"
    if normalized.startswith(("backend/internal/productcore/", "backend/internal/securityaudit/", "backend/internal/web/")):
        return "productcore"
    if normalized.startswith(("backend/internal/repository/user_", "backend/internal/server/routes/user.go")):
        return "productcore"
    if normalized.startswith("backend/internal/server/"):
        return "infrastructure"
    if normalized.startswith(
        (
            "backend/internal/config/",
            "backend/internal/middleware/",
            "backend/internal/pkg/",
            "backend/internal/setup/",
            "backend/internal/util/",
        )
    ):
        return "infrastructure"
    if normalized.startswith("backend/internal/testutil/"):
        return "test"
    if normalized.startswith("backend/resources/model-pricing/"):
        return "productcore"
    if normalized.startswith("backend/internal/") and any(marker in normalized for marker in PRODUCT_MARKERS):
        return "productcore"
    if normalized.startswith(("assets/", "openspec/", "skills/")):
        return "documentation"
    if normalized.startswith(
        (
            "deploy/",
            ".github/",
            "Dockerfile",
            "Makefile",
            "backend/cmd/",
            "backend/go.mod",
            "backend/go.sum",
            "backend/Makefile",
            "backend/Dockerfile",
            "backend/.dockerignore",
            "backend/.golangci.yml",
            "backend/scripts/",
            "tools/",
            ".dockerignore",
            ".gitattributes",
            ".gitignore",
            ".goreleaser",
        )
    ):
        return "infrastructure"
    if normalized.endswith("_test.go") or "/testdata/" in normalized:
        return "test"
    if normalized.startswith(("docs/", "README", "CLA.md", "DEV_GUIDE.md", "LICENSE")):
        return "documentation"
    return "needs_review"


def classify_commit(subject: str) -> str:
    for category, subjects in REVIEWED_COMMIT_SUBJECTS.items():
        if subject in subjects:
            return category
    normalized = " ".join(subject.lower().split())
    if normalized.startswith("merge "):
        return "merge"
    if normalized == "chore: update sponsors":
        return "documentation"
    if any(marker in normalized for marker in COMMIT_PROVIDER_MARKERS):
        return "runtime_provider"
    if any(marker in normalized for marker in COMMIT_PROTOCOL_MARKERS):
        return "runtime_protocol"
    if any(marker in normalized for marker in COMMIT_PRODUCT_MARKERS):
        return "productcore"
    if normalized.startswith(("docs", "doc", "readme")):
        return "documentation"
    if any(marker in normalized for marker in ("migration", "schema", "index")):
        return "database"
    if any(marker in normalized for marker in ("frontend", " ui", "admin page", "web ui")):
        return "frontend_product"
    if any(marker in normalized for marker in ("docker", "ci", "workflow", "dependency", "dependencies", "deps")):
        return "infrastructure"
    if normalized.startswith(("test", "tests")):
        return "test"
    return "needs_review"


def migration_number(path: str):
    match = re.search(r"(?:^|/)migrations/(\d+)[a-zA-Z_].*\.sql$", path.replace("\\", "/"))
    return int(match.group(1)) if match else None


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def include_inventory_path(relative_path: str) -> bool:
    normalized = relative_path.replace("\\", "/")
    path = Path(normalized)
    if any(part in IGNORED_PARTS for part in path.parts):
        return False
    if normalized == "frontend/dist" or normalized.startswith("frontend/dist/"):
        return False
    return path.suffix.lower() not in IGNORED_SUFFIXES


def compare_trees(official: Path, current: Path, excluded_current_prefixes: tuple[str, ...] = ()):
    def include_current(path: Path) -> bool:
        relative = path.relative_to(current).as_posix()
        excluded = any(relative == prefix or relative.startswith(prefix + "/") for prefix in excluded_current_prefixes)
        return not excluded and include_inventory_path(relative)

    official_files = {
        p.relative_to(official).as_posix(): p
        for p in official.rglob("*")
        if p.is_file() and include_inventory_path(p.relative_to(official).as_posix())
    }
    current_files = {
        p.relative_to(current).as_posix(): p
        for p in current.rglob("*")
        if p.is_file() and include_current(p)
    }
    rows = []
    for path in sorted(set(official_files) | set(current_files)):
        left = official_files.get(path)
        right = current_files.get(path)
        if left is None:
            state = "current_only"
        elif right is None:
            state = "official_only"
        else:
            state = "same" if sha256_file(left) == sha256_file(right) else "different"
        rows.append(
            {
                "path": path,
                "state": state,
                "category": classify_path(path),
                "official_sha256": sha256_file(left) if left else "",
                "current_sha256": sha256_file(right) if right else "",
                "migration_number": migration_number(path) or "",
            }
        )
    return rows


def resolve_tag_commit(ref_payload: dict, tag_payload: dict | None = None) -> str:
    obj = ref_payload["object"]
    if obj["type"] == "commit":
        return obj["sha"]
    if obj["type"] == "tag" and tag_payload:
        return tag_payload["object"]["sha"]
    raise ValueError("annotated tag payload is required")


def verify_target_commit(actual: str, expected: str) -> None:
    if actual != expected:
        raise ValueError(f"target commit mismatch: expected {expected}, got {actual}")


def write_csv(path: Path, rows: list[dict], fieldnames: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)


def write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    path.write_text(text, encoding="utf-8", newline="\n")


def github_headers() -> dict[str, str]:
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "XCode-Upstream-Inventory",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def request_bytes(url: str) -> bytes:
    request = urllib.request.Request(url, headers=github_headers())
    with urllib.request.urlopen(request, timeout=120) as response:
        return response.read()


def github_json(path: str) -> dict:
    return json.loads(request_bytes("https://api.github.com" + path))


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def extract_archive(archive: bytes, destination: Path) -> str:
    with zipfile.ZipFile(BytesIO(archive)) as bundle:
        names = [name for name in bundle.namelist() if name and not name.endswith("/")]
        if not names:
            raise ValueError("official archive is empty")
        roots = {name.replace("\\", "/").split("/", 1)[0] for name in names}
        if len(roots) != 1:
            raise ValueError("official archive must contain one source root")
        for name in bundle.namelist():
            normalized = name.replace("\\", "/")
            parts = Path(normalized).parts
            if normalized.startswith("/") or ".." in parts:
                raise ValueError(f"unsafe archive member: {name}")
        bundle.extractall(destination)
    return next(iter(roots))


def snapshot_upstream(
    *,
    repo: str,
    base: str,
    target: str,
    expected_commit: str,
    cache_dir: Path,
    fetch_json=github_json,
    download_bytes=request_bytes,
    generated_at: str | None = None,
) -> dict:
    cache_dir = Path(cache_dir)
    raw_dir = cache_dir / "raw"
    raw_dir.mkdir(parents=True, exist_ok=True)

    release = fetch_json(f"/repos/{repo}/releases/tags/{urllib.parse.quote(target, safe='')}")
    write_json(raw_dir / "release.json", release)
    ref = fetch_json(f"/repos/{repo}/git/ref/tags/{urllib.parse.quote(target, safe='')}")
    write_json(raw_dir / "tag-ref.json", ref)

    tag_payload = None
    if ref.get("object", {}).get("type") == "tag":
        tag_sha = ref["object"]["sha"]
        tag_payload = fetch_json(f"/repos/{repo}/git/tags/{tag_sha}")
        write_json(raw_dir / "annotated-tag.json", tag_payload)
    target_commit = resolve_tag_commit(ref, tag_payload)
    verify_target_commit(target_commit, expected_commit)

    compare_pages = []
    collected = 0
    total_commits = None
    page = 1
    encoded_base = urllib.parse.quote(base, safe="")
    encoded_target = urllib.parse.quote(target, safe="")
    while total_commits is None or collected < total_commits:
        path = f"/repos/{repo}/compare/{encoded_base}...{encoded_target}?per_page=100&page={page}"
        payload = fetch_json(path)
        write_json(raw_dir / f"compare-page-{page:03d}.json", payload)
        commits = payload.get("commits", [])
        if total_commits is None:
            total_commits = int(payload.get("total_commits", len(commits)))
        if not commits and collected < total_commits:
            raise ValueError(f"compare pagination stopped at {collected} of {total_commits} commits")
        compare_pages.append(payload)
        collected += len(commits)
        page += 1
    if collected != total_commits:
        raise ValueError(f"compare commit count mismatch: expected {total_commits}, got {collected}")

    archive_url = f"https://github.com/{repo}/archive/refs/tags/{urllib.parse.quote(target, safe='')}.zip"
    archive = download_bytes(archive_url)
    archive_sha256 = hashlib.sha256(archive).hexdigest()
    archive_path = cache_dir / f"{target}-{archive_sha256}.zip"
    archive_path.write_bytes(archive)
    extraction_dir = cache_dir / "source" / archive_sha256
    extraction_dir.mkdir(parents=True, exist_ok=True)
    source_name = extract_archive(archive, extraction_dir)
    source_root = extraction_dir / source_name
    version_path = source_root / "backend" / "cmd" / "server" / "VERSION"
    official_version = version_path.read_text(encoding="utf-8").strip()

    manifest = {
        "repo": repo,
        "base_tag": base,
        "target_tag": target,
        "target_commit": target_commit,
        "expected_commit": expected_commit,
        "release_published_at": release.get("published_at", ""),
        "archive_sha256": archive_sha256,
        "archive_file": archive_path.relative_to(cache_dir).as_posix(),
        "source_root": source_root.relative_to(cache_dir).as_posix(),
        "official_version_file": official_version,
        "commit_count": total_commits,
        "compare_page_count": len(compare_pages),
        "generated_at": generated_at or utc_now(),
    }
    write_json(cache_dir / "snapshot.json", manifest)
    return manifest


def load_compare_commits(snapshot_dir: Path, page_count: int) -> list[dict]:
    rows = []
    seen = set()
    paths = [snapshot_dir / "raw" / f"compare-page-{page:03d}.json" for page in range(1, page_count + 1)]
    for path in paths:
        payload = json.loads(path.read_text(encoding="utf-8"))
        for commit in payload.get("commits", []):
            sha = commit["sha"]
            if sha not in seen:
                rows.append(commit)
                seen.add(sha)
    return rows


def generate_inventory(snapshot_dir: Path, current_root: Path, output_dir: Path) -> None:
    snapshot_dir = Path(snapshot_dir)
    current_root = Path(current_root)
    output_dir = Path(output_dir)
    manifest = json.loads((snapshot_dir / "snapshot.json").read_text(encoding="utf-8"))
    commits = load_compare_commits(snapshot_dir, int(manifest["compare_page_count"]))
    if len(commits) != int(manifest["commit_count"]):
        raise ValueError(
            f"snapshot commit count mismatch: expected {manifest['commit_count']}, got {len(commits)}"
        )

    commit_rows = []
    for commit in commits:
        details = commit.get("commit", {})
        author = details.get("author") or details.get("committer") or {}
        subject = details.get("message", "").splitlines()[0].strip()
        commit_rows.append(
            {
                "sha": commit["sha"],
                "date": author.get("date", ""),
                "subject": subject,
                "category": classify_commit(subject),
                "files_known": "false",
            }
        )

    source_root = snapshot_dir / manifest["source_root"]
    excluded_prefixes = ()
    try:
        output_relative = output_dir.resolve().relative_to(current_root.resolve()).as_posix()
        excluded_prefixes = (output_relative,)
    except ValueError:
        pass
    file_rows = compare_trees(source_root, current_root, excluded_prefixes)
    metadata = {field: manifest[field] for field in METADATA_FIELDS}
    write_json(output_dir / "metadata.json", metadata)
    write_csv(output_dir / "commits.csv", commit_rows, list(COMMIT_FIELDS))
    write_csv(output_dir / "files.csv", file_rows, list(FILE_FIELDS))


def read_csv(path: Path) -> list[dict]:
    with path.open(encoding="utf-8", newline="") as handle:
        return list(csv.DictReader(handle))


def feature_rows_from_markdown(markdown: str) -> list[dict]:
    lines = markdown.splitlines()
    required = {"ID", "官方提交", "归宿", "阶段"}
    for index, line in enumerate(lines[:-1]):
        if not line.strip().startswith("|"):
            continue
        headers = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if not required.issubset(headers):
            continue
        separator = [cell.strip() for cell in lines[index + 1].strip().strip("|").split("|")]
        if len(separator) != len(headers) or not all(re.fullmatch(r":?-{3,}:?", cell) for cell in separator):
            raise ValueError("feature matrix header is missing its separator row")
        aliases = {"ID": "id", "官方提交": "commits", "归宿": "disposition", "阶段": "phase"}
        rows = []
        for row_line in lines[index + 2 :]:
            if not row_line.strip().startswith("|"):
                break
            cells = [cell.strip() for cell in row_line.strip().strip("|").split("|")]
            if len(cells) != len(headers):
                raise ValueError(f"feature matrix row has {len(cells)} cells, expected {len(headers)}")
            source = dict(zip(headers, cells))
            rows.append({target: source[header].strip("`") for header, target in aliases.items()})
        if not rows:
            raise ValueError("feature matrix contains no rows")
        return rows
    raise ValueError("feature matrix table is missing")


def validate_inventory(inventory_dir: Path) -> None:
    inventory_dir = Path(inventory_dir)
    metadata = json.loads((inventory_dir / "metadata.json").read_text(encoding="utf-8"))
    commits = read_csv(inventory_dir / "commits.csv")
    files = read_csv(inventory_dir / "files.csv")
    if metadata.get("target_tag") == "v0.1.179":
        verify_target_commit(metadata.get("target_commit", ""), "75f88be5f75c27771836b586f7de1503afa0e3bc")
        if len(commits) != 594 or int(metadata.get("commit_count", -1)) != 594:
            raise ValueError(f"commit count mismatch: expected 594, got {len(commits)}")
    unresolved_commits = [row["sha"] for row in commits if not row.get("category") or row["category"] == "needs_review"]
    unresolved_files = [row["path"] for row in files if not row.get("category") or row["category"] == "needs_review"]
    if unresolved_commits:
        raise ValueError(f"needs_review commits: {len(unresolved_commits)}")
    if unresolved_files:
        raise ValueError(f"needs_review files: {len(unresolved_files)}")
    if not (inventory_dir / "runtime-feature-matrix.md").is_file():
        raise ValueError("runtime feature matrix is missing")
    features = feature_rows_from_markdown(
        (inventory_dir / "runtime-feature-matrix.md").read_text(encoding="utf-8")
    )
    validate_matrix(commits, features)
    if not (inventory_dir / "database-impact.md").is_file():
        raise ValueError("database impact mapping is missing")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Build and validate a Sub2API upstream inventory")
    commands = parser.add_subparsers(dest="command", required=True)

    snapshot = commands.add_parser("snapshot")
    snapshot.add_argument("--repo", required=True)
    snapshot.add_argument("--base", required=True)
    snapshot.add_argument("--target", required=True)
    snapshot.add_argument("--expected-commit", required=True)
    snapshot.add_argument("--cache-dir", type=Path, required=True)

    generate = commands.add_parser("generate")
    generate.add_argument("--snapshot-dir", type=Path, required=True)
    generate.add_argument("--current-root", type=Path, required=True)
    generate.add_argument("--output-dir", type=Path, required=True)

    validate = commands.add_parser("validate")
    validate.add_argument("--inventory-dir", type=Path, required=True)
    return parser


def main(argv=None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "snapshot":
        snapshot_upstream(
            repo=args.repo,
            base=args.base,
            target=args.target,
            expected_commit=args.expected_commit,
            cache_dir=args.cache_dir,
        )
    elif args.command == "generate":
        generate_inventory(args.snapshot_dir, args.current_root, args.output_dir)
    else:
        validate_inventory(args.inventory_dir)
    return 0


def validate_matrix(commits: list[dict], features: list[dict]) -> None:
    invalid = [
        row.get("id", "<missing>")
        for row in features
        if row.get("disposition") not in DISPOSITIONS or row.get("phase") not in {"2", "3", "4", "5", "6"}
    ]
    if invalid:
        raise ValueError("invalid feature rows: " + ", ".join(invalid))
    covered = {sha.strip() for row in features for sha in row.get("commits", "").split() if sha.strip()}
    runtime_commits = [row for row in commits if row.get("category", "").startswith("runtime_")]
    missing = sorted(row["sha"] for row in runtime_commits if row["sha"] not in covered)
    if missing:
        raise ValueError("uncovered commits: " + ", ".join(missing))


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as error:
        print(f"error: {error}", file=sys.stderr)
        raise SystemExit(1)
