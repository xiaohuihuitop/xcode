#!/usr/bin/env python3
import hashlib
import re
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


def classify_path(path: str) -> str:
    normalized = path.replace("\\", "/")
    if normalized.startswith("backend/migrations/") or normalized.startswith("backend/ent/schema/"):
        return "database"
    if normalized.startswith("frontend/"):
        return "frontend_product"
    if normalized.startswith(RUNTIME_PROTOCOL_PREFIXES):
        return "runtime_protocol"
    if normalized.startswith(RUNTIME_PROVIDER_PREFIXES):
        return "runtime_provider"
    if normalized.startswith("backend/internal/") and any(marker in normalized for marker in PRODUCT_MARKERS):
        return "productcore"
    if normalized.startswith(("deploy/", ".github/", "Dockerfile", "Makefile")):
        return "infrastructure"
    if normalized.endswith("_test.go") or "/testdata/" in normalized:
        return "test"
    if normalized.startswith(("docs/", "README")):
        return "documentation"
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


def compare_trees(official: Path, current: Path):
    official_files = {p.relative_to(official).as_posix(): p for p in official.rglob("*") if p.is_file()}
    current_files = {p.relative_to(current).as_posix(): p for p in current.rglob("*") if p.is_file()}
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
        rows.append({"path": path, "state": state, "category": classify_path(path)})
    return rows


def resolve_tag_commit(ref_payload: dict, tag_payload: dict | None = None) -> str:
    obj = ref_payload["object"]
    if obj["type"] == "commit":
        return obj["sha"]
    if obj["type"] == "tag" and tag_payload:
        return tag_payload["object"]["sha"]
    raise ValueError("annotated tag payload is required")


def validate_matrix(commits: list[dict], features: list[dict]) -> None:
    invalid = [
        row.get("id", "<missing>")
        for row in features
        if row.get("disposition") not in DISPOSITIONS or not row.get("phase")
    ]
    if invalid:
        raise ValueError("invalid feature rows: " + ", ".join(invalid))
    covered = {sha.strip() for row in features for sha in row.get("commits", "").split() if sha.strip()}
    runtime_commits = [row for row in commits if row.get("category", "").startswith("runtime_")]
    missing = sorted(row["sha"] for row in runtime_commits if row["sha"] not in covered)
    if missing:
        raise ValueError("uncovered commits: " + ", ".join(missing))
