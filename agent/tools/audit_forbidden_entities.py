"""诊断脚本：把固定题库每道题的 Guard 禁词表打出来。

关注点是 kernel/guard.py 的 _sensitive_tokens 会把根因与未释放证据里的**所有数字**
纳入禁词。如果表里出现无上下文的小整数，Mentor 就再也不能写「3 个方向」这类
正常表述，会触发 entity_leak 重试并退化成更空泛的回复。
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from hiddenworld.contracts import HiddenWorld, PublicScenario  # noqa: E402
from hiddenworld.kernel.guard import extract_forbidden_entities  # noqa: E402

SMALL_INTEGER = re.compile(r"^\d{1,2}$")

bank = sorted((Path(__file__).resolve().parents[1] / "src/hiddenworld/bank/fixed").glob("*.json"))
findings = []
for path in bank:
    raw = json.loads(path.read_text(encoding="utf-8"))
    world = HiddenWorld.model_validate(raw["hidden_world"])
    public = PublicScenario.model_validate(raw["public_scenario"])
    entities = extract_forbidden_entities(world, released_evidence_ids=[], public_scenario=public)
    small = [item for item in entities if SMALL_INTEGER.match(item)]
    findings.append((path.stem, len(entities), small))
    print(f"{path.stem}: {len(entities)} entities")
    print(f"  small integers: {small}")
    print(f"  all: {entities}")
    print()

flagged = [name for name, _, small in findings if small]
print(f"题目数 {len(findings)}；出现无上下文小整数的题目：{flagged or '无'}")
