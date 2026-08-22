import { Suspense, lazy, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, PointerEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { CheckCircle2, ChevronDown, ChevronUp, FileText, Send } from 'lucide-react'
import { api } from '../../api/client'
import type { ScenarioInvestigationState, ScenarioQuestion } from '../../types'
import { EmptyState, Loading } from '../../components/common'
import { MarkdownComposer } from '../../components/common/MarkdownComposer'
import { MermaidLoading } from '../../components/common/MermaidLoading'
import { useToken } from '../../lib/auth'
import { redactSensitiveText } from '../../lib/redaction'
import { useScenarioSessionStore } from '../../stores/scenarioSessionStore'
import { AgentRun, collectProactiveClues, resolveQuickActionUserLabel } from './agentrun'
import type { ObservationRelease } from './agentrun'
import { repairStatusLabel, resolveRepairStatus } from '../../types/agentRun'
import type { ScenarioAllowedAction, ScenarioRepairStatus } from '../../types/agentRun'
import './ScenarioSessionPage.css'

const MermaidRenderer = lazy(() => import('../../components/common/MermaidRenderer').then((module) => ({ default: module.MermaidRenderer })))

const CONTEXT_WIDTH_MIN = 280
// 快照面板可以拉到很宽以看清架构图；实际拖拽时再按窗口宽度动态收口，
// 给右侧对话区至少保留 CHAT_PANE_MIN 的空间。
const CONTEXT_WIDTH_MAX = 1280
const CHAT_PANE_MIN = 460
const ANSWER_HEIGHT_MIN = 220
const ANSWER_HEIGHT_MAX = 540

function contextWidthCeiling(): number {
  if (typeof window === 'undefined') return CONTEXT_WIDTH_MAX
  return Math.min(CONTEXT_WIDTH_MAX, Math.max(window.innerWidth - CHAT_PANE_MIN, CONTEXT_WIDTH_MIN))
}

export function ScenarioSessionPage() {
  const token = useToken()
  const navigate = useNavigate()
  const { id = '' } = useParams()
  const location = useLocation()
  const state = location.state as { question?: ScenarioQuestion; sessionId?: string } | null
  const question = useScenarioSessionStore((store) => store.question)
  const session = useScenarioSessionStore((store) => store.session)
  const messages = useScenarioSessionStore((store) => store.messages)
  const isLoading = useScenarioSessionStore((store) => store.isLoading)
  const isSending = useScenarioSessionStore((store) => store.isSending)
  const isQuitting = useScenarioSessionStore((store) => store.isQuitting)
  const sendError = useScenarioSessionStore((store) => store.sendError)
  const activeRun = useScenarioSessionStore((store) => store.activeRun)
  const completedRuns = useScenarioSessionStore((store) => store.completedRuns)
  const completedDebugReasoning = useScenarioSessionStore((store) => store.completedDebugReasoning)
  const completedDebugReasoningDuration = useScenarioSessionStore((store) => store.completedDebugReasoningDuration)
  const hydrateSession = useScenarioSessionStore((store) => store.hydrate)
  const sendMessage = useScenarioSessionStore((store) => store.sendMessage)
  const sendStructuredAction = useScenarioSessionStore((store) => store.sendStructuredAction)
  const quitScenarioSession = useScenarioSessionStore((store) => store.quit)
  const clearScenarioSession = useScenarioSessionStore((store) => store.clear)
  const [content, setContent] = useState('')
  const [answer, setAnswer] = useState('')
  const [isSubmittingAnswer, setSubmittingAnswer] = useState(false)
  const [answerStatus, setAnswerStatus] = useState('')
  const [answerError, setAnswerError] = useState('')
  const [contextWidth, setContextWidth] = useState(340)
  const [answerHeight, setAnswerHeight] = useState(300)
  const [isAnswerOpen, setAnswerOpen] = useState(false)
  const clueKeysRef = useRef<string[] | null>(null)
  const [animatedClueKeys, setAnimatedClueKeys] = useState<string[]>([])
  const isTurnInFlight = isSending || Boolean(
    activeRun && !activeRun.events.some((event) => event.kind === 'turn_failed'),
  )

  useEffect(() => {
    void hydrateSession(token, id, { question: state?.question ?? null }).catch(() => {})
    return () => {
      clearScenarioSession()
    }
  }, [clearScenarioSession, hydrateSession, id, state?.question, token])

  // 这些 Hooks 必须在加载态和已加载态都执行，避免 React 在恢复会话后改变
  // Hooks 数量。线索数据本身不依赖 question，可以安全地在条件渲染前聚合。
  const visibleRunEvents = useMemo(
    () => [
      ...messages.flatMap((message) => completedRuns[message.id] ?? message.response_meta.run_events ?? []),
      // turn_failed 表示本轮没有提交成功；其旁路事件只能用于当前失败
      // 状态的诊断，不能继续污染常驻线索板。
      ...(activeRun?.events.some((event) => event.kind === 'turn_failed') ? [] : (activeRun?.events ?? [])),
    ],
    [activeRun?.events, completedRuns, messages],
  )
  const clueReleases = useMemo(() => {
    return collectProactiveClues(visibleRunEvents)
  }, [visibleRunEvents])
  const clueKeySignature = clueReleases.map((item) => item.key).join('|')

  useEffect(() => {
    if (isLoading) return
    const keys = clueReleases.map((item) => item.key)
    if (clueKeysRef.current === null) {
      clueKeysRef.current = keys
      setAnimatedClueKeys([])
      return
    }
    const previous = new Set(clueKeysRef.current)
    setAnimatedClueKeys(keys.filter((key) => !previous.has(key)))
    clueKeysRef.current = keys
    const timer = window.setTimeout(() => setAnimatedClueKeys([]), 520)
    return () => window.clearTimeout(timer)
  }, [clueKeySignature, isLoading, clueReleases])

  if (isLoading && !question) {
    return <Loading title="恢复排查会话" />
  }

  if (!question) {
    // 深链直开会话时题目快照由 hydrate 从服务端补齐；走到这里说明补齐失败。
    // sendError 携带真实原因（会话不存在 / 不属于当前账号 / 凭证失效），
    // 不能用「请从排查工坊选择题目」掩盖跨账号复制会话链接的场景。
    return (
      <EmptyState
        title={sendError ? '会话读取失败' : '缺少会话上下文'}
        description={
          sendError
            ? `${sendError}。若这是从别处复制的会话链接，请先用创建该会话的账号登录。`
            : '请从排查工坊选择题目后进入会话。'
        }
        action={<Link className="primary-button" to="/scenarios">返回题目列表</Link>}
      />
    )
  }

  async function send() {
    const userContent = content.trim()
    if (!userContent || isSubmittingAnswer) return
    setContent('')
    try {
      await sendMessage(token, id, userContent)
    } catch (err) {
      void err
    }
  }

  function handleQuickAction(action: ScenarioAllowedAction) {
    if (isSubmittingAnswer) return
    void sendStructuredAction(token, id, action).catch(() => {})
  }

  async function submitAnswer() {
    if (!answer.trim() || isTurnInFlight || isQuitting) return
    setSubmittingAnswer(true)
    setAnswerError('')
    setAnswerStatus('正在提交排查结论')
    try {
      await api.submitScenarioAnswer(token, id, answer)
      navigate(`/scenarios/session/${id}/review`)
    } catch (err) {
      setAnswerError(err instanceof Error ? err.message : '排查结论提交失败')
      setAnswerStatus('排查结论提交失败')
    } finally {
      setSubmittingAnswer(false)
    }
  }

  async function quitSession() {
    if (isTurnInFlight || isSubmittingAnswer) return
    try {
      await quitScenarioSession(token, id)
      navigate('/scenarios', { replace: true })
    } catch (err) {
      void err
    }
  }

  const activeSession = session ?? {
    current_turn: 0,
    max_turns: 50,
    revealed_clue_count: 0,
    investigation_state: {
      current_focus: '',
      current_hypothesis: '',
      has_current_hypothesis: false,
      collected_evidence_count: 0,
      established_facts: [],
      ruled_out_labels: [],
      hint_level: 0,
    },
    state_revision: 0,
    status: 'active',
  }
  const establishedEvidenceCount = activeSession.investigation_state?.collected_evidence_count ?? 0
  const importantClueCount = Math.max(activeSession.revealed_clue_count ?? 0, clueReleases.length)
  const repairStatus = resolveRepairStatus(activeSession.investigation_state, visibleRunEvents)
  const snapshotText = (value = '') => question.is_sanitized ? redactSensitiveText(value) : value
  const publicScenario = question.content.public_scenario
  const diagramCode = publicScenario?.architecture_diagram ?? question.content.architecture_diagram ?? ''
  const diagramStatusMessage = getDiagramStatusMessage(question.content.diagram_status)
  const diagramWarningCount = question.content.diagram_warnings?.length ?? 0
  const layoutStyle = {
    '--session-context-width': `${contextWidth}px`,
    '--scenario-answer-height': `${answerHeight}px`,
  } as CSSProperties
  function resizeContext(event: PointerEvent<HTMLButtonElement>) {
    const startX = event.clientX
    const startWidth = contextWidth
    const pointerId = event.pointerId
    event.currentTarget.setPointerCapture(pointerId)

    const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
      const nextWidth = clamp(startWidth + moveEvent.clientX - startX, CONTEXT_WIDTH_MIN, contextWidthCeiling())
      setContextWidth(nextWidth)
    }
    const stopResize = () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', stopResize)
      window.removeEventListener('pointercancel', stopResize)
    }
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', stopResize)
    window.addEventListener('pointercancel', stopResize)
  }

  function resizeAnswer(event: PointerEvent<HTMLButtonElement>) {
    const startY = event.clientY
    const startHeight = answerHeight
    const pointerId = event.pointerId
    event.currentTarget.setPointerCapture(pointerId)

    const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
      const nextHeight = clamp(startHeight + startY - moveEvent.clientY, ANSWER_HEIGHT_MIN, ANSWER_HEIGHT_MAX)
      setAnswerHeight(nextHeight)
    }
    const stopResize = () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', stopResize)
      window.removeEventListener('pointercancel', stopResize)
    }
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', stopResize)
    window.addEventListener('pointercancel', stopResize)
  }

  return (
    <section className={`scenario-session-page session-layout ${isAnswerOpen ? 'answer-open' : 'answer-collapsed'}`} style={layoutStyle}>
      <aside className="context-pane" data-testid="session-context-pane">
        <div className="session-context-header">
          <div className="panel-title scenario-snapshot-title">
            <span><FileText size={18} /> 题目快照</span>
            <span className="scenario-difficulty-badge" data-testid="scenario-difficulty-badge">
              难度 {snapshotText(question.difficulty)}
            </span>
          </div>
        </div>
        <div className="session-context-body">
          <h2>{snapshotText(question.title)}</h2>
          <p>{snapshotText(question.description)}</p>
          {diagramStatusMessage && (
            <div
              className="mermaid-status-line"
              role="status"
              aria-live="polite"
              aria-label={`${diagramStatusMessage}${diagramWarningCount > 0 ? `，包含 ${diagramWarningCount} 条处理提示` : ''}`}
            >
              <span className={`mermaid-render-chip ${question.content.diagram_status === 'normalized' ? 'success' : ''}`}>
                {diagramStatusMessage}
              </span>
            </div>
          )}
          <Suspense fallback={<MermaidLoading />}>
            <MermaidRenderer code={snapshotText(diagramCode)} />
          </Suspense>
          <div className="clue-status">
            <span>轮次 {activeSession.current_turn}/{activeSession.max_turns}</span>
            <span>已形成证据 {establishedEvidenceCount}</span>
            <span>重要线索 {importantClueCount}</span>
          </div>
          <InvestigationStatePanel state={activeSession.investigation_state} repairStatus={repairStatus} />
          <ClueReleaseTimeline clues={clueReleases} animatedKeys={animatedClueKeys} snapshotText={snapshotText} />
        </div>
      </aside>
      <button
        className="session-context-resizer"
        type="button"
        data-testid="session-context-resizer"
        onPointerDown={resizeContext}
        aria-label="拖拽调整题目快照宽度"
        title="拖拽调整题目快照宽度"
      />
      <main className="chat-pane">
        <div className="chat-header">
          <div>
            <div className="chat-title-line">
              <strong>渐进式排查会话</strong>
            </div>
            <span>导师只依据已公开信息回应，不提前展示隐藏答案。</span>
          </div>
          <button
            className="ghost-button compact"
            type="button"
            onClick={() => void quitSession()}
            disabled={isQuitting || isTurnInFlight || isSubmittingAnswer}
          >
            {isQuitting ? '放弃中' : '放弃会话'}
          </button>
        </div>
        <div className="message-thread" data-testid="session-message-thread">
          {messages.map((message) => (
            <AgentRun
              key={message.id}
              events={completedRuns[message.id] ?? message.response_meta.run_events ?? []}
              fallbackUser={resolveQuickActionUserLabel(
                message.user_content,
                { events: completedRuns[message.id] ?? message.response_meta.run_events ?? [] },
              )}
              fallbackReply={message.assistant_content}
              rawReasoningChunks={completedDebugReasoning[message.id] ?? []}
              rawReasoningElapsedSeconds={completedDebugReasoningDuration[message.id]}
              onQuickAction={message.id === messages[messages.length - 1]?.id ? handleQuickAction : undefined}
              quickActionDisabled={isSending || isQuitting || isSubmittingAnswer}
            />
          ))}
          {activeRun && (
            <AgentRun
              events={activeRun.events}
              fallbackUser={resolveQuickActionUserLabel(
                activeRun.structuredAction?.title ?? activeRun.userContent,
                {
                  actionId: activeRun.structuredAction?.action_id,
                  toolKind: activeRun.structuredAction?.tool_kind,
                  events: activeRun.events,
                },
              )}
              active={isSending}
              rawReasoningChunks={activeRun.reasoningChunks}
              rawReasoningActive={isSending}
              onQuickAction={handleQuickAction}
              quickActionDisabled={isSending || isQuitting || isSubmittingAnswer}
            />
          )}
        </div>
        {sendError && <div className="inline-error chat-error">{sendError}</div>}
        <div className="composer">
          <textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="输入你的排查提问..." disabled={isSending || isQuitting || isSubmittingAnswer} />
          <button className="icon-button filled" onClick={() => void send()} disabled={isSending || isQuitting || isSubmittingAnswer} title="发送">
            <Send size={18} />
          </button>
        </div>
        <section className="scenario-answer-panel" data-testid="scenario-answer-panel">
          {isAnswerOpen && (
            <button
              className="scenario-answer-resizer"
              type="button"
              data-testid="scenario-answer-resizer"
              onPointerDown={resizeAnswer}
              aria-label="拖拽调整排查结论区高度"
              title="拖拽调整排查结论区高度"
            />
          )}
          <div className="scenario-answer-heading">
            <div>
              <strong>提交排查结论</strong>
              <span>{isAnswerOpen ? '按直接触发、潜在问题、证据链、风险、修复和验证组织结论。' : '先完成调查，需要提交时再展开。'}</span>
            </div>
            <div className="scenario-answer-actions">
              <button
                className="ghost-button compact"
                type="button"
                onClick={() => setAnswerOpen((current) => !current)}
                aria-expanded={isAnswerOpen}
                aria-controls="scenario-answer-editor-region"
              >
                {isAnswerOpen ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
                {isAnswerOpen ? '收起结论区' : '展开结论区'}
              </button>
              <button
                className="primary-button compact"
                onClick={() => void submitAnswer()}
                disabled={isQuitting || isSubmittingAnswer || isTurnInFlight || !answer.trim()}
                aria-busy={isSubmittingAnswer}
                data-testid="submit-scenario-answer"
              >
                <CheckCircle2 size={16} />{isSubmittingAnswer ? '提交中' : '提交排查结论'}
              </button>
            </div>
          </div>
          {isAnswerOpen && (
            <div id="scenario-answer-editor-region" className="scenario-answer-body">
              <MarkdownComposer
                value={answer}
                onChange={(value) => {
                  setAnswer(value)
                  setAnswerError('')
                  if (answerStatus === '排查结论提交失败') {
                    setAnswerStatus('')
                  }
                }}
                disabled={isQuitting || isSubmittingAnswer || isTurnInFlight}
                placeholder="建议按以下结构填写：直接触发、潜在问题、证据链、衍生风险、修复方案、验证与回滚观察。"
                editorLabel="Markdown 排查结论"
                editorTestId="scenario-answer-editor"
                fileInputTestId="scenario-answer-markdown-file-input"
                previewEmptyText="预览区：输入排查结论后会显示 Markdown 排版效果。"
                previewNote="这是 Markdown 渲染预览，提交时仍会使用原始结论内容。"
                onImportStatus={setAnswerStatus}
                onImportError={(message) => {
                  setAnswerError(message)
                  setAnswerStatus('Markdown 导入失败')
                }}
              />
            </div>
          )}
          {(answerStatus || answerError) && (
            <div className={`stream-status scenario-answer-status ${answerError ? 'error' : ''}`} role="status" aria-live="polite">
              <strong>{answerStatus || '排查结论状态'}</strong>
              {answerError && <span>{answerError}</span>}
            </div>
          )}
        </section>
      </main>
    </section>
  )
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max)
}

