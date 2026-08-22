// LegacyEventAdapter：v1 旧事件 → UnifiedViewModel；V2 事件直达同一 ViewModel。
//
// 适配只改变展示模型，不把旧事件伪装成新的 Runtime 事实：v1 的内部阶段事件
// （guard_passed / mentor_buffered / response_summary / proposal_approved）在两侧
// 都被丢弃；assistant 回复在 v1 里叫 reply_delta、在 v2 里叫
// assistant_delta(phase=replying)，进入 ViewModel 后是同一条回复流。
// 旧 trace 的序号是稳定落库序号，直接沿用，不重新编号。

import type {
  ScenarioPublicAnswerComparison,
  ScenarioPublicAnswerComparisonPayload,
  ScenarioPublicContent,
  ScenarioPublicObservation,
  ScenarioRunEvent,
  ScenarioRunEventAny,
  ScenarioAllowedAction,
  ScenarioTaskPayload,
  ScenarioToolResultPayload,
} from '../../../types/agentRun'
import { isScenarioRunEventV2 } from '../../../types/agentRun'

export interface AgentRunViewModel {
  userText: string
  understanding: { chunks: string[]; settled: boolean } | null
  legacyReasoningItems: string[]
  tasks: ScenarioTaskPayload[]
  toolResults: ScenarioToolResultPayload[]
  clues: { clueId: string; content: ScenarioPublicContent }[]
  hints: { hintId: string; level: number; content: ScenarioPublicContent }[]
  replyChunks: string[]
  failure: string | null
  failureCode: string | null
  complete: boolean
  nextActions: ScenarioAllowedAction[]
  /** 最近一条实质事件，驱动 Thinking State 文案；回复流期间为 replying。 */
  lastSignal: 'turn' | 'understanding' | 'tool' | 'clue' | 'hint' | 'replying' | 'done' | 'failed'
}

export function buildAgentRunViewModel(events: ScenarioRunEventAny[]): AgentRunViewModel {
  const ordered = dedupeRunEvents(events)
  const model: AgentRunViewModel = {
    userText: '',
    understanding: null,
    legacyReasoningItems: [],
    tasks: [],
    toolResults: [],
    clues: [],
    hints: [],
    replyChunks: [],
    failure: null,
    failureCode: null,
    complete: false,
    nextActions: [],
    lastSignal: 'turn',
  }
  const understandingChunks: string[] = []
  let understandingAccumulated = ''
  let understandingSettled = false
  const tasksById = new Map<string, ScenarioTaskPayload>()

  for (const event of ordered) {
    if (isScenarioRunEventV2(event)) {
      applyV2Event(model, event, {
        noteUnderstanding(text) {
          if (understandingSettled) return
          // completed 投影携带整段文本，与此前增量拼接结果一致：视为定稿，
          // 覆盖碎片，避免同一段话渲染两遍。
          if (understandingAccumulated && text === understandingAccumulated) {
            understandingSettled = true
            understandingChunks.length = 0
            understandingChunks.push(text)
            return
          }
          understandingChunks.push(text)
          understandingAccumulated += text
        },
        markUnderstandingSettled() {
          understandingSettled = true
        },
        upsertTask(task) {
          tasksById.set(task.task_id, { ...tasksById.get(task.task_id), ...task })
        },
        linkTaskResult(callId) {
          for (const [taskId, task] of tasksById) {
            if (task.state === 'completed') continue
            if (task.call_id === callId || taskId === callId) {
              tasksById.set(taskId, { ...task, state: 'completed' })
              return
            }
          }
        },
      })
      continue
    }
    applyLegacyEvent(model, event, {
      noteUnderstanding(text) {
        if (understandingSettled) return
        if (understandingAccumulated && text === understandingAccumulated) {
          understandingSettled = true
          understandingChunks.length = 0
          understandingChunks.push(text)
          return
        }
        understandingChunks.push(text)
        understandingAccumulated += text
      },
      markUnderstandingSettled() {
        understandingSettled = true
      },
    })
  }
  model.understanding = understandingChunks.length > 0 ? { chunks: understandingChunks, settled: understandingSettled } : null
  model.tasks = [...tasksById.values()]
  return model
}

