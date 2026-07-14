#!/usr/bin/env python3
"""
Build normalized interview-bank import packages for the current admin workflow.

Supported inputs in this minimal version:
  - a single JSON atom object
  - a JSON array of atom objects
  - a JSON package containing `items` or `atoms`
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
import uuid
from collections.abc import Iterable
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

VALID_CATEGORIES = {
    "java",
    "database",
    "cache",
    "middleware",
    "system_design",
    "frontend",
    "ai_llm",
    "hr_soft_skill",
}
VALID_DIFFICULTIES = {"L1", "L2", "L3", "L4", "L5"}
VALID_QUESTION_ROLES = {"opening", "followup", "mixed"}
VALID_STATUSES = {"draft", "published", "archived"}
DOC_EXTS = {".txt", ".md", ".pdf", ".docx"}
JSON_EXTS = {".json"}
SUPPORTED_EXTS = DOC_EXTS | JSON_EXTS


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build interview-bank import package JSON for admin validate/publish flows.")
    parser.add_argument("--input", action="append", required=True, help="Source file or directory. Repeatable.")
    parser.add_argument("--output", help="Output file path. Defaults to interview_bank_imports/<batchId>.json")
    parser.add_argument("--batch-id", help="Override generated batch id.")
    parser.add_argument("--source-ref", help="Default sourceRef for atoms missing this field.")
    parser.add_argument("--domain", help="Default domain for atoms missing this field.")
    parser.add_argument("--category", help="Default category for atoms missing this field.")
    parser.add_argument("--difficulty", help="Default difficulty for atoms missing this field.")
    parser.add_argument("--question-role", default="opening", help="Default question role for atoms missing this field.")
    parser.add_argument("--status", default="published", help="Default status for atoms missing this field.")
    parser.add_argument("--base-url", default=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"))
    parser.add_argument("--api-key", default=os.getenv("OPENAI_API_KEY"))
    parser.add_argument("--model", default=os.getenv("OPENAI_MODEL", "gpt-4.1-mini"))
    parser.add_argument("--chunk-size", type=int, default=5000)
    parser.add_argument("--overlap", type=int, default=300)
    parser.add_argument("--max-atoms-per-chunk", type=int, default=6)
    parser.add_argument("--pause-seconds", type=float, default=0.1)
    return parser.parse_args()


def collect_files(paths: list[Path]) -> list[Path]:
    files: list[Path] = []
    for path in paths:
        if path.is_file() and path.suffix.lower() in SUPPORTED_EXTS:
            files.append(path)
            continue
        if path.is_dir():
            for child in path.rglob("*"):
                if child.is_file() and child.suffix.lower() in SUPPORTED_EXTS:
                    files.append(child)
    return sorted(files)


def extract_text(path: Path) -> str:
    ext = path.suffix.lower()
    if ext in {".txt", ".md"}:
        return path.read_text(encoding="utf-8", errors="ignore").strip()
    if ext == ".pdf":
        try:
            import PyPDF2
        except ImportError as exc:
            raise RuntimeError("解析 PDF 需要安装 PyPDF2。") from exc
        parts: list[str] = []
        with path.open("rb") as handle:
            reader = PyPDF2.PdfReader(handle)
            for page in reader.pages:
                text = page.extract_text()
                if text:
                    parts.append(text)
        return "\n".join(parts).strip()
    if ext == ".docx":
        try:
            from docx import Document
        except ImportError as exc:
            raise RuntimeError("解析 DOCX 需要安装 python-docx。") from exc
        document = Document(str(path))
        return "\n".join(paragraph.text for paragraph in document.paragraphs if paragraph.text.strip()).strip()
    raise RuntimeError(f"不支持的文档类型：{path.suffix}")


def chunk_text(text: str, chunk_size: int, overlap: int) -> list[str]:
    if not text:
        return []
    chunks: list[str] = []
    cursor = 0
    step = max(1, chunk_size - overlap)
    while cursor < len(text):
        chunks.append(text[cursor : cursor + chunk_size])
        cursor += step
    return chunks


def load_json_atoms(path: Path) -> list[dict[str, Any]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(payload, dict):
        if isinstance(payload.get("items"), list):
            return [item for item in payload["items"] if isinstance(item, dict)]
        if isinstance(payload.get("atoms"), list):
            return [item for item in payload["atoms"] if isinstance(item, dict)]
        return [payload]
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    raise ValueError("Expected a JSON object, array, or package with items/atoms.")


def parse_atoms(raw: str) -> list[dict[str, Any]]:
    cleaned = raw.strip()
    cleaned = re.sub(r"^```(?:json)?\s*", "", cleaned)
    cleaned = re.sub(r"\s*```$", "", cleaned)
    payload = json.loads(cleaned)
    if isinstance(payload, dict) and isinstance(payload.get("atoms"), list):
        payload = payload["atoms"]
    if isinstance(payload, dict):
        payload = [payload]
    if not isinstance(payload, list):
        raise ValueError("Expected model output to be a JSON array of atoms.")
    return [item for item in payload if isinstance(item, dict)]


def call_chat_api(
    *,
    text: str,
    category: str,
    source_name: str,
    base_url: str,
    api_key: str,
    model: str,
    max_atoms: int,
) -> list[dict[str, Any]]:
    prompt = f"""
