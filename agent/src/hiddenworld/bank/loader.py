"""固定题库加载。

固定题以 JSON 落盘而不是写在 Python 里：它们是**内容**，会随出题迭代变化，
而代码不该因为改一句题面就产生 diff。JSON 也让同一份内容可以直接喂给 Go
的种子流程，两边不会各写一份而逐渐漂移。
"""

from __future__ import annotations

import json
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field

from ..contracts import CONTRACT_VERSION, HiddenWorld, PublicScenario
from .validation import ValidationError, validate_question

# 四道固定题的稳定 ID。通过四次**独立的单题生成调用**创建，
# 不新增批量生成入口——批量入口会让"一次请求生成一道题"的正式语义悄悄消失。
FIXED_BANK_IDS = (
    "hw-db-index-001",
    "hw-network-vip-001",
    "hw-k8s-io-001",
    "hw-cache-key-001",
)

_BANK_DIR = Path(__file__).parent / "fixed"


class FixedQuestion(BaseModel):
    """一道固定题。外层是目录字段，内层是 public_scenario + hidden_world。

    前端训练接口只拿得到目录字段和 public_scenario；hidden_world 只供服务端
    运行时组件和受权限控制的管理接口读取，学生接口不能复用管理端完整详情。
    """

    model_config = ConfigDict(extra="forbid")

    question_id: str
    domain: str
    difficulty: str
    scenario_type: str
    tags: list[str] = Field(default_factory=list)
    source: str = "fixed_hiddenworld"
    version: int = 1
    status: str = "active"
    model_version: str = CONTRACT_VERSION
    public_scenario: PublicScenario
    hidden_world: HiddenWorld

    def public_payload(self) -> dict:
        """学生可见的投影。刻意不含 hidden_world。"""
        return {
            "question_id": self.question_id,
            "domain": self.domain,
            "difficulty": self.difficulty,
            "scenario_type": self.scenario_type,
            "tags": list(self.tags),
            "source": self.source,
            "version": self.version,
            "status": self.status,
            "model_version": self.model_version,
            "public_scenario": self.public_scenario.model_dump(),
        }


def load_fixed_question(question_id: str, *, validate: bool = True) -> FixedQuestion:
    """读取一道固定题。默认跑完三层校验。

    校验默认开启而不是默认关闭：一道结构不完整的题如果能被静默加载，
    它会一路走到学生面前才暴露。
    """
    path = _BANK_DIR / f"{question_id}.json"
    if not path.is_file():
        raise FileNotFoundError(f"固定题 {question_id} 不存在：{path}")

    question = FixedQuestion.model_validate_json(path.read_text(encoding="utf-8"))

    if question.model_version != CONTRACT_VERSION:
        raise ValueError(
            f"固定题 {question_id} 的 model_version 是 {question.model_version!r}，"
            f"当前契约是 {CONTRACT_VERSION!r}"
        )

    if validate:
        report = validate_question(
            question.public_scenario,
            question.hidden_world,
            require_fixed_bank_scale=True,
        )
        if not report.ok:
            raise ValidationError(report)

    return question


def list_fixed_questions(*, validate: bool = True) -> list[FixedQuestion]:
    """加载全部已落盘的固定题。缺哪道就跳过哪道，便于分批落地。"""
    questions: list[FixedQuestion] = []
    for question_id in FIXED_BANK_IDS:
        if (_BANK_DIR / f"{question_id}.json").is_file():
            questions.append(load_fixed_question(question_id, validate=validate))
    return questions


def export_for_seed(question: FixedQuestion) -> str:
    """导出给 Go 种子流程消费的 JSON。"""
    return json.dumps(question.model_dump(), ensure_ascii=False, indent=2)