interface ViewModelCallbacks {
  noteUnderstanding: (text: string) => void
  markUnderstandingSettled: () => void
}

function applyV2Event(
  model: AgentRunViewModel,
  event: ScenarioRunEventAny & { schema_version: string },
  callbacks: ViewModelCallbacks & {
    upsertTask: (task: ScenarioTaskPayload) => void
    linkTaskResult: (callId: string) => void
  },
): void {
  const v2 = event as import('../../../types/agentRun').ScenarioRunEventV2
  switch (v2.kind) {
    case 'turn_started':
      model.lastSignal = 'turn'
      break
    case 'assistant_delta': {
      if (v2.payload.phase === 'understanding') {
        callbacks.noteUnderstanding(v2.payload.markdown_ready_delta)
        model.lastSignal = 'understanding'
      } else {
        if (model.replyChunks.length === 0) callbacks.markUnderstandingSettled()
        if (v2.payload.markdown_ready_delta) model.replyChunks.push(v2.payload.markdown_ready_delta)
        model.lastSignal = 'replying'
      }
      break
    }
    case 'task_upserted':
      callbacks.upsertTask(v2.payload.task)
      model.lastSignal = 'tool'
      break
    case 'tool_result': {
      const toolResult = v2.payload.tool_result
      if (toolResult.content?.content_type !== 'clue'
        && toolResult.content?.content_type !== 'hint') {
        model.toolResults.push(toolResult)
      }
      // 工具结果到达即补齐对应任务的终态：即使 task_upserted(completed)
      // 事件丢失或乱序，芯片也不会永远停在“查询中”。
      callbacks.linkTaskResult(toolResult.call_id)
      model.lastSignal = 'tool'
      break
    }
    case 'clue_published':
      if (!model.clues.some((item) => item.clueId === v2.payload.clue.clue_id)) {
        model.clues.push({ clueId: v2.payload.clue.clue_id, content: v2.payload.clue.content })
      }
      model.lastSignal = 'clue'
      break
    case 'hint_published':
      if (!model.hints.some((item) => item.hintId === v2.payload.hint.hint_id)) {
        model.hints.push({
          hintId: v2.payload.hint.hint_id,
          level: v2.payload.hint.level,
          content: v2.payload.hint.content,
        })
      }
      model.lastSignal = 'hint'
      break
    case 'turn_completed':
      model.complete = true
      model.lastSignal = 'done'
      model.nextActions = v2.payload.next_actions ?? []
      break
    case 'turn_failed':
      model.failure = '本轮处理失败，请重试。'
      model.failureCode = v2.payload.error_code ?? 'turn_failed'
      model.lastSignal = 'failed'
      break
  }
}

function applyLegacyEvent(model: AgentRunViewModel, event: ScenarioRunEvent, callbacks: ViewModelCallbacks): void {
  switch (event.kind) {
    case 'user_message':
      if (event.text) model.userText = event.text
      model.lastSignal = 'turn'
      break
    case 'reasoning_summary_delta':
      if (event.reasoning?.stage === 'understanding_message' && event.reasoning.text) {
        callbacks.noteUnderstanding(event.reasoning.text)
        model.lastSignal = 'understanding'
      }
      break
    case 'reasoning_summary_completed':
      if (event.reasoning?.stage === 'understanding_message' && event.reasoning.text) {
        callbacks.noteUnderstanding(event.reasoning.text)
        model.lastSignal = 'understanding'
      } else if (event.reasoning?.text) {
        model.legacyReasoningItems.push(event.reasoning.text)
      }
      break
    case 'observation_result':
      if (event.observation) {
        model.toolResults.push(legacyObservationToolResult(event.observation))
        model.lastSignal = 'tool'
      }
      break
    case 'tool_started':
    case 'tool_result':
    case 'tool_completed':
      if (event.tool) {
        applyLegacyCompareAnswerEvent(model, event.kind, event.tool)
        model.lastSignal = 'tool'
      }
      break
    case 'reply_delta':
      if (model.replyChunks.length === 0) callbacks.markUnderstandingSettled()
      if (event.text) model.replyChunks.push(event.text)
      model.lastSignal = 'replying'
      break
    case 'turn_completed':
      model.complete = true
      model.lastSignal = 'done'
      break
    case 'turn_failed':
      model.failure = sanitizeFailureSummary(event.summary)
      model.failureCode = event.error_code || 'turn_failed'
      model.lastSignal = 'failed'
      break
    default:
      // response_summary / mentor_buffered / guard_passed / proposal_approved
      // 是内部机器汇报，两侧适配都丢弃，不进入展示模型。
      break
  }
}

