#!/usr/bin/env python3
"""将模板化题库包精简为每个知识点 1 条（优先 opening）。"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# 复用现有导入脚本的校验与规范化
from interview_bank_import import normalize_import_package

ROLE_PRIORITY = {"opening": 0, "mixed": 1, "followup": 2}
ID_SUFFIX_RE = re.compile(r"-\d+-[0-9a-f]{6,}$", re.IGNORECASE)


def knowledge_key(item: dict[str, Any]) -> str:
    item_id = str(item.get("id") or "").strip()
    if item_id:
        return ID_SUFFIX_RE.sub("", item_id)
    subject = str(item.get("subject") or item.get("title") or "").strip()
    category = str(item.get("category") or item.get("domain") or "").strip()
    return f"{category}|{subject}"


def pick_one(items: list[dict[str, Any]]) -> dict[str, Any]:
    return min(
        items,
        key=lambda item: (
            ROLE_PRIORITY.get(str(item.get("questionRole") or item.get("question_role") or ""), 99),
            str(item.get("id") or ""),
        ),
    )


def thin_items(items: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], dict[str, int]]:
    groups: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for item in items:
        if isinstance(item, dict):
            groups[knowledge_key(item)].append(item)
    selected = [pick_one(group) for group in groups.values()]
    selected.sort(key=lambda item: str(item.get("id") or ""))
    stats = {
        "source_count": len(items),
        "group_count": len(groups),
        "selected_count": len(selected),
        "removed_count": max(0, len(items) - len(selected)),
    }
    return selected, stats


def load_items(path: Path) -> list[dict[str, Any]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(payload, dict):
        if isinstance(payload.get("items"), list):
            return [item for item in payload["items"] if isinstance(item, dict)]
        if isinstance(payload.get("atoms"), list):
            return [item for item in payload["atoms"] if isinstance(item, dict)]
        return [payload]
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    raise ValueError("unsupported package shape")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Thin interview bank package to one atom per knowledge point.")
    parser.add_argument("--input", required=True, help="Source package JSON path")
    parser.add_argument("--output", required=True, help="Output package JSON path")
    parser.add_argument("--batch-id", default="", help="Override output batchId")
    parser.add_argument("--source-ref", default="", help="Default sourceRef if missing")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    input_path = Path(args.input).expanduser().resolve()
    output_path = Path(args.output).expanduser().resolve()
    if not input_path.is_file():
        print(f"input not found: {input_path}", file=sys.stderr)
        return 1

    selected, stats = thin_items(load_items(input_path))
    package = normalize_import_package(
        selected,
        {
            "source_ref": args.source_ref or f"thinned-from:{input_path.name}",
            "domain": "",
            "category": "",
            "difficulty": "L2",
            "question_role": "opening",
            "status": "published",
        },
        batch_id=args.batch_id or f"qb-mainstream-it-opening-{datetime.now(timezone.utc).strftime('%Y%m%d')}",
    )
    package["reviewReport"]["thinStats"] = stats
    package["validationReport"]["tool"] = "scripts/thin_interview_bank_package.py"
    package["validationReport"]["warnings"] = list(package["validationReport"].get("warnings") or [])
    package["validationReport"]["warnings"].append(
        f"thinned {stats['source_count']} -> {stats['selected_count']} by knowledge key (prefer opening)"
    )

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(package, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"wrote: {output_path}")
    print(f"items: {stats['selected_count']} (from {stats['source_count']}, groups={stats['group_count']})")
    errors = package["validationReport"].get("errors") or []
    if errors:
        print("validation errors:")
        for error in errors[:20]:
            print(f"  - {error}")
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
