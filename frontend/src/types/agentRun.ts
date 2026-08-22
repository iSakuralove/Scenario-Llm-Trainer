// 排查工坊运行事件契约。
//
// v1（无 schema_version）：旧落库 trace 的只读兼容形状，经 LegacyEventAdapter
// 适配进统一 ViewModel；v1 会话不回填、不重写。
// v2（schema_version = "hiddenworld.v2"）：Go 独占生成的正式判别联合，
// sequence / state_revision / schema_version 三者职责分离，前端只消费
// PublicContent.markdown_ready 与 assistant_delta 的 markdown_ready_delta。

export type ScenarioRunEventStatus = 'started' | 'running' | 'completed' | 'failed'

export type ScenarioRunEventKind =
  | 'user_message'
  | 'reasoning_summary_delta'
  | 'reasoning_summary_completed'
  | 'observation_result'
  | 'tool_started'
  | 'tool_result'
  | 'tool_completed'
  | 'response_summary'
  | 'mentor_buffered'
  | 'guard_passed'
  | 'proposal_approved'
  | 'reply_delta'
  | 'turn_completed'
  | 'turn_failed'

export interface ScenarioPublicReasoningSummary {
  stage: 'understanding_message' | 'checking_observations' | 'verifying_answer' | 'composing_reply'
  text: string
}

// 测试专用调试事件。它不属于 ScenarioRunEvent，不参与 sequence、落库或历史回放；
// 正式环境不会从 API 收到该事件。
export interface ScenarioDebugTraceEvent {
  kind: 'reasoning_raw_delta'
  text: string
}

export interface ScenarioPublicObservation {
  action: string
  result: string
  is_negative: boolean
}

/**
 * Python V2 的公开答案对比投影。
 *
 * 这五个字段是跨语言契约的判定维度；它们在新事件中始终存在，不能
 * 用旧 v1 的 support_status/next_action 替代。旧事件由下方的
 * ScenarioLegacyPublicAnswerComparison 只读适配。
 */
export interface ScenarioPublicAnswerComparison {
  tool: 'compare_answer' | string
  status: string
  user_points: string[]
  conclusion_status: 'none' | 'partial' | 'supported' | 'contradictory'
  evidence_status: 'none' | 'insufficient' | 'partial' | 'sufficient'
  causal_status: 'missing' | 'partial' | 'sufficient'
  missing_dimensions: Array<'conclusion' | 'evidence' | 'causal_link' | 'consistency'>
  contradictions: string[]
}

/** 存量 v1 事件的只读形状；新写入不得使用这两个字段。 */
export interface ScenarioLegacyPublicAnswerComparison {
  tool: 'compare_answer' | string
  status: string
  user_points: string[]
  support_status: 'insufficiently_specific' | 'needs_more_evidence' | 'has_evidence_conflict' | 'evidence_consistent'
  next_action: string
}

/** SSE/历史适配层同时接受新 V2 与存量 V1，业务组件只生成 V2。 */
export type ScenarioPublicAnswerComparisonPayload =
  | ScenarioPublicAnswerComparison
  | ScenarioLegacyPublicAnswerComparison

export interface ScenarioToolEventPayload {
  name: string
  redacted_arguments: Record<string, string>
  duration_ms: number
  result?: ScenarioPublicAnswerComparisonPayload
}

export interface ScenarioRunEvent {
  request_id: string
  sequence: number
  kind: ScenarioRunEventKind
  status: ScenarioRunEventStatus
  text?: string
  summary?: string
  reasoning?: ScenarioPublicReasoningSummary
  observation?: ScenarioPublicObservation
  tool?: ScenarioToolEventPayload
  error_code?: string
}

// ===== V2 正式事件（hiddenworld.v2，Go 唯一生成）=====

export const SCENARIO_RUN_EVENT_SCHEMA_V2 = 'hiddenworld.v2'

export type ToolCallState =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'unsupported'
  | 'rejected'
  | 'expired'
  | 'already_completed'

export type ToolResultStatus = 'succeeded' | 'failed' | 'timeout'

export interface ScenarioPublicContentMeta {
  tool_kind?: string
  is_negative?: boolean
  source_kind?: string
  source_label?: string
  title?: string
}

/** observation / clue / hint 的统一外发内容层；markdown_ready 是唯一渲染源。 */
export interface ScenarioPublicContent {
  content_type: 'observation' | 'clue' | 'hint'
  markdown_ready: string
  display_variant?: 'log' | 'tool_return' | 'clue' | 'hint'
  meta?: ScenarioPublicContentMeta
}

/** task_upserted 的负载：工具调用生命周期状态。 */
export interface ScenarioTaskPayload {
  task_id: string
  call_id?: string
  title: string
  state: ToolCallState
  tool_ref?: string
  error_code?: string
}

/** tool_result 的负载：只表达执行终态，不含 pending/running。 */
export interface ScenarioToolResultPayload {
  call_id: string
  tool_id: string
  tool_kind: string
  result_status: ToolResultStatus
  duration_ms: number
  content?: ScenarioPublicContent
  error_code?: string
}

export interface ScenarioAllowedAction {
  action_id: string
  catalog_version: string
  tool_kind: string
  title: string
}

export interface ScenarioTeachingDimensionRef {
  dimension_id: string
  category: string
  status: 'unexplored' | 'in_progress' | 'covered'
  hint_level: 'none' | 'light' | 'direct'
}

export interface ScenarioCluePayload {
  clue_id: string
  content: ScenarioPublicContent
  dimension?: ScenarioTeachingDimensionRef
}

export interface ScenarioHintPayload {
  hint_id: string
  level: number
  content: ScenarioPublicContent
}

interface RunEventV2Base {
  request_id: string
  sequence: number
  schema_version: typeof SCENARIO_RUN_EVENT_SCHEMA_V2
  state_revision: number
}

export type ScenarioRunEventV2 =
  | (RunEventV2Base & { kind: 'turn_started'; payload: { turn_id?: string; task_summary?: string } })
  | (RunEventV2Base & { kind: 'task_upserted'; payload: { task: ScenarioTaskPayload } })
  | (RunEventV2Base & { kind: 'tool_result'; payload: { tool_result: ScenarioToolResultPayload } })
  | (RunEventV2Base & { kind: 'clue_published'; payload: { clue: ScenarioCluePayload } })
  | (RunEventV2Base & { kind: 'hint_published'; payload: { hint: ScenarioHintPayload } })
  | (RunEventV2Base & {
      kind: 'assistant_delta'
      payload: { phase: 'understanding' | 'replying'; markdown_ready_delta: string }
    })
  | (RunEventV2Base & { kind: 'turn_completed'; payload: { next_actions?: ScenarioAllowedAction[] } })
  | (RunEventV2Base & { kind: 'turn_failed'; payload: { error_code?: string; retryable?: boolean } })

export type ScenarioRunEventAny = ScenarioRunEvent | ScenarioRunEventV2

export function isScenarioRunEventV2(event: ScenarioRunEventAny): event is ScenarioRunEventV2 {
  return (event as ScenarioRunEventV2).schema_version === SCENARIO_RUN_EVENT_SCHEMA_V2
}