function sanitizeFailureSummary(summary?: string): string {
  const normalized = summary?.trim() ?? ''
  if (!normalized) return '本轮处理失败，请重试。'
  const canonical = normalized.replace(/[。.!！]+$/g, '').trim().toLowerCase()
  const stableMessages: Record<string, string> = {
    'session not found': '排查会话不存在或已失效',
    'session is abandoned': '排查会话已结束，请重新开始',
    'session is not active': '排查会话已结束，请重新开始',
    'scenario session is not active': '排查会话已结束，请重新开始',
    'content is required': '请输入排查内容',
    'request_id is invalid': '本轮请求标识无效，请重新发送',
    'max turns reached, please submit an answer': '本轮次已用完，请提交排查结论',
  }
  if (stableMessages[canonical]) return stableMessages[canonical]
  if (/(postgres|postgresql|mysql|redis|sqlstate|dial tcp|hostname|lookup .*127\.0\.0\.1|connection refused|stack trace|panic)/i.test(normalized)) {
    return '本轮处理失败，请重试。'
  }
  if (/^(agent_|stream_|scenario_|reply_echoed_user_message|turn_failed|public_boundary_rejected)/i.test(canonical)) {
    return '本轮处理失败，请重试。'
  }
  return normalized
}

function applyLegacyCompareAnswerEvent(
  model: AgentRunViewModel,
  kind: ScenarioRunEvent['kind'],
  tool: import('../../../types/agentRun').ScenarioToolEventPayload,
): void {
  if (tool.name !== 'compare_answer') return
  if (kind === 'tool_started') {
    model.tasks.push({
      task_id: 'compare-answer',
      call_id: 'compare_answer',
      title: '对比答案与已公开证据',
      state: 'running',
      tool_ref: 'compare_answer',
    })
    return
  }
  if (kind === 'tool_completed') {
    const task = model.tasks.find((item) => item.task_id === 'compare-answer')
    if (task) task.state = 'completed'
    return
  }
  // tool_result：合成与 Go V2 投影一致的 markdown_ready。
  if (tool.result) {
    model.toolResults.push({
      call_id: 'compare_answer',
      tool_id: 'compare_answer',
      tool_kind: 'verification',
      result_status: 'succeeded',
      duration_ms: tool.duration_ms,
      content: {
        content_type: 'observation',
        markdown_ready: legacyComparisonMarkdown(tool.result),
        display_variant: 'tool_return',
        meta: { tool_kind: 'verification' },
      },
    })
  }
}

function legacyComparisonMarkdown(comparison: ScenarioPublicAnswerComparisonPayload): string {
  const isV2 = 'conclusion_status' in comparison
  if (isV2) {
    const parts = [
      `结论完整度：${conclusionStatusLabel(comparison.conclusion_status)}`,
      `证据充分度：${evidenceStatusLabel(comparison.evidence_status)}`,
      `因果链：${causalStatusLabel(comparison.causal_status)}`,
    ]
    if ((comparison.missing_dimensions ?? []).length > 0) {
      parts.push(`还需补充：${comparison.missing_dimensions?.map(comparisonDimensionLabel).join('、')}`)
    }
    if ((comparison.contradictions ?? []).length > 0) {
      parts.push(`需要核对：${comparison.contradictions?.join('；')}`)
    }
    return parts.join('；')
  }

  const parts = [`答案对比：${supportStatusLabel(comparison.support_status)}`]
  if (comparison.user_points.length > 0) parts.push(`你的要点：${comparison.user_points.join('；')}`)
  return parts.join('；')
}

