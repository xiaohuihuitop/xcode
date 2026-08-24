#!/usr/bin/env python3
"""Build and validate a reviewable Sub2API Runtime sync plan."""

from __future__ import annotations

import argparse
import json
import re
import shutil
from dataclasses import asdict, dataclass
from pathlib import Path

try:
    from tools import sub2api_upstream_inventory as inventory
except ModuleNotFoundError:  # Direct execution from the repository root.
    import sub2api_upstream_inventory as inventory


TAG_PATTERN = re.compile(r"^v\d+\.\d+\.\d+$")
FULL_SHA_PATTERN = re.compile(r"^[0-9a-f]{40}$")
DIRECT_SYNC_PREFIXES = (
    "backend/internal/runtime/sub2api/upstream/",
    "backend/internal/pkg/apicompat/",
)
OFFICIAL_RUNTIME_PREFIX = "backend/internal/runtime/sub2api/upstream/"
TARGET_PATH_PREFIXES = {
    "backend/internal/pkg/apicompat/": OFFICIAL_RUNTIME_PREFIX + "apicompat/",
}


@dataclass(frozen=True)
class SyncPlan:
    base_tag: str
    target_tag: str
    target_commit: str

    @property
    def target(self) -> str:
        return f"{self.target_tag}@{self.target_commit}"


def build_sync_plan(base_tag: str, target_tag: str, target_commit: str) -> SyncPlan:
    if not TAG_PATTERN.fullmatch(base_tag) or not TAG_PATTERN.fullmatch(target_tag):
        raise ValueError("sync requires immutable tag names, not a moving branch or ref")
    if not FULL_SHA_PATTERN.fullmatch(target_commit):
        raise ValueError("target commit must be a full lowercase commit SHA")
    if base_tag == target_tag:
        raise ValueError("base and target tags must differ")
    return SyncPlan(base_tag, target_tag, target_commit)


def disposition_for_path(path: str, category: str) -> str:
    normalized = path.replace("\\", "/")
    if normalized.startswith("backend/pkg/runtimebridge/v1/"):
        return "adapter_port"
    if normalized.startswith(DIRECT_SYNC_PREFIXES):
        return "direct_sync"
    if category in {"runtime_protocol", "runtime_provider", "test"}:
        return "adapter_port"
    if category == "productcore":
        return "productcore_mapping"
    return "not_runtime"


def target_path_for_path(path: str) -> str:
    """Return the explicit XCode Official Runtime destination for a source path."""
    normalized = path.replace("\\", "/")
    if normalized.startswith(OFFICIAL_RUNTIME_PREFIX):
        return normalized
    for source_prefix, target_prefix in TARGET_PATH_PREFIXES.items():
        if normalized.startswith(source_prefix):
            return target_prefix + normalized[len(source_prefix) :]
    raise ValueError(f"direct_sync path has no Official Runtime target mapping: {path}")


def _validate_relative_path(path: str, field: str) -> str:
    normalized = path.replace("\\", "/")
    candidate = Path(normalized)
    parts = candidate.parts
    if not normalized or candidate.is_absolute() or candidate.anchor or ".." in parts:
        raise ValueError(f"{field} must be a safe repository-relative path: {path}")
    return normalized


def build_file_plan(file_rows: list[dict]) -> list[dict]:
    planned = []
    for row in file_rows:
        path = row["path"]
        disposition = disposition_for_path(path, row.get("category", "needs_review"))
        inventory.validate_sync_path(path, disposition)
        state = row.get("state", "")
        action = {
            "same": "unchanged",
            "different": "candidate",
            "official_only": "candidate",
            "current_only": "preserve",
        }.get(state)
        if action is None:
            raise ValueError(f"invalid file state in sync plan: {path}")
        source_path = path.replace("\\", "/")
        target_path = target_path_for_path(path) if disposition == "direct_sync" else ""
        planned.append(
            {
                "path": source_path,
                "source_path": source_path,
                "target_path": target_path,
                "state": state,
                "category": row.get("category", ""),
                "disposition": disposition,
                "action": action,
                "official_sha256": row.get("official_sha256", ""),
                "current_sha256": row.get("current_sha256", ""),
                "migration_number": row.get("migration_number", ""),
                "approved": False,
            }
        )
    return planned


def build_sync_plan_document(snapshot_dir: Path, current_root: Path, output_dir: Path) -> dict:
    snapshot_dir = Path(snapshot_dir)
    current_root = Path(current_root)
    output_dir = Path(output_dir)
    manifest = json.loads((snapshot_dir / "snapshot.json").read_text(encoding="utf-8"))
    plan_identity = build_sync_plan(
        manifest["base_tag"],
        manifest["target_tag"],
        manifest["target_commit"],
    )
    inventory.generate_inventory(snapshot_dir, current_root, output_dir)
    files = inventory.read_csv(output_dir / "files.csv")
    return {
        **asdict(plan_identity),
        "target": plan_identity.target,
        "source": "Wei-Shaw/sub2api",
        "source_root": str((snapshot_dir / manifest["source_root"]).resolve()),
        "files": build_file_plan(files),
    }


