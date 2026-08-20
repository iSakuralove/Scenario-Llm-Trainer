import { Bot, ChevronDown, UserRound, Wrench, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { ScenarioRunEvent, ScenarioToolEventPayload } from '../../../types/agentRun'
import { StreamingText } from './StreamingText'
import { ThinkingReasoning } from './ThinkingReasoning'
import { ThinkingState } from './ThinkingState'
import styles from './AgentRun.module.css'

type ToolLifecycle = 'started' | 'result' | 'completed'

interface AggregatedTool extends ScenarioToolEventPayload {
  lifecycle: ToolLifecycle
}

interface AgentRunProps {
  events: ScenarioRunEvent[]
  fallbackUser?: string
  fallbackReply?: string
  active?: boolean
}

export function AgentRun({ events, fallbackUser = '', fallbackReply = '', active = false }: AgentRunProps) {
  const ordered = useMemo(() => dedupeEvents(events), [events])
  const userText = ordered.find((event) => event.kind === 'user_message')?.text || fallbackUser
  const replyChunks = ordered.filter((event) => event.kind === 'reply_delta' && event.text).map((event) => event.text ?? '')
  const understanding = understandingSummary(ordered)
  const reasoning = uniqueReasoning(ordered)
  const tools = aggregateTools(ordered)
  const failure = ordered.find((event) => event.kind === 'turn_failed')
  const complete = ordered.some((event) => event.kind === 'turn_completed')
  const isProcessing = active && !complete && !failure
  // 本轮失败时不渲染任何正文。turn_failed 意味着回复没通过安全校验或状态审批，
  // 此前流出的分片属于未获批内容，必须从屏幕上撤回而不是留在失败提示上方。
  const visibleReply = failure
    ? []
    : (replyChunks.length > 0 ? replyChunks : (fallbackReply ? [fallbackReply] : []))

  return (
    <div className={styles.run} data-testid="scenario-agent-run">
      <article className={`${styles.message} ${styles.userMessage}`}>
        <span className={styles.avatar} role="img" aria-label="你">
          <UserRound size={17} aria-hidden="true" />
        </span>
        <div className={styles.userBubble}>
          <p>{userText}</p>
        </div>
      </article>

      <article className={`${styles.message} ${styles.agentMessage}`}>
        <span className={styles.avatar} role="img" aria-label="排查导师">
          <Bot size={17} aria-hidden="true" />
        </span>

        <div className={styles.agentFlow}>
          {isProcessing && !understanding && <ThinkingState label={thinkingLabel(ordered)} />}

          {understanding && (
            <div className={styles.reasoningLine} aria-live="polite" data-testid="agent-run-understanding">
              <StreamingText chunks={understanding.chunks} active={isProcessing && !understanding.settled} />
            </div>
          )}

          <ThinkingReasoning items={reasoning} />

          {tools.map((tool, index) => (
            <ToolDisclosure key={`${tool.name}-${index}`} tool={tool} />
          ))}

          {visibleReply.length > 0 && (
            <div className={styles.replyLine} aria-live="polite" data-testid="agent-run-reply">
              <p><StreamingText chunks={visibleReply} active={isProcessing && replyChunks.length > 0} /></p>
            </div>
          )}

          {failure && (
            <div className={styles.failureLine} role="alert">
              <XCircle size={15} aria-hidden="true" />
              <span>{failure.summary || '本轮处理失败，请重试。'}</span>
            </div>
          )}
        </div>
      </article>
    </div>
  )
}

function ToolDisclosure({ tool }: { tool: AggregatedTool }) {
  const [open, setOpen] = useState(false)
  const result = tool.result
  return (
    <div className={styles.toolLine}>
      <button
        className={styles.toolButton}
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
      >
        <Wrench size={14} aria-hidden="true" />
        <span>调用工具 {tool.name}</span>
        <small>{toolButtonStatus(tool)}</small>
        <ChevronDown className={open ? styles.chevronOpen : ''} size={14} aria-hidden="true" />
      </button>
      <div className={`${styles.disclosureGrid} ${open ? styles.disclosureGridOpen : ''}`}>
        <div className={styles.disclosureInner}>
          <dl className={styles.toolDetails}>
            <div>
              <dt>执行状态</dt>
              <dd>{toolLifecycleLabel(tool.lifecycle)}</dd>
            </div>
            <div>
              <dt>耗时</dt>
              <dd>{tool.duration_ms > 0 ? `${tool.duration_ms} ms` : '等待结果'}</dd>
            </div>
            <div>
              <dt>脱敏参数</dt>
              <dd>{formatRecord(tool.redacted_arguments)}</dd>
            </div>
            {result && (
              <>
                <div>
                  <dt>证据状态</dt>
                  <dd>{supportStatusLabel(result.support_status)}</dd>
                </div>
                {result.user_points.length > 0 && (
                  <div>
                    <dt>你的要点</dt>
                    <dd>{result.user_points.join('；')}</dd>
                  </div>
                )}
                {result.next_action && (
                  <div>
                    <dt>下一步</dt>
                    <dd>{result.next_action}</dd>
                  </div>
                )}
              </>
            )}
          </dl>
        </div>
      </div>
    </div>
  )
}

function dedupeEvents(events: ScenarioRunEvent[]) {
  const byKey = new Map<string, ScenarioRunEvent>()
  for (const event of events) byKey.set(`${event.request_id}:${event.sequence}`, event)
  return [...byKey.values()].sort((left, right) => left.sequence - right.sequence)
}

// understanding_message 阶段单独抽出来渲染成一条持续可见的行。
// 后端现在会把 public_summary 拆成 reasoning_summary_delta 流式下发，
// 如果继续丢给 ThinkingReasoning，每个碎片都会变成折叠列表里的一行。
// completed 事件到达后用它的完整文本覆盖累积文本，避免收尾时文字跳动。
function understandingSummary(events: ScenarioRunEvent[]) {
  const settledText = events.find(
    (event) => event.kind === 'reasoning_summary_completed' && event.reasoning?.stage === 'understanding_message',
  )?.reasoning?.text
  if (settledText) return { chunks: [settledText], settled: true }

  const chunks = events
    .filter((event) => event.kind === 'reasoning_summary_delta' && event.reasoning?.stage === 'understanding_message')
    .map((event) => event.reasoning?.text ?? '')
    .filter(Boolean)
  return chunks.length > 0 ? { chunks, settled: false } : null
}

function uniqueReasoning(events: ScenarioRunEvent[]) {
  const seen = new Set<string>()
  return events.flatMap((event) => {
    if (event.kind === 'reasoning_summary_delta') return []
    if (event.reasoning?.stage === 'understanding_message') return []
    if (!event.reasoning?.text || seen.has(event.reasoning.text)) return []
    seen.add(event.reasoning.text)
    return [event.reasoning]
  })
}

function aggregateTools(events: ScenarioRunEvent[]) {
  const tools = new Map<string, AggregatedTool>()
  for (const event of events) {
    if (!event.tool?.name) continue
    const key = event.tool.redacted_arguments?.answer_attempt_id || event.tool.name
    const current = tools.get(key)
    tools.set(key, {
      ...current,
      ...event.tool,
      redacted_arguments: event.tool.redacted_arguments ?? current?.redacted_arguments ?? {},
      result: event.tool.result ?? current?.result,
      lifecycle: toolLifecycle(event.kind),
    })
  }
  return [...tools.values()]
}

function toolLifecycle(kind: ScenarioRunEvent['kind']): ToolLifecycle {
  if (kind === 'tool_completed') return 'completed'
  if (kind === 'tool_result') return 'result'
  return 'started'
}

function toolLifecycleLabel(lifecycle: ToolLifecycle) {
  switch (lifecycle) {
    case 'started':
      return '执行中'
    case 'result':
      return '结果已返回'
    case 'completed':
      return '已完成'
  }
}

function toolButtonStatus(tool: AggregatedTool) {
  const status = toolLifecycleLabel(tool.lifecycle)
  return tool.duration_ms > 0 ? `${tool.duration_ms} ms · ${status}` : status
}

function thinkingLabel(events: ScenarioRunEvent[]) {
  if (events.some((event) => event.kind === 'tool_started')) return '正在校验本轮答案表述'
  if (events.some((event) => event.kind === 'response_summary' || event.kind === 'mentor_buffered')) return '正在整理导师回复'
  return '正在理解本轮排查意图'
}

// response_summary / mentor_buffered / guard_passed / proposal_approved 刻意不渲染：
// 那是内部机器在向学生汇报自己的工作，零信息量，还暴露了安全机制的存在。
//
// 但这些事件**不能从协议里删**——scenario_agent.go 强制要求每轮恰好一条
// guard_passed，Python 少发一条整轮会以 public_trace_rejected 失败。
// 「不展示」和「不发送」是两件事。

function supportStatusLabel(status: string) {
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
      return status
  }
}

function formatRecord(value: Record<string, string>) {
  const entries = Object.entries(value)
  if (entries.length === 0) return '无公开参数'
  return entries.map(([key, item]) => `${key}: ${item}`).join('，')
}