export function supportStatusLabel(status?: string): string {
  switch (status) {
    case 'insufficiently_specific':
      return '表述还不够具体'
    case 'needs_more_evidence':
      return '还需要更多直接观察'
    case 'has_evidence_conflict':
      return '与已有观察存在冲突'
    case 'evidence_consistent':
      return '与已有观察一致'
    default:
      return '暂无法判断'
  }
}

function conclusionStatusLabel(status?: ScenarioPublicAnswerComparison['conclusion_status']): string {
  switch (status) {
    case 'none':
      return '尚未形成明确结论'
    case 'partial':
      return '已提出部分结论'
    case 'supported':
      return '结论已有公开证据支持'
    case 'contradictory':
      return '结论与公开证据存在矛盾'
    default:
      return '暂无法判断'
  }
}

function evidenceStatusLabel(status?: ScenarioPublicAnswerComparison['evidence_status']): string {
  switch (status) {
    case 'none':
      return '尚未引用公开证据'
    case 'insufficient':
      return '证据不足'
    case 'partial':
      return '已有部分证据'
    case 'sufficient':
      return '证据链基本充分'
    default:
      return '暂无法判断'
  }
}

function causalStatusLabel(status?: ScenarioPublicAnswerComparison['causal_status']): string {
  switch (status) {
    case 'missing':
      return '尚未说明因果关系'
    case 'partial':
      return '因果链仍不完整'
    case 'sufficient':
      return '因果链基本完整'
    default:
      return '暂无法判断'
  }
}

function comparisonDimensionLabel(dimension: string): string {
  switch (dimension) {
    case 'conclusion':
      return '明确结论'
    case 'evidence':
      return '证据链'
    case 'causal_link':
      return '因果关系'
    case 'consistency':
      return '证据一致性'
    default:
      return '必要分析维度'
  }
}

function legacyObservationToolResult(observation: ScenarioPublicObservation): ScenarioToolResultPayload {
  const toolKind = toolKindForAction(observation.action)
  return {
    call_id: `obs:${observation.action}`,
    tool_id: observation.action,
    tool_kind: toolKind,
    result_status: 'succeeded',
    duration_ms: 0,
    content: {
      content_type: 'observation',
      markdown_ready: observation.result,
      display_variant: toolKind === 'logs' ? 'log' : 'tool_return',
      meta: { tool_kind: toolKind, is_negative: observation.is_negative },
    },
  }
}

function toolKindForAction(action: string): string {
  const [, remainder = ''] = action.split(':')
  const [kind] = remainder.split('.')
  return kind || 'observation'
}

export function dedupeRunEvents(events: ScenarioRunEventAny[]): ScenarioRunEventAny[] {
  const byKey = new Map<string, ScenarioRunEventAny>()
  for (const event of events) byKey.set(`${event.request_id}:${event.sequence}`, event)
  return [...byKey.values()].sort((left, right) => left.sequence - right.sequence)
}

/** 左侧线索时间线只展示主动发布的 clue，不承载对话中的 observation。 */
export interface ObservationRelease {
  action: string
  result: string
  is_negative: boolean
  key: string
  title?: string
}

export function collectObservationReleases(events: ScenarioRunEventAny[]): ObservationRelease[] {
  const seen = new Set<string>()
  const releases: ObservationRelease[] = []
  for (const event of dedupeRunEvents(events)) {
    if (!isScenarioRunEventV2(event) || event.kind !== 'clue_published') continue
    const clue = event.payload.clue
    const action = `clue:${clue.clue_id}`
    const result = clue.content.markdown_ready
    if (!result) continue
    const key = `${action}::${result}`
    if (seen.has(key)) continue
    seen.add(key)
    releases.push({
      action,
      result,
      is_negative: false,
      key,
      title: clue.content.meta?.title,
    })
  }
  return releases
}

/** 新命名供调用方使用；保留旧导出名兼容 v1/V2 既有调用。 */
export const collectProactiveClues = collectObservationReleases
