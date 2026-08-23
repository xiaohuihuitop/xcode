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
COMMIT_PROTOCOL_MARKERS = (
    "apicompat",
    "chat completion",
    "chatcompletion",
    "input token",
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


def classify_commit(subject: str) -> str:
    normalized = " ".join(subject.lower().split())
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
        if row.get("disposition") not in DISPOSITIONS or not row.get("phase")
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
