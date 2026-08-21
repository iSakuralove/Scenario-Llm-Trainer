"""题目定义、Runtime 白名单过滤的安全教学维度。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict


TeachingDimensionCategory = Literal[
    "evidence",
    "causal",
    "temporal",
    "dependency",
    "capacity",
    "configuration",
    "verification",
    "resource",
    "data",
]
TeachingDimensionStatus = Literal["unexplored", "in_progress", "covered"]
HintLevel = Literal["none", "light", "direct"]


class TeachingDimensionRef(BaseModel):
    """不包含答案关键词的教学导航引用。"""

    model_config = ConfigDict(extra="forbid")

    dimension_id: str
    category: TeachingDimensionCategory
    status: TeachingDimensionStatus = "unexplored"
    hint_level: HintLevel = "none"

