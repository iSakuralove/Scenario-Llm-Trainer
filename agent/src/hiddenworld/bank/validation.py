"""题目三层校验：结构、图关系、教学可完成性。

第三层是最有价值的一层，也是最容易被漏掉的一层：前两层都通过、图也自洽，
题目仍可能**根本做不出来**——某条充分证据集里有一条证据，它的前置链条上
存在一个没有任何 observation 能产出的节点。学生会一直卡在那里，而系统
永远认为他"证据还不够"。

这类题在人工出题时靠 review 兜，在 LLM 生成时必须靠代码兜。
"""

from __future__ import annotations

from dataclasses import dataclass, field

from ..contracts import HiddenWorld, PublicScenario

# 006 定下的固定测试题最小规模。用户生成题不强制这一档，
# 但图关系与教学可完成性两层对所有题目一视同仁。
MIN_HYPOTHESES = 4
MIN_EVIDENCE_NODES = 6
MIN_SUFFICIENT_SETS = 2
MIN_NEGATIVE_OBSERVATIONS = 1
MIN_MISCONCEPTIONS = 1

HYPOTHESIS_OTHER = "H_OTHER"


class ValidationError(Exception):
    """题目校验失败。携带结构化错误列表，直接回给生成接口。"""

    def __init__(self, report: ValidationReport) -> None:
        super().__init__("; ".join(report.errors))
        self.report = report


@dataclass
class ValidationReport:
    layer: str = ""
    errors: list[str] = field(default_factory=list)
    reachable_evidence: set[str] = field(default_factory=set)
    completable_sets: list[int] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.errors


def validate_question(
    public_scenario: PublicScenario,
    world: HiddenWorld,
    *,
    require_fixed_bank_scale: bool = False,
) -> ValidationReport:
    """跑完三层校验。任一层失败即返回，不继续往下跑。

    分层短路是有意的：图关系错了再去算可完成性只会产出一堆派生错误，
    把真正的根因淹掉。
    """
    report = _validate_structure(
        public_scenario, world, require_fixed_bank_scale=require_fixed_bank_scale
    )
    if not report.ok:
        return report

    report = _validate_graph(world)
    if not report.ok:
        return report

    return _validate_completability(world)


def _validate_structure(
    public_scenario: PublicScenario,
    world: HiddenWorld,
    *,
    require_fixed_bank_scale: bool,
) -> ValidationReport:
    report = ValidationReport(layer="structure")

    if not public_scenario.title.strip():
        report.errors.append("public_scenario.title 不能为空")
    if not public_scenario.description.strip():
        report.errors.append("public_scenario.description 不能为空")

    if not world.root_cause.description.strip():
        report.errors.append("root_cause.description 不能为空")
    if not world.root_cause.accepted_hypotheses:
        report.errors.append("root_cause.accepted_hypotheses 不能为空")
    if not world.root_cause.sufficient_evidence_sets:
        report.errors.append("root_cause.sufficient_evidence_sets 不能为空")

    ids = [h.hypothesis_id for h in world.hypotheses]
    if HYPOTHESIS_OTHER not in ids:
        report.errors.append(
            f"假设表必须包含 {HYPOTHESIS_OTHER}：真实学生一定会提出出题人没想到的东西，"
            "强行往已有 ID 上靠会让后续全错"
        )
    if len(ids) != len(set(ids)):
        report.errors.append("hypothesis_id 存在重复")

    evidence_ids = [e.evidence_id for e in world.evidence_graph]
    if len(evidence_ids) != len(set(evidence_ids)):
        report.errors.append("evidence_id 存在重复")

    actions = [o.action for o in world.observations]
    if len(actions) != len(set(actions)):
        report.errors.append("observation.action 存在重复，同一动作只能有一条响应")

    if not require_fixed_bank_scale:
        return report

    if len(world.hypotheses) < MIN_HYPOTHESES:
        report.errors.append(f"固定题至少需要 {MIN_HYPOTHESES} 个假设，当前 {len(world.hypotheses)}")
    if len(world.evidence_graph) < MIN_EVIDENCE_NODES:
        report.errors.append(
            f"固定题至少需要 {MIN_EVIDENCE_NODES} 个证据节点，当前 {len(world.evidence_graph)}"
        )
    if len(world.root_cause.sufficient_evidence_sets) < MIN_SUFFICIENT_SETS:
        report.errors.append(
            f"固定题至少需要 {MIN_SUFFICIENT_SETS} 条充分证据路径，"
            f"当前 {len(world.root_cause.sufficient_evidence_sets)}"
        )
    negatives = [o for o in world.observations if o.is_negative]
    if len(negatives) < MIN_NEGATIVE_OBSERVATIONS:
        report.errors.append(
            f"固定题至少需要 {MIN_NEGATIVE_OBSERVATIONS} 个阴性观察——"
            "排除法是排障的核心能力，没有阴性观察就没法度量它"
        )
    if len(world.misconception_rules) < MIN_MISCONCEPTIONS:
        report.errors.append(f"固定题至少需要 {MIN_MISCONCEPTIONS} 个误解方向")

    return report


