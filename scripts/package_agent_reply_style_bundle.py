from __future__ import annotations

import argparse
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile


FILES_TO_PACKAGE = [
    "backend/internal/agent/diagnostic.go",
    "backend/internal/agent/diagnostic_test.go",
    "backend/internal/agent/semantic_gate.go",
    "backend/internal/agent/semantic_gate_test.go",
    "backend/internal/ai/openai_provider.go",
    "backend/internal/ai/provider.go",
    "backend/internal/ai/prompts/scenario_reply.tmpl",
    "流程/102.排查工坊Agent默认不释线索决策升级.md",
]


def package_bundle(repo_root: Path, output_zip: Path) -> list[str]:
    output_zip.parent.mkdir(parents=True, exist_ok=True)
    packed: list[str] = []

    with ZipFile(output_zip, "w", compression=ZIP_DEFLATED) as zf:
        for rel in FILES_TO_PACKAGE:
            src = repo_root / rel
            if not src.exists():
                continue
            zf.write(src, arcname=rel)
            packed.append(rel)

        manifest = "\n".join(packed) + ("\n" if packed else "")
        zf.writestr("MANIFEST.txt", manifest)

    return packed


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Package the main files for the agent reply style changes."
    )
    parser.add_argument(
        "--repo-root",
        default=".",
        help="Repository root path. Defaults to current directory.",
    )
    parser.add_argument(
        "--output",
        default="tmp/agent-reply-style-bundle.zip",
        help="Output zip path relative to repo root or absolute path.",
    )
    args = parser.parse_args()

    repo_root = Path(args.repo_root).resolve()
    output_zip = Path(args.output)
    if not output_zip.is_absolute():
        output_zip = (repo_root / output_zip).resolve()

    packed = package_bundle(repo_root, output_zip)

    print(output_zip)
    for rel in packed:
        print(rel)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
