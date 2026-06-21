# 面试舱 Launchpad 接口接入 PRD

## 目标

根据既有决策文档，把面试舱用户侧启动台从“纯前端静态配置”推进到“后端开放组合驱动”。本阶段只做用户侧 Launchpad 聚合接口和前端消费适配，不展开完整题库治理后台。

## 用户价值

- 学生只看到当前系统认为可启动的训练轨道，避免前端展示不可用组合。
- 后续题库治理、最低题量门槛和索引状态接入后，不需要再次重写启动台页面。
- 接口失败或后端能力未完全接入时，前端仍可使用现有静态轨道兜底，保证比赛演示稳定。

## 已确认事实

- 决策文档要求首期用户侧只展示后端返回的可用组合，未开放组合不显示。
- 首期开放组合优先为 `java/database/cache/ai_llm` 的 `L2/L3`。
- 索引失败不应阻断开场题普通筛选；追问增强失败应走规则回退。
- 当前 `InterviewsPage.tsx` 仍直接消费 `launchpadConfig.ts` 的 `interviewLaunchTracks`。
- 当前创建面试会话接口仍要求 `domain / difficulty / question_type`，并从旧 `InterviewQuestion` 查题。
- 本阶段不要求完整接入 `InterviewKnowledgeAtom` 管理端、导入、版本历史和动态 RAG 追问。

## 范围

### 本阶段必须做

- 新增用户侧 `GET /interviews/launchpad` 接口。
- 接口返回面试启动台摘要、开放轨道、能力域和降级状态。
- 后端首期可基于现有种子题和已确认首期组合计算开放轨道，作为未来题库状态计算的兼容层。
- 前端新增 API 调用，优先使用后端返回的轨道数据。
- 前端保留 `launchpadConfig.ts` 作为接口失败或返回空列表时的兜底。
- 前端展示接口加载、接口失败兜底和空状态提示，不把索引异常表现为整场不可用。
- 启动面试仍使用选中轨道的 `domain / difficulty / question_type` 调用现有创建会话接口。

### 本阶段不做

- 不实现完整题库导入、预览、发布、归档、恢复和版本历史。
- 不把创建会话主链路切到新题库表。
- 不实现动态 RAG 追问检索。
- 不新增用户自定义模型配置、Qdrant 或第二套运行时。
- 不在普通用户侧展示 `sourceRef`、版本号、审计字段或内部检索上下文。

## 接口需求

`GET /interviews/launchpad` 返回建议结构：

```json
{
  "summary": {
    "open_track_count": 8,
    "published_atom_count": 0,
    "indexed_atom_count": 0,
    "fallback_mode": true,
    "message": "当前使用兼容题库轨道，后续将由题库治理状态驱动。"
  },
  "domains": [],
  "open_tracks": [],
  "coverage": {
    "domains": [],
    "difficulties": [],
    "question_types": []
  },
  "fallback_mode": true
}
```

字段命名使用后端 JSON 风格，前端适配为现有 `InterviewLaunchTrack` 视图模型。

## 前端需求

- 页面初始进入时读取 `api.interviewLaunchpad(token)`。
- 读取成功且 `open_tracks` 非空时，使用接口轨道。
- 读取失败或 `open_tracks` 为空时，使用 `interviewLaunchTracks` 兜底，并展示温和提示。
- 键盘选择、开始面试、历史面试、题目一致性校验继续可用。
- 选中轨道变化后，首屏配置、轨道卡片和领域 chip 同步更新。

## 验收标准

- 访问面试舱时会请求 `GET /interviews/launchpad`。
- 后端接口返回的轨道能驱动前端轨道列表和开始面试参数。
- 关闭或破坏 Launchpad 接口时，页面仍能显示现有 8 个首期轨道，并提示使用兼容配置。
- 前端不再把 `interviewLaunchTracks` 当作唯一事实来源。
- `npm --prefix frontend run build` 通过。
- `npm --prefix frontend run lint` 通过。
- 后端相关测试或构建命令通过。

## 开放问题

无。当前阶段范围已由既有决策文档确定。
