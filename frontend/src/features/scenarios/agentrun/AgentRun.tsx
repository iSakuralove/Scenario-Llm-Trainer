import { Bot, ChevronDown, Clock3, FileText, UserRound, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { ScenarioAllowedAction, ScenarioRunEventAny, ScenarioToolResultPayload } from '../../../types/agentRun'
import { buildAgentRunViewModel } from './LegacyEventAdapter'
import { QuickActions } from './QuickActions'
import { ToolKindIcon } from './ToolKindIcon'
import { StreamingText } from './StreamingText'
import { TaskList } from './TaskList'
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
  const hasPublicObservation = model.toolResults.length > 0 || model.clues.length > 0
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
  // Task List 阈值：单轮至少 2 个工具/任务才显示内嵌列表；单个工具走工具行。
  const showTaskList = model.tasks.length >= 2
  const soloTasks = model.tasks.length === 1 ? model.tasks : []

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

          <ThinkingReasoning items={model.legacyReasoningItems.map((text) => ({ stage: 'composing_reply' as const, text }))} />

          {showTaskList && <TaskList tasks={model.tasks} active={isProcessing} />}

          {model.toolResults.map((toolResult, index) => (
            <ToolResultRow key={`${toolResult.call_id}-${index}`} toolResult={toolResult} />
          ))}

          {soloTasks.map((task) => (
            <SoloTaskLine key={task.task_id} title={task.title} state={task.state} />
          ))}

          {visibleReply.length > 0 && (
            <div className={styles.replyLine} aria-live="polite" data-testid="agent-run-reply">
              <p><StreamingText chunks={visibleReply} active={isProcessing && model.replyChunks.length > 0} /></p>
            </div>
          )}

          {model.failure && (
            <div className={styles.failureLine} role="alert">
              <XCircle size={15} aria-hidden="true" />
              <span>{model.failure}</span>
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

function ToolResultRow({ toolResult }: { toolResult: ScenarioToolResultPayload }) {
  const [open, setOpen] = useState(false)
  const content = toolResult.content
  const statusLabel = toolResultStatusLabel(toolResult)
  return (
    <div className={styles.toolLine} data-testid="agent-run-tool-result">
      <button
        className={styles.toolButton}
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
      >
        <ToolKindIcon kind={toolResult.tool_kind} size={14} />
        <span>{toolRowTitle(toolResult)}</span>
        <small>{statusLabel}</small>
        <ChevronDown className={open ? styles.chevronOpen : ''} size={14} aria-hidden="true" />
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
                <strong>{toolResultLabel(toolResult.tool_kind)}</strong>
                {toolResult.duration_ms > 0 && (
                  <span>
                    <Clock3 size={12} aria-hidden="true" />
                    {toolResult.duration_ms} ms
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

function SoloTaskLine({ title, state }: { title: string; state: string }) {
  return (
    <div className={styles.toolLine}>
      <div className={styles.toolButton} role="status">
        <FileText size={14} aria-hidden="true" />
        <span>{title}</span>
        <small>{state === 'completed' ? '已完成' : '进行中'}</small>
      </div>
    </div>
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
  if (toolResult.tool_id === 'compare_answer') return '对比答案与已公开证据'
  return `查询${toolResultLabel(toolResult.tool_kind)}`
}

function toolResultStatusLabel(toolResult: ScenarioToolResultPayload): string {
  const status =
    toolResult.result_status === 'succeeded' ? '已返回' : toolResult.result_status === 'timeout' ? '超时' : '失败'
  return toolResult.duration_ms > 0 ? `${toolResult.duration_ms} ms · ${status}` : status
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
    case 'understanding':
      return '正在根据本轮输入安排检查'
    default:
      return '正在处理本轮内容'
  }
}