function ClueReleaseTimeline({
  clues,
  animatedKeys,
  snapshotText,
}: {
  clues: ObservationRelease[]
  animatedKeys: string[]
  snapshotText: (value?: string) => string
}) {
  const animated = new Set(animatedKeys)
  return (
    <section className="clue-release-panel" aria-label="重要线索" data-testid="important-clues-panel">
      <div className="clue-release-heading">
        <strong>重要线索</strong>
        <span>{clues.length > 0 ? `${clues.length} 条已发现` : '尚未形成'}</span>
      </div>
      {clues.length > 0 ? (
        <div className="clue-release-list">
          {clues.map((clue) => (
            <article className={`clue-release-card ${animated.has(clue.key) ? 'is-new' : ''}`} key={clue.key}>
              <div className="clue-release-card-header">
                <strong>{snapshotText(clue.title?.trim() || clueLabel(clue.action))}</strong>
              </div>
              <p>{snapshotText(clue.result)}</p>
            </article>
          ))}
        </div>
      ) : (
        <p className="clue-release-empty">重要线索会在你通过调查获得后显示在这里。</p>
      )}
    </section>
  )
}

function InvestigationStatePanel({
  state,
  repairStatus,
}: {
  state?: ScenarioInvestigationState
  repairStatus?: ScenarioRepairStatus
}) {
  const focusLabel = scenarioFocusLabel(state?.current_focus)
  const hypothesisLabel = state?.current_hypothesis?.trim() ?? ''
  const establishedFacts = state?.established_facts ?? []
  const ruledOutLabels = state?.ruled_out_labels ?? []
  return (
    <section className="investigation-state-panel" aria-label="当前调查状态" data-testid="investigation-state-panel">
      <div className="investigation-state-heading">
        <strong>当前调查状态</strong>
        <span>{focusLabel || '尚未形成主线'}</span>
      </div>
      <div className="investigation-state-grid">
        <div>
          <span>当前关注</span>
          <strong>{focusLabel || '等待公开证据'}</strong>
        </div>
        <div>
          <span>当前假设</span>
          <strong>{hypothesisLabel || (state?.has_current_hypothesis ? '已有方向，待补充描述' : '尚未形成')}</strong>
        </div>
        <div>
          <span>已形成事实</span>
          <strong>{establishedFacts.length > 0 ? establishedFacts.join('；') : '尚未形成'}</strong>
        </div>
        <div>
          <span>已降低优先级</span>
          <strong>{ruledOutLabels.length > 0 ? ruledOutLabels.join('；') : '暂无'}</strong>
        </div>
        <div>
          <span>提示进度</span>
          <strong>{hintLevelLabel(state?.hint_level ?? 0)}</strong>
        </div>
        {repairStatus && (
          <div>
            <span>修复进度</span>
            <strong>{repairStatusLabel(repairStatus)}</strong>
          </div>
        )}
      </div>
    </section>
  )
}

