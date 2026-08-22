"""把确定性状态编译成导师回复任务简报。

这个模块只负责整理回复生成所需的**公开事实边界**，不生成自然语言，也不
读取 ``HiddenWorld.root_cause``、``CanonicalAnswer`` 或 evidence/hypothesis id。
它是 ScenarioAgent 与 StateReducer 之间的一层只读适配：

* ``InvestigationView`` 是可以安全投影给 Agent/前端的调查状态；
* ``ResponseBrief`` 是给回复生成器的内部任务简报；
* 所有证据都以公开结果/线索文本存在，动作标识和证据 id 在入口处丢弃。

调用方可以逐步接入本模块。参数刻意接受当前项目中已有的 Pydantic 模型，
同时保留 ``required_concepts``、``public_clues`` 等显式参数，避免从答案模型
反推教学内容。
"""

from __future__ import annotations

from collections.abc import Iterable, Mapping, Sequence
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from hiddenworld.contracts import (
    ConceptDefinition,
    GuidanceState,
    HintStep,
    LearnerState,
    LearnerStateView,
    Observation,
    PrimaryTeachingTask,
    TeachingDecision,
    TurnAssessment,
)


class PublicObservationBrief(BaseModel):
    """已经可以展示给学生的观察摘要。

    ``action``、``evidence_id`` 和内部工具字段故意不存在；工具卡/事件投影
    负责展示事实，回复生成只需要知道事实本身及阴性结果。
    """

    model_config = ConfigDict(extra="forbid")

    result: str
    is_negative: bool = False


class InvestigationView(BaseModel):
    """调查状态的安全投影。

    这里的键和值都是学生可理解的标签或公开事实，不包含内部 id、答案正文、
    充分证据集、Guard/Proposal 信息。``concept_mastery`` 的键已经从 concept
    id 转成了概念标签。
    """

    model_config = ConfigDict(extra="forbid")

    current_focus: str = ""
    current_hypothesis_label: str | None = None
    ruled_out_labels: list[str] = Field(default_factory=list)
    discovered_clues: list[str] = Field(default_factory=list)
    pending_goals: list[str] = Field(default_factory=list)
    public_facts: list[str] = Field(default_factory=list)
    concept_mastery: dict[str, int] = Field(default_factory=dict)
    primary_task: PrimaryTeachingTask = "acknowledge_progress"
    missing_concepts: list[str] = Field(default_factory=list)
    hint_level: int = Field(default=0, ge=0, le=4)
    last_hint: str = ""


class CausalBoundary(BaseModel):
    """公开因果边界的结构化标签，不是标准答案。"""

    model_config = ConfigDict(extra="forbid")

    role: Literal["direct_trigger", "latent_issue", "causal_chain"]
    statement: str


class ResponseBrief(BaseModel):
    """回复生成器使用的结构化简报，不包含最终回复正文。"""

    model_config = ConfigDict(extra="forbid")

    primary_task: PrimaryTeachingTask
    investigation: InvestigationView
    explain_concepts: list[str] = Field(default_factory=list)
    known_concepts: list[str] = Field(default_factory=list)
    public_observations: list[PublicObservationBrief] = Field(default_factory=list)
    new_clues: list[str] = Field(default_factory=list)
    next_goals: list[str] = Field(default_factory=list)
    hint_level: int = Field(default=0, ge=0, le=4)
    hint_text: str = ""
    causal_boundaries: list[CausalBoundary] = Field(default_factory=list)
    do_not_repeat: list[str] = Field(default_factory=list)
    forbidden_topics: list[str] = Field(default_factory=list)

    # 迁移期便捷读取属性：不增加序列化字段，也不改变内部简报的唯一来源。
    @property
    def investigation_view(self) -> InvestigationView:
        return self.investigation

    @property
    def missing_concepts(self) -> list[str]:
        return list(self.explain_concepts)

    @property
    def allowed_observations(self) -> list[str]:
        return [item.result for item in self.public_observations]