你是一名技术面试题库编辑。
请把下面的原始文档内容转成最多 {max_atoms} 条独立的面试题库原子。

只返回合法 JSON 数组，不要包 Markdown。

每条原子必须至少包含：
[
  {{
    "id": "stable-english-slug-id",
    "title": "展示标题",
    "subject": "稳定考察点标题",
    "domain": "{category}",
    "category": "{category}",
    "difficulty": "L2|L3|L4|L5",
    "questionRole": "opening|followup|mixed",
    "sourceRef": "{source_name}",
    "tags": ["tag1", "tag2"],
    "principles": ["要点1", "要点2"],
    "pitfalls": ["误区1", "误区2"],
    "followUpPaths": ["追问路径1", "追问路径2"],
    "status": "published"
  }}
]

忽略目录、页眉页脚、重复噪音和与技术面试题库无关的内容。
如果没有可提炼的有效内容，返回 []。

原始内容：
{text}
""".strip()
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": "只返回严格 JSON。"},
            {"role": "user", "content": prompt},
        ],
        "temperature": 0.2,
    }
    endpoint = base_url.rstrip("/") + "/chat/completions"
    request = urllib.request.Request(
        endpoint,
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            data = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="ignore")
        raise RuntimeError(f"Chat API 调用失败：HTTP {exc.code} {body}") from exc
    raw = data["choices"][0]["message"]["content"]
    return parse_atoms(raw)


def generate_atoms(args: argparse.Namespace, files: list[Path]) -> list[dict[str, Any]]:
    atoms: list[dict[str, Any]] = []
    json_files = [path for path in files if path.suffix.lower() in JSON_EXTS]
    doc_files = [path for path in files if path.suffix.lower() in DOC_EXTS]

    for path in json_files:
        atoms.extend(load_json_atoms(path))

    if doc_files:
        if not args.category:
            raise RuntimeError("文档输入模式下必须提供 --category。")
        if not args.api_key:
            raise RuntimeError("文档输入模式下必须通过 --api-key 或 OPENAI_API_KEY 提供模型密钥。")
        for path in doc_files:
            text = extract_text(path)
            if not text:
                continue
            for chunk in chunk_text(text, args.chunk_size, args.overlap):
                atoms.extend(
                    call_chat_api(
                        text=chunk,
                        category=args.category,
                        source_name=path.name,
                        base_url=args.base_url,
                        api_key=args.api_key,
                        model=args.model,
                        max_atoms=args.max_atoms_per_chunk,
                    )
                )
                time.sleep(args.pause_seconds)
    return atoms


def normalize_import_package(raw_atoms: list[dict[str, Any]], defaults: dict[str, str], *, batch_id: str | None = None) -> dict[str, Any]:
    items = [normalize_atom(item, defaults) for item in raw_atoms]
    items = dedupe_items(items)
    errors, warnings = validate_items(items)
    resolved_batch_id = batch_id or default_batch_id(defaults.get("category") or defaults.get("domain"))
    return {
        "batchId": resolved_batch_id,
        "items": items,
        "validationReport": {
            "tool": "scripts/interview_bank_import.py",
            "generatedAt": datetime.now(timezone.utc).isoformat(),
            "errors": errors,
            "warnings": warnings,
        },
        "reviewReport": {
            "atomCount": len(items),
            "domains": sorted({item["domain"] for item in items if item.get("domain")}),
            "categories": sorted({item["category"] for item in items if item.get("category")}),
            "statuses": sorted({item["status"] for item in items if item.get("status")}),
        },
    }


def normalize_atom(raw: dict[str, Any], defaults: dict[str, str]) -> dict[str, Any]:
    content = raw.get("content") if isinstance(raw.get("content"), dict) else {}
    category = text_value(raw, "category", default=defaults.get("category"))
    domain = text_value(raw, "domain", default=defaults.get("domain") or category)
    subject = text_value(raw, "subject")
    title = text_value(raw, "title", default=subject)
    source_ref = text_value(raw, "sourceRef", "source_ref", default=defaults.get("source_ref"))
    question_role = text_value(raw, "questionRole", "question_role", default=defaults.get("question_role") or "opening")
    status = text_value(raw, "status", default=defaults.get("status") or "published")
    difficulty = text_value(raw, "difficulty", default=defaults.get("difficulty") or "L3").upper()

    principles = normalize_string_list(raw.get("principles", content.get("principles")))
    pitfalls = normalize_string_list(raw.get("pitfalls", content.get("pitfalls")))
    follow_up_paths = normalize_string_list(raw.get("followUpPaths", raw.get("follow_up_paths", content.get("followUpPaths", content.get("follow_up_paths")))))

    normalized = {
        "id": text_value(raw, "id") or slugify(title or subject) or f"atom-{uuid.uuid4().hex[:8]}",
        "title": title,
        "subject": subject or title,
        "domain": domain,
        "category": category,
        "difficulty": difficulty,
        "questionRole": question_role,
        "sourceRef": source_ref,
        "tags": normalize_string_list(raw.get("tags")),
        "principles": principles,
        "pitfalls": pitfalls,
        "followUpPaths": follow_up_paths,
        "status": status,
    }
    return normalized


def dedupe_items(items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    seen: set[str] = set()
    output: list[dict[str, Any]] = []
    for item in items:
        base = item.get("id", "") or f"atom-{uuid.uuid4().hex[:8]}"
        candidate = base
        suffix = 2
        while candidate in seen:
            candidate = f"{base}-{suffix}"
            suffix += 1
        item["id"] = candidate
        seen.add(candidate)
        output.append(item)
    return output


def validate_items(items: list[dict[str, Any]]) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []
    for item in items:
        item_id = item.get("id", "<missing-id>")
        for field in ("id", "title", "subject", "domain", "category", "difficulty", "questionRole", "sourceRef", "status"):
            if not text_value(item, field):
                errors.append(f"{item_id}: {field} is required")
        if item.get("category") not in VALID_CATEGORIES:
            errors.append(f"{item_id}: category must be one of {sorted(VALID_CATEGORIES)}")
        if item.get("domain") not in VALID_CATEGORIES:
            errors.append(f"{item_id}: domain must be one of {sorted(VALID_CATEGORIES)}")
        if item.get("difficulty") not in VALID_DIFFICULTIES:
            errors.append(f"{item_id}: difficulty must be one of {sorted(VALID_DIFFICULTIES)}")
        if item.get("questionRole") not in VALID_QUESTION_ROLES:
            errors.append(f"{item_id}: questionRole must be one of {sorted(VALID_QUESTION_ROLES)}")
        if item.get("status") not in VALID_STATUSES:
            errors.append(f"{item_id}: status must be one of {sorted(VALID_STATUSES)}")
        for field in ("principles", "pitfalls", "followUpPaths"):
            values = item.get(field) if isinstance(item.get(field), list) else []
            if len(values) < 2:
                errors.append(f"{item_id}: {field} requires at least 2 items")
        if item.get("title") == item.get("subject"):
            warnings.append(f"{item_id}: title equals subject; this is allowed but may reduce display richness")
    return errors, warnings


def normalize_string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        parts = re.split(r"[\r\n;；]+", value)
        return unique_strings(parts)
    if isinstance(value, Iterable) and not isinstance(value, (bytes, bytearray, dict)):
        return unique_strings(str(item) for item in value)
    return unique_strings([str(value)])


def unique_strings(values: Iterable[str]) -> list[str]:
    seen: set[str] = set()
    result: list[str] = []
    for value in values:
        cleaned = str(value).strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    return result


def text_value(source: dict[str, Any], *keys: str, default: str | None = None) -> str:
    for key in keys:
        value = source.get(key)
        if value is None:
            continue
        cleaned = str(value).strip()
        if cleaned:
            return cleaned
    return (default or "").strip()


def slugify(value: str) -> str:
    text = value.strip().lower()
    text = re.sub(r"[^a-z0-9\u4e00-\u9fff]+", "-", text)
    text = re.sub(r"-+", "-", text).strip("-")
    return text[:80]


def default_batch_id(scope: str | None) -> str:
    scope_value = slugify(scope or "interview-bank") or "interview-bank"
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    suffix = uuid.uuid4().hex[:6]
    return f"qb-{scope_value}-{timestamp}-{suffix}"


def default_output_path(batch_id: str) -> Path:
    return Path("interview_bank_imports") / f"{batch_id}.json"


def main() -> int:
    args = parse_args()
    input_paths = [Path(item).expanduser().resolve() for item in args.input]
    files = collect_files(input_paths)
    if not files:
        print("没有找到支持的输入文件。", file=sys.stderr)
        return 1

    raw_atoms = generate_atoms(args, files)
    package = normalize_import_package(
        raw_atoms,
        {
            "source_ref": args.source_ref or ", ".join(str(path) for path in files),
            "domain": args.domain or args.category or "",
            "category": args.category or args.domain or "",
            "difficulty": args.difficulty or "L3",
            "question_role": args.question_role or "opening",
            "status": args.status or "published",
        },
        batch_id=args.batch_id,
    )
    output_path = Path(args.output).resolve() if args.output else default_output_path(package["batchId"]).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(package, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"wrote import package: {output_path}")
    print(f"items: {len(package['items'])}")
    if package["validationReport"]["errors"]:
        print("validation errors:")
        for error in package["validationReport"]["errors"]:
            print(f"  - {error}")
    return 0 if not package["validationReport"]["errors"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