function scenarioFocusLabel(focus?: string) {
  const labels: Record<string, string> = {
    logs: '日志',
    metrics: '指标',
    config: '配置',
    change: '变更',
    dependency: '依赖',
    data: '数据',
    resource: '资源',
  }
  return focus ? labels[focus] ?? focus : ''
}

function hintLevelLabel(level: number) {
  switch (Math.max(0, Math.min(4, level))) {
    case 1:
      return '已提醒变化点'
    case 2:
      return '已提示排查方向'
    case 3:
      return '已收窄检查范围'
    case 4:
      return '已给出可验证事实'
    default:
      return '尚未使用教学提示'
  }
}

function clueLabel(action: string) {
  if (action.startsWith('clue:')) return '重要线索'
  const labels: Record<string, string> = {
    'inspect:logs.callback_timeout': '回调访问日志',
    'inspect:config.route_diff': '网关路由配置',
    'inspect:database.order_write': '订单库写入日志',
    'inspect:dependency.vip_route': 'VIP 后端池',
    'inspect:change.gateway_release': '网关发布记录',
    'inspect:dependency.dns': 'DNS 解析',
    'inspect:metrics.service': '回调服务指标',
  }
  return labels[action] ?? action.replace(/^inspect:/, '').replace(/[._-]+/g, ' ')
}

function getDiagramStatusMessage(status?: string) {
  switch (status) {
    case 'fallback':
      return '架构图已自动简化'
    case 'normalized':
      return '架构图已自动校正'
    default:
      return ''
  }
}
