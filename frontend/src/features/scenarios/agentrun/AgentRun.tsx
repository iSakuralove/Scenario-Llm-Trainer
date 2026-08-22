import { Bot, ChevronDown, Clock3, Lightbulb, Loader2, UserRound, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import type {
  ScenarioAllowedAction,
  ScenarioRunEventAny,
  ScenarioTaskPayload,
  ScenarioToolResultPayload,
} from '../../../types/agentRun'
import { buildAgentRunViewModel } from './LegacyEventAdapter'
import { QuickActions } from './QuickActions'
import { TaskList } from './TaskList'
import { ToolKindIcon } from './ToolKindIcon'
import { StreamingText } from './StreamingText'
import { ThinkingReasoning } from './ThinkingReasoning'
import { ThinkingState } from './ThinkingState'
import styles from './AgentRun.module.css'

interface AgentRunProps {
  events: ScenarioRunEventAny[]
  fallbackUser?: string
  fallbackReply?: string
  active?: boolean
  onQuickAction?: (action: ScenarioAllowedAction) => void
  quickActionDisabled?: boolean
}

export function AgentRun({
  events,
  fallbackUser = '',
  fallbackReply = '',
  active = false,
  onQuickAction,
  quickActionDisabled = false,
}: AgentRunProps) {
  const model = useMemo(() => buildAgentRunViewModel(events), [events])
  const userText = model.userText || fallbackUser
  const isProcessing = active && !model.complete && !model.failure
  const hasPublicObservation = model.toolResults.length > 0 || model.clues.length > 0 || model.hints.length > 0
  const historicalUnderstanding = model.understanding && !active
    ? model.understanding.chunks.join('').trim()
    : ''
  const showUnderstanding = Boolean(
    model.understanding
    && (!historicalUnderstanding || !isTransientHistoricalStatus(historicalUnderstanding)),
  )
  const replyChunks = model.replyChunks.length > 0
    ? model.replyChunks
    : (fallbackReply ? [fallbackReply] : [])
  const visibleReplyChunks = normalizeHistoricalReply(
    replyChunks,
    { active, hasPublicObservation },
  )
  // 本轮失败时不渲染任何正文。turn_failed 意味着回复没通过安全校验或状态审批，
  // 此前流出的分片属于未获批内容，必须从屏幕上撤回而不是留在失败提示上方。
  const visibleReply = model.failure
    ? []
    : visibleReplyChunks
  // 统一工具芯片：任务（运行中状态）与工具结果按 call_id 合并成一行，
  // 点击展开查看返回内容——与 Codex 的 MCP 工具行同构。
  const toolChips = useMemo(() => mergeTasksAndResults(model.tasks, model.toolResults), [model])

  return (
    <div className={styles.run} data-testid="scenario-agent-run">
      {userText && (
        <article className={`${styles.message} ${styles.userMessage}`}>
          <span className={styles.avatar} role="img" aria-label="你">
            <UserRound size={17} aria-hidden="true" />
          </span>
          <div className={styles.userBubble}>
            <p>{userText}</p>
          </div>
        </article>
      )}

      <article className={`${styles.message} ${styles.agentMessage}`}>
        <span className={styles.avatar} role="img" aria-label="对方">
          <Bot size={17} aria-hidden="true" />
        </span>

        <div className={styles.agentFlow}>
          {isProcessing && shouldShowThinking(model) && <ThinkingState label={thinkingLabel(model)} />}

          {showUnderstanding && model.understanding && (
            <div className={styles.reasoningLine} aria-live="polite" data-testid="agent-run-understanding">
              <StreamingText
                chunks={model.understanding.chunks}
                active={isProcessing && !model.understanding.settled}
              />
            </div>
          )}

          <ThinkingReasoning
            items={model.legacyReasoningItems.map((text) => ({ stage: 'composing_reply' as const, text }))}
          />

          {model.tasks.length > 1 && <TaskList tasks={model.tasks} active={isProcessing} />}

          {toolChips.map((chip) => (
            <ToolChipRow key={chip.key} chip={chip} />
          ))}

          {model.hints.map((hint) => (
            <HintCard key={hint.hintId} level={hint.level} content={hint.content} />
          ))}

          {visibleReply.length > 0 && (
            <div className={styles.replyLine} aria-live="polite" data-testid="agent-run-reply">
              <p><StreamingText chunks={visibleReply} active={isProcessing && model.replyChunks.length > 0} /></p>
            </div>
          )}

          {model.failure && (
            <div className={styles.failureLine} role="alert">
              <XCircle size={15} aria-hidden="true" />
              <span>
                {model.failure}
                {model.failureCode && model.failureCode !== 'turn_failed' && (
                  <small data-testid="agent-run-failure-code"> · {model.failureCode}</small>
                )}
              </span>
            </div>
          )}

          {model.complete && !model.failure && model.nextActions.length > 0 && onQuickAction && (
            <QuickActions actions={model.nextActions} disabled={quickActionDisabled} onSelect={onQuickAction} />
          )}
        </div>
      </article>
    </div>
  )
}

function isTransientHistoricalStatus(text: string): boolean {
  return /^(正在|帮你查询|为你查询|引导学生|已为你|正在为你)/.test(text)
}

function normalizeHistoricalReply(
  chunks: string[],
  options: { active: boolean; hasPublicObservation: boolean },
): string[] {
  if (options.active || options.hasPublicObservation || chunks.length === 0) return chunks
  const joined = chunks.join('').trim()
  if (!/^(已记录这轮信息|已记录本轮信息|已记录这轮信息。|已记录本轮信息。)$/.test(joined)) return chunks
  return ['本轮没有形成新的公开观察。']
}

interface ToolChip {
  key: string
  title: string
  state: ScenarioTaskPayload['state']
  toolKind: string
  result?: ScenarioToolResultPayload
}

function mergeTasksAndResults(
  tasks: ScenarioTaskPayload[],
  toolResults: ScenarioToolResultPayload[],
): ToolChip[] {
  const consumed = new Set<string>()
  const chips: ToolChip[] = tasks.map((task) => {
    const result = toolResults.find(
      (item) =>
        !consumed.has(item.call_id)
        && (item.call_id === task.call_id || item.call_id === task.task_id),
    )
    if (result) consumed.add(result.call_id)
    return {
      key: task.task_id,
      title: task.title,
      state: result && result.result_status === 'succeeded' ? 'completed' : task.state,
      toolKind: result?.tool_kind || toolKindFromToolRef(task.tool_ref),
      result,
    }
  })
  for (const result of toolResults) {
    if (consumed.has(result.call_id)) continue
    consumed.add(result.call_id)
    chips.push({
      key: result.call_id,
      title: toolRowTitle(result),
      state: 'completed',
      toolKind: result.tool_kind,
      result,
    })
  }
  return chips
}

function toolKindFromToolRef(toolRef?: string): string {
  if (!toolRef) return 'observation'
  const [, remainder = ''] = toolRef.split(':')
  const [kind] = remainder.split('.')
  return kind || 'observation'
}

function ToolChipRow({ chip }: { chip: ToolChip }) {
  const [open, setOpen] = useState(false)
  const content = chip.result?.content
  const sourceLabel = publicSourceLabel(content?.meta?.source_kind, content?.meta?.source_label)
  return (
    <div className={styles.toolLine} data-testid="agent-run-tool-chip">
      <button
        className={styles.toolButton}
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
      >
        <ToolKindIcon kind={chip.toolKind} size={14} />
        <span>{chip.title}</span>
        {sourceLabel && <span className={styles.toolSourceBadge}>{sourceLabel}</span>}
        <small>{chipStatusLabel(chip)}</small>
        {chip.state === 'running'
          ? <Loader2 className={styles.taskSpinner} size={14} aria-hidden="true" />
          : <ChevronDown className={open ? styles.chevronOpen : ''} size={14} aria-hidden="true" />}
      </button>
      {content && (
        <div className={`${styles.disclosureGrid} ${open ? styles.disclosureGridOpen : ''}`}>
          <div className={styles.disclosureInner}>
            <div
              className={
                content.display_variant === 'log'
                  ? styles.observationResult
                  : styles.toolReturn
              }
              data-negative={content.meta?.is_negative ? 'true' : undefined}
            >
              <div className={styles.observationHeader}>
                <strong>{toolResultLabel(chip.toolKind)}</strong>
                {(chip.result?.duration_ms ?? 0) > 0 && (
                  <span>
                    <Clock3 size={12} aria-hidden="true" />
                    {chip.result?.duration_ms} ms
                  </span>
                )}
              </div>
              <p>{content.markdown_ready}</p>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function chipStatusLabel(chip: ToolChip): string {
  if (chip.state === 'running') return '查询中'
  if (chip.state === 'failed') return '失败'
  if (chip.state === 'rejected' || chip.state === 'unsupported' || chip.state === 'expired') return '已跳过'
  if (chip.result?.result_status === 'timeout') return '超时'
  if (chip.result && chip.result.duration_ms > 0) return `${chip.result.duration_ms} ms · 已返回`
  return '已返回'
}

function HintCard({
  level,
  content,
}: {
  level: number
  content: NonNullable<ScenarioToolResultPayload['content']>
}) {
  return (
    <aside className={styles.hintCard} data-testid="agent-run-hint" aria-label="教学提示">
      <div className={styles.hintHeader}>
        <span><Lightbulb size={14} aria-hidden="true" />{content.meta?.title || '教学提示'}</span>
        <small>{hintLevelLabel(level)}</small>
      </div>
      <p>{content.markdown_ready}</p>
    </aside>
  )
}

function toolResultLabel(toolKind: string): string {
  switch (toolKind) {
    case 'logs':
      return '日志'
    case 'metrics':
      return '指标'
    case 'config':
      return '配置'
    case 'database':
    case 'data':
      return '数据'
    case 'dependency':
      return '依赖'
    case 'verification':
      return '答案对比'
    default:
      return '观察'
  }
}

function toolRowTitle(toolResult: ScenarioToolResultPayload): string {
  const publicTitle = toolResult.content?.meta?.title?.trim()
  if (publicTitle) return publicTitle
  if (toolResult.tool_id === 'compare_answer') return '对比答案与已公开证据'
  return `查询${toolResultLabel(toolResult.tool_kind)}`
}

function publicSourceLabel(sourceKind?: string, sourceLabel?: string): string {
  if (sourceLabel?.trim()) return sourceLabel.trim()
  if (sourceKind === 'teaching_simulation' || sourceKind === 'simulation' || sourceKind === 'simulated') {
    return '教学模拟'
  }
  return ''
}

function hintLevelLabel(level: number): string {
  switch (Math.max(0, Math.min(4, level))) {
    case 1:
      return '提醒变化点'
    case 2:
      return '提示排查方向'
    case 3:
      return '收窄检查范围'
    case 4:
      return '给出可验证事实'
    default:
      return '方向提醒'
  }
}

// Thinking State 只表示“本轮仍在处理”，文案由最近一条实质事件推导；
// 回复正文开始流出后不再显示思考指示，正文本身就是进度。
function shouldShowThinking(model: ReturnType<typeof buildAgentRunViewModel>): boolean {
  return model.lastSignal !== 'replying' && model.lastSignal !== 'done' && model.lastSignal !== 'failed'
}

function thinkingLabel(model: ReturnType<typeof buildAgentRunViewModel>): string {
  switch (model.lastSignal) {
    case 'tool':
      return '查询已返回，正在继续'
    case 'clue':
      return '新线索已发布'
    case 'hint':
      return '教学提示已给出'
    case 'understanding':
      return '正在根据本轮输入安排检查'
    default:
      return '正在处理本轮内容'
  }
}
