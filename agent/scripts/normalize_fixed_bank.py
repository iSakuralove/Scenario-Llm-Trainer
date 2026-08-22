"""将旧固定题快照导出为 scenario.v3 文件和迁移清单。"""

from __future__ import annotations

import argparse
from pathlib import Path

from hiddenworld.bank.v3_contract import export_fixed_v3_bank


def main() -> None:
    parser = argparse.ArgumentParser(description="export fixed HiddenWorld bank as scenario.v3")
    parser.add_argument("destination", type=Path, help="V3 artifact output directory")
    args = parser.parse_args()
    manifest = export_fixed_v3_bank(args.destination)
    print(f"exported {manifest['artifact_count']} scenario.v3 contracts to {args.destination}")


if __name__ == "__main__":
    main()