def validate_sync_plan_document(plan: dict, inventory_files: list[dict] | None = None) -> None:
    missing_identity = [
        field for field in ("base_tag", "target_tag", "target_commit") if field not in plan
    ]
    if missing_identity:
        raise ValueError("sync plan is missing identity fields: " + ", ".join(missing_identity))
    build_sync_plan(plan["base_tag"], plan["target_tag"], plan["target_commit"])
    source_root = plan.get("source_root")
    if not isinstance(source_root, str) or not source_root:
        raise ValueError("sync plan source_root is required")
    files = plan.get("files")
    if not isinstance(files, list):
        raise ValueError("sync plan files must be a list")
    seen = set()
    inventory_by_path = {row.get("path", ""): row for row in inventory_files or []}
    if inventory_files is not None and not inventory_by_path:
        raise ValueError("sync plan inventory files are required")
    for row in files:
        path = row.get("source_path", row.get("path", ""))
        path = _validate_relative_path(path, "source_path")
        if path in seen:
            raise ValueError(f"duplicate sync plan path: {path}")
        seen.add(path)
        if row.get("path", path) != path:
            raise ValueError(f"sync plan path/source_path mismatch: {path}")
        inventory.validate_sync_path(
            path,
            row.get("disposition", ""),
            migration_handling=row.get("migration_handling"),
        )
        if row.get("action") not in {"unchanged", "candidate", "preserve"}:
            raise ValueError(f"invalid sync plan action: {path}")
        if not isinstance(row.get("approved"), bool):
            raise ValueError(f"sync plan approval must be boolean: {path}")
        target_path = row.get("target_path", "")
        if row.get("disposition") == "direct_sync":
            target_path = _validate_relative_path(target_path, "target_path")
            if not target_path.startswith(OFFICIAL_RUNTIME_PREFIX):
                raise ValueError(f"direct_sync target is outside Official Runtime zone: {target_path}")
        elif target_path:
            raise ValueError(f"non-direct sync row cannot have target_path: {path}")
        if inventory_files is not None:
            expected = inventory_by_path.get(path)
            if expected is None:
                raise ValueError(f"sync plan path is missing from inventory: {path}")
            for field in ("state", "category", "official_sha256", "current_sha256", "migration_number"):
                if row.get(field, "") != expected.get(field, ""):
                    raise ValueError(f"sync plan {field} mismatch: {path}")
    if inventory_files is not None and seen != set(inventory_by_path):
        missing = sorted(set(inventory_by_path) - seen)
        extra = sorted(seen - set(inventory_by_path))
        details = []
        if missing:
            details.append("missing " + ", ".join(missing))
        if extra:
            details.append("extra " + ", ".join(extra))
        raise ValueError("sync plan does not match inventory: " + "; ".join(details))


def apply_sync_plan(plan: dict, worktree: Path) -> None:
    """Copy only approved files that already target the Official Runtime zone."""
    validate_sync_plan_document(plan)
    source_root = Path(plan["source_root"]).resolve()
    if not source_root.is_dir():
        raise ValueError(f"sync plan source_root does not exist: {source_root}")
    worktree = Path(worktree).resolve()
    for row in plan.get("files", []):
        path = row.get("source_path", row.get("path", ""))
        disposition = row.get("disposition", "")
        inventory.validate_sync_path(path, disposition, migration_handling=row.get("migration_handling"))
        action = row.get("action")
        if disposition == "direct_sync":
            if action not in {"candidate", "unchanged"}:
                continue
            if row.get("approved") is not True:
                if action == "candidate":
                    raise ValueError(f"direct_sync candidate requires explicit approval: {path}")
                continue
        elif action != "candidate":
            continue
        else:
            continue
        source_relative = Path(_validate_relative_path(path, "source_path"))
        target_relative = Path(_validate_relative_path(row.get("target_path", ""), "target_path"))
        if not target_relative.as_posix().startswith(OFFICIAL_RUNTIME_PREFIX):
            raise ValueError(f"direct_sync target is outside Official Runtime zone: {path}")
        source = (source_root / source_relative).resolve()
        target = (worktree / target_relative).resolve()
        try:
            source.relative_to(source_root)
        except ValueError as error:
            raise ValueError(f"sync plan source escapes snapshot root: {path}") from error
        if not source.is_file():
            raise ValueError(f"sync plan source file is missing: {source}")
        try:
            target.relative_to(worktree)
        except ValueError as error:
            raise ValueError(f"sync plan target escapes worktree: {path}") from error
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)


def write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    snapshot = commands.add_parser("snapshot")
    snapshot.add_argument("--repo", required=True)
    snapshot.add_argument("--base", required=True)
    snapshot.add_argument("--target", required=True)
    snapshot.add_argument("--expected-commit", required=True)
    snapshot.add_argument("--cache-dir", type=Path, required=True)

    plan = commands.add_parser("plan")
    plan.add_argument("--snapshot-dir", type=Path, required=True)
    plan.add_argument("--current-root", type=Path, required=True)
    plan.add_argument("--output-dir", type=Path, required=True)

    apply = commands.add_parser("apply")
    apply.add_argument("--plan", type=Path, required=True)
    apply.add_argument("--mode", choices={"runtime-only"}, required=True)
    apply.add_argument("--worktree", type=Path, required=True)

    validate = commands.add_parser("validate")
    validate.add_argument("--inventory-dir", type=Path, required=True)
    validate.add_argument("--current-root", type=Path, default=Path("."))
    return parser


def main(argv=None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "snapshot":
        inventory.snapshot_upstream(
            repo=args.repo,
            base=args.base,
            target=args.target,
            expected_commit=args.expected_commit,
            cache_dir=args.cache_dir,
        )
        return 0
    if args.command == "plan":
        document = build_sync_plan_document(args.snapshot_dir, args.current_root, args.output_dir)
        write_json(args.output_dir / "sync-plan.json", document)
        return 0
    if args.command == "apply":
        plan = json.loads(args.plan.read_text(encoding="utf-8"))
        apply_sync_plan(plan, args.worktree)
        return 0

    inventory.validate_inventory(args.inventory_dir, args.current_root)
    plan_path = args.inventory_dir / "sync-plan.json"
    if not plan_path.is_file():
        raise ValueError("sync-plan.json is required")
    validate_sync_plan_document(
        json.loads(plan_path.read_text(encoding="utf-8")),
        inventory.read_csv(args.inventory_dir / "files.csv"),
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