class ResponseBriefBuilder:
    """从安全状态和公开观察构造 ``ResponseBrief``。

    该构造器没有 ``HiddenWorld`` 或 ``CanonicalAnswer`` 参数，故不能靠调用方
    误传完整题目世界而把答案带入简报。若需要加入触发因素/潜在问题边界，
    必须由上游显式传入已经公开的表述。
    """

    _KNOWN_MASTERY = 2

    def build(
        self,
        learner_state: LearnerState | LearnerStateView | None = None,
        *,
        learner: LearnerState | LearnerStateView | None = None,
        guidance_state: GuidanceState | None = None,
        turn_assessment: TurnAssessment | None = None,
        assessment: TurnAssessment | None = None,
        teaching_decision: TeachingDecision | None = None,
        concept_catalog: Sequence[ConceptDefinition | Mapping[str, Any]] = (),
        required_concepts: Sequence[str] = (),
        observations: Sequence[Observation | PublicObservationBrief | Mapping[str, Any] | str] = (),
        public_observations: Sequence[Observation | PublicObservationBrief | Mapping[str, Any] | str] = (),
        public_clues: Sequence[str] = (),
        discovered_clues: Sequence[str] = (),
        next_goals: Sequence[str] = (),
        pending_goals: Sequence[str] = (),
        public_facts: Sequence[str] = (),
        hypothesis_labels: Mapping[str, str] | None = None,
        current_hypothesis_label: str = "",
        ruled_out_labels: Sequence[str] = (),
        hint_steps: Sequence[HintStep | Mapping[str, Any]] = (),
        hint_text: str = "",
        direct_trigger: str = "",
        latent_issue: str = "",
        causal_chain: Sequence[str] = (),
        causal_boundaries: Sequence[CausalBoundary | Mapping[str, Any]] = (),
    ) -> ResponseBrief:
        """构造一份回复任务简报。

        ``learner_state`` 支持位置参数是为了方便在 StateReducer/Runtime 迁移期
        接入；其余输入均为关键字，减少跨层调用把内部对象错位传入的风险。
        """

        state = learner_state or learner or LearnerStateView()
        hypothesis_labels = hypothesis_labels or {}
        current_assessment = turn_assessment or assessment
        catalog = _concept_catalog(concept_catalog)
        mastery = _public_mastery(state, catalog)
        required = _resolve_required_concepts(
            required_concepts,
            catalog,
            current_assessment,
        )
        known = [label for label in required if _mastery_for(label, state, catalog) >= self._KNOWN_MASTERY]
        missing = [label for label in required if label not in known]

        selected_task = _select_primary_task(
            current_assessment,
            teaching_decision,
            guidance_state,
            state,
            has_observations=bool(observations or public_observations),
            has_new_clues=bool(public_clues or discovered_clues),
            has_missing_concepts=bool(missing),
        )

        obs = _public_observation_list(observations or public_observations)
        clues = _unique_texts((*public_clues, *discovered_clues))
        goals = _unique_texts((*next_goals, *pending_goals))
        facts = _unique_texts(public_facts)
        level, resolved_hint = _resolve_hint(
            state,
            current_assessment,
            hint_steps,
            hint_text,
        )
        boundaries = _resolve_boundaries(
            causal_boundaries,
            direct_trigger=direct_trigger,
            latent_issue=latent_issue,
            causal_chain=causal_chain,
        )

        view = InvestigationView(
            current_focus=_get_text(guidance_state, "current_focus") or _get_text(state, "current_focus"),
            current_hypothesis_label=(
                current_hypothesis_label.strip()
                or _get_text(state, "current_hypothesis_label")
                or hypothesis_labels.get(_get_text(state, "current_hypothesis"), "")
                or None
            ),
            ruled_out_labels=_unique_texts(
                (
                    *ruled_out_labels,
                    *_texts(getattr(state, "ruled_out_labels", ())),
                    *(
                        hypothesis_labels.get(str(item), "")
                        for item in getattr(state, "ruled_out_hypotheses", ())
                    ),
                )
            ),
            discovered_clues=clues,
            pending_goals=goals,
            public_facts=facts,
            concept_mastery=mastery,
            primary_task=selected_task,
            missing_concepts=missing,
            hint_level=level,
            last_hint=resolved_hint,
        )

        do_not_repeat = _unique_texts((*known, *(item.result for item in obs)))
        forbidden = [
            "未公开的证据、答案正文或内部 ID",
            "把假设说成已确认事实",
            "把提示当作学生独立发现的证据",
        ]
        return ResponseBrief(
            primary_task=selected_task,
            investigation=view,
            explain_concepts=missing,
            known_concepts=known,
            public_observations=obs,
            new_clues=clues,
            next_goals=goals,
            hint_level=level,
            hint_text=resolved_hint,
            causal_boundaries=boundaries,
            do_not_repeat=do_not_repeat,
            forbidden_topics=forbidden,
        )