def _validate_graph(world: HiddenWorld) -> ValidationReport:
    report = ValidationReport(layer="graph")
    evidence_ids = {e.evidence_id for e in world.evidence_graph}
    hypothesis_ids = world.hypothesis_ids()

    for node in world.evidence_graph:
        for prereq in node.prerequisites:
            if prereq not in evidence_ids:
                report.errors.append(f"证据 {node.evidence_id} 的前置 {prereq} 不存在")
            if prereq == node.evidence_id:
                report.errors.append(f"证据 {node.evidence_id} 把自己列为前置")

    for observation in world.observations:
        for produced in observation.yields_evidence:
            if produced not in evidence_ids:
                report.errors.append(f"观察 {observation.action} 产出的证据 {produced} 不存在")
        for ruled in observation.rules_out:
            if ruled not in hypothesis_ids:
                report.errors.append(f"观察 {observation.action} 排除的假设 {ruled} 不存在")

    for accepted in world.root_cause.accepted_hypotheses:
        if accepted not in hypothesis_ids:
            report.errors.append(f"accepted_hypotheses 中的 {accepted} 不在假设表里")
        if accepted == HYPOTHESIS_OTHER:
            report.errors.append(
                f"{HYPOTHESIS_OTHER} 不能作为正确答案：它表示「候选表之外」，"
                "把它设成 target 会让 Verifier 对任何未知说法都返回命中"
            )

    for index, evidence_set in enumerate(world.root_cause.sufficient_evidence_sets):
        if not evidence_set:
            report.errors.append(f"第 {index} 条充分证据集为空")
        for evidence_id in evidence_set:
            if evidence_id not in evidence_ids:
                report.errors.append(f"第 {index} 条充分证据集引用了不存在的证据 {evidence_id}")

    for rule in world.misconception_rules:
        for hypothesis_id in rule.pattern_hypotheses:
            if hypothesis_id not in hypothesis_ids:
                report.errors.append(f"误解规则 {rule.misconception_id} 引用了不存在的假设 {hypothesis_id}")

    if _has_prerequisite_cycle(world):
        report.errors.append("证据前置关系存在环，学生永远无法进入这个环")

    return report


def _has_prerequisite_cycle(world: HiddenWorld) -> bool:
    """DFS 三色标记查环。"""
    graph = {node.evidence_id: list(node.prerequisites) for node in world.evidence_graph}
    WHITE, GREY, BLACK = 0, 1, 2
    color = dict.fromkeys(graph, WHITE)

    def visit(node_id: str) -> bool:
        if color.get(node_id, BLACK) == GREY:
            return True
        if color.get(node_id, BLACK) != WHITE:
            return False
        color[node_id] = GREY
        for prereq in graph.get(node_id, []):
            if visit(prereq):
                return True
        color[node_id] = BLACK
        return False

    return any(visit(node_id) for node_id in list(graph))


def _validate_completability(world: HiddenWorld) -> ValidationReport:
    """教学可完成性：至少有一条充分证据集是学生真的能走完的。

    做法是从空手开始跑不动点：反复扫描所有观察，只要某条观察产出的证据
    其前置都已到手，这条观察就可执行，把它的产出收进来；直到没有新证据为止。

    这一层挡住的是"看起来自洽、实际做不出来"的题——比如某条充分证据集里
    有一条证据，它的前置链上有一个节点没有任何观察能产出。
    """
    report = ValidationReport(layer="completability")
    node_by_id = {node.evidence_id: node for node in world.evidence_graph}

    obtained: set[str] = set()
    while True:
        grew = False
        for observation in world.observations:
            for evidence_id in observation.yields_evidence:
                if evidence_id in obtained:
                    continue
                node = node_by_id.get(evidence_id)
                if node is None:
                    continue
                if all(prereq in obtained for prereq in node.prerequisites):
                    obtained.add(evidence_id)
                    grew = True
        if not grew:
            break

    report.reachable_evidence = obtained

    unreachable = {node.evidence_id for node in world.evidence_graph} - obtained
    if unreachable:
        report.errors.append(
            f"以下证据没有任何可执行的观察能产出，学生永远拿不到：{sorted(unreachable)}"
        )

    for index, evidence_set in enumerate(world.root_cause.sufficient_evidence_sets):
        if set(evidence_set) <= obtained:
            report.completable_sets.append(index)

    if not report.completable_sets:
        report.errors.append(
            "没有任何一条充分证据集是可达的——这道题做不出来，"
            "学生会一直被判「证据还不够」却找不到路"
        )

    for hypothesis in world.hypotheses:
        if hypothesis.hypothesis_id == HYPOTHESIS_OTHER:
            continue
        if hypothesis.hypothesis_id in world.root_cause.accepted_hypotheses:
            continue
        ruled_out_somewhere = any(
            hypothesis.hypothesis_id in observation.rules_out for observation in world.observations
        )
        if not ruled_out_somewhere:
            report.errors.append(
                f"干扰假设 {hypothesis.hypothesis_id} 没有任何观察能排除它——"
                "排除必须是学生动作的结果，不能靠系统替他划掉"
            )

    return report