def _concept_catalog(items: Sequence[ConceptDefinition | Mapping[str, Any]]) -> dict[str, tuple[str, set[str]]]:
    result: dict[str, tuple[str, set[str]]] = {}
    for raw in items:
        if isinstance(raw, ConceptDefinition):
            concept_id, label, aliases = raw.concept_id, raw.label, raw.aliases
        elif isinstance(raw, Mapping):
            concept_id = str(raw.get("concept_id", "")).strip()
            label = str(raw.get("label", "")).strip()
            aliases = [str(item).strip() for item in raw.get("aliases", ())]
        else:
            continue
        if concept_id and label:
            result[concept_id] = (label, {label.casefold(), concept_id.casefold(), *(item.casefold() for item in aliases if item)})
    return result


def _public_mastery(state: Any, catalog: Mapping[str, tuple[str, set[str]]]) -> dict[str, int]:
    raw = getattr(state, "concept_mastery", {}) or {}
    result: dict[str, int] = {}
    for key, value in raw.items():
        text = str(key)
        label = catalog.get(text, ("", set()))[0]
        if not label and text in {item[0] for item in catalog.values()}:
            label = text
        if label:
            result[label] = max(0, min(4, int(value)))
    return result


def _mastery_for(label: str, state: Any, catalog: Mapping[str, tuple[str, set[str]]]) -> int:
    raw = getattr(state, "concept_mastery", {}) or {}
    for key, value in raw.items():
        if str(key) == label:
            return int(value)
        if str(key) in catalog and catalog[str(key)][0] == label:
            return int(value)
    return 0


def _resolve_required_concepts(
    required: Sequence[str],
    catalog: Mapping[str, tuple[str, set[str]]],
    assessment: TurnAssessment | None,
) -> list[str]:
    candidates = list(required)
    if not candidates and assessment is not None:
        context = " ".join(
            item
            for item in (
                assessment.user_goal,
                assessment.requested_action,
                assessment.clarification_target,
                assessment.hypothesis_raw,
            )
            if item
        ).casefold()
        if context:
            for label, aliases in catalog.values():
                if any(alias and alias in context for alias in aliases):
                    candidates.append(label)
    result: list[str] = []
    for item in candidates:
        text = str(item).strip()
        if not text:
            continue
        if text in catalog:
            text = catalog[text][0]
        if text.casefold() not in {value.casefold() for value in result}:
            result.append(text)
    return result


def _select_primary_task(
    assessment: TurnAssessment | None,
    decision: TeachingDecision | None,
    guidance: GuidanceState | None,
    state: Any,
    *,
    has_observations: bool,
    has_new_clues: bool,
    has_missing_concepts: bool,
) -> PrimaryTeachingTask:
    if decision is not None:
        return decision.primary_task
    if guidance is not None and guidance.teaching_state == "debrief":
        return "close_investigation"
    if has_missing_concepts:
        return "explain_concept"
    if assessment is not None:
        if assessment.intent in {"clarification", "explanation_request"}:
            return "explain_concept"
        if assessment.random_investigation:
            return "redirect_investigation"
        if assessment.contains_answer_attempt or assessment.claim_type == "answer":
            return "correct_conclusion"
        if assessment.is_stuck or getattr(state, "stalled_turns", 0) > 0:
            return "release_hint"
        if has_observations:
            return "interpret_evidence"
        if assessment.progress_assessment == "progress" or has_new_clues:
            return "acknowledge_progress"
    if has_observations:
        return "interpret_evidence"
    if has_new_clues:
        return "acknowledge_progress"
    return "acknowledge_progress"


def _resolve_hint(
    state: Any,
    assessment: TurnAssessment | None,
    hint_steps: Sequence[HintStep | Mapping[str, Any]],
    explicit_text: str,
) -> tuple[int, str]:
    stalled = int(getattr(state, "stalled_turns", 0) or 0)
    current = int(getattr(state, "hint_level", 0) or 0)
    progress = assessment is not None and assessment.progress_assessment == "progress"
    level = 0 if progress else min(4, max(stalled, current))
    if level <= 0:
        return 0, ""
    text = explicit_text.strip()
    if not text:
        for raw in hint_steps:
            step_level = int(raw.level if isinstance(raw, HintStep) else raw.get("level", 0))
            if step_level == level:
                text = str(raw.public_hint if isinstance(raw, HintStep) else raw.get("public_hint", "")).strip()
                break
    return level, text


def _resolve_boundaries(
    items: Sequence[CausalBoundary | Mapping[str, Any]],
    *,
    direct_trigger: str,
    latent_issue: str,
    causal_chain: Sequence[str],
) -> list[CausalBoundary]:
    result: list[CausalBoundary] = []
    for raw in items:
        if isinstance(raw, CausalBoundary):
            item = raw
        elif isinstance(raw, Mapping):
            role = str(raw.get("role", ""))
            statement = str(raw.get("statement", "")).strip()
            if role not in {"direct_trigger", "latent_issue", "causal_chain"} or not statement:
                continue
            item = CausalBoundary(role=role, statement=statement)  # type: ignore[arg-type]
        else:
            continue
        if item.statement.strip():
            result.append(item)
    if direct_trigger.strip():
        result.append(CausalBoundary(role="direct_trigger", statement=direct_trigger.strip()))
    if latent_issue.strip():
        result.append(CausalBoundary(role="latent_issue", statement=latent_issue.strip()))
    for statement in causal_chain:
        if str(statement).strip():
            result.append(CausalBoundary(role="causal_chain", statement=str(statement).strip()))
    seen: set[tuple[str, str]] = set()
    return [item for item in result if not ((item.role, item.statement) in seen or seen.add((item.role, item.statement)))]


def _public_observation_list(items: Sequence[Observation | PublicObservationBrief | Mapping[str, Any] | str]) -> list[PublicObservationBrief]:
    result: list[PublicObservationBrief] = []
    seen: set[tuple[str, bool]] = set()
    for raw in items:
        if isinstance(raw, PublicObservationBrief):
            item = raw
        elif isinstance(raw, Observation):
            item = PublicObservationBrief(result=raw.result, is_negative=raw.is_negative)
        elif isinstance(raw, Mapping):
            result_text = str(raw.get("result", raw.get("content", ""))).strip()
            if not result_text:
                continue
            item = PublicObservationBrief(result=result_text, is_negative=bool(raw.get("is_negative", False)))
        else:
            text = str(raw).strip()
            if not text:
                continue
            item = PublicObservationBrief(result=text)
        key = (item.result, item.is_negative)
        if key not in seen:
            seen.add(key)
            result.append(item)
    return result


def _texts(items: Iterable[Any]) -> list[str]:
    return _unique_texts(str(item) for item in items if str(item).strip())


def _unique_texts(items: Iterable[Any]) -> list[str]:
    result: list[str] = []
    seen: set[str] = set()
    for raw in items:
        text = str(raw).strip()
        if text and text.casefold() not in seen:
            seen.add(text.casefold())
            result.append(text)
    return result


def _get_text(obj: Any, name: str) -> str:
    value = getattr(obj, name, "") if obj is not None else ""
    return str(value).strip() if value is not None else ""


__all__ = [
    "CausalBoundary",
    "InvestigationView",
    "PublicObservationBrief",
    "ResponseBrief",
    "ResponseBriefBuilder",
]
