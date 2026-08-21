import { Suspense, lazy, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, PointerEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { CheckCircle2, ChevronDown, ChevronUp, FileText, Send } from 'lucide-react'
import { api } from '../../api/client'
import type { ScenarioQuestion } from '../../types'
import { EmptyState, Loading } from '../../components/common'
import { MarkdownComposer } from '../../components/common/MarkdownComposer'
import { MermaidLoading } from '../../components/common/MermaidLoading'
import { useToken } from '../../lib/auth'
import { redactSensitiveText } from '../../lib/redaction'
import { useScenarioSessionStore } from '../../stores/scenarioSessionStore'
import { AgentRun, collectProactiveClues, resolveQuickActionUserLabel } from './agentrun'
import type { ObservationRelease } from './agentrun'
import type { ScenarioAllowedAction } from '../../types/agentRun'
import './ScenarioSessionPage.css'

const MermaidRenderer = lazy(() => import('../../components/common/MermaidRenderer').then((module) => ({ default: module.MermaidRenderer })))

const CONTEXT_WIDTH_MIN = 280
const CONTEXT_WIDTH_MAX = 560
const ANSWER_HEIGHT_MIN = 220
const ANSWER_HEIGHT_MAX = 540

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

  useEffect(() => {
    void hydrateSession(token, id, { question: state?.question ?? null }).catch(() => {})
    return () => {
      clearScenarioSession()
    }
  }, [clearScenarioSession, hydrateSession, id, state?.question, token])

  // 这些 Hooks 必须在加载态和已加载态都执行，避免 React 在恢复会话后改变
  // Hooks 数量。线索数据本身不依赖 question，可以安全地在条件渲染前聚合。
  const clueReleases = useMemo(() => {
    const runs = messages.flatMap((message) => completedRuns[message.id] ?? message.response_meta.run_events ?? [])
    return collectProactiveClues([...runs, ...(activeRun?.events ?? [])])
  }, [activeRun?.events, completedRuns, messages])
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
    if (!userContent) return
    setContent('')
    try {
      await sendMessage(token, id, userContent)
    } catch (err) {
      void err
    }
  }

  function handleQuickAction(action: ScenarioAllowedAction) {
    void sendStructuredAction(token, id, action).catch(() => {})
  }

  async function submitAnswer() {
    if (!answer.trim()) return
    setSubmittingAnswer(true)
    setAnswerError('')
    setAnswerStatus('提交最终答案中')
    try {
      await api.submitScenarioAnswer(token, id, answer)
      navigate(`/scenarios/session/${id}/review`)
    } catch (err) {
      setAnswerError(err instanceof Error ? err.message : '提交答案失败')
      setAnswerStatus('提交答案失败')
    } finally {
      setSubmittingAnswer(false)
    }
  }

  async function quitSession() {
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
    revealed_clue_ids: [],
    investigation_state: {
      current_focus: '',
      has_current_hypothesis: false,
      collected_evidence_count: 0,
    },
    state_revision: 0,
    status: 'active',
  }
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
      const nextWidth = clamp(startWidth + moveEvent.clientX - startX, CONTEXT_WIDTH_MIN, CONTEXT_WIDTH_MAX)
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
            <span>状态版本 {activeSession.state_revision}</span>
            <span>已验证观察 {(activeSession.revealed_clue_ids ?? []).length}</span>
          </div>
          <InvestigationStatePanel
            state={activeSession.investigation_state}
            clueCount={clueReleases.length}
            observedCount={(activeSession.revealed_clue_ids ?? []).length}
          />
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
            <span>仅依据已公开信息回应，不展示隐藏答案或原始思维链。</span>
          </div>
          <button className="ghost-button compact" type="button" onClick={() => void quitSession()} disabled={isQuitting}>
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
              onQuickAction={message.id === messages[messages.length - 1]?.id ? handleQuickAction : undefined}
              quickActionDisabled={isSending || isQuitting}
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
              onQuickAction={handleQuickAction}
              quickActionDisabled={isSending || isQuitting}
            />
          )}
        </div>
        {sendError && <div className="inline-error chat-error">{sendError}</div>}
        <div className="composer">
          <textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="输入你的排查提问..." disabled={isSending || isQuitting} />
          <button className="icon-button filled" onClick={() => void send()} disabled={isSending || isQuitting} title="发送">
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
              aria-label="拖拽调整最终答案区高度"
              title="拖拽调整最终答案区高度"
            />
          )}
          <div className="scenario-answer-heading">
            <div>
              <strong>最终根因答案</strong>
              <span>{isAnswerOpen ? '支持 Markdown 结构化记录根因、证据、命令和修复验证。' : '默认收起，先把空间留给排查对话。'}</span>
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
                {isAnswerOpen ? '收起答案区' : '展开最终答案区'}
              </button>
              <button
                className="primary-button compact"
                onClick={() => void submitAnswer()}
                disabled={isQuitting || isSubmittingAnswer || !answer.trim()}
                aria-busy={isSubmittingAnswer}
                data-testid="submit-scenario-answer"
              >
                <CheckCircle2 size={16} />{isSubmittingAnswer ? '提交中' : '提交答案'}
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
                  if (answerStatus === '提交答案失败') {
                    setAnswerStatus('')
                  }
                }}
                disabled={isQuitting || isSubmittingAnswer}
                placeholder="用 Markdown 记录最终根因：现象、关键证据、验证命令、修复方案和回滚观察..."
                editorLabel="Markdown 最终答案"
                editorTestId="scenario-answer-editor"
                fileInputTestId="scenario-answer-markdown-file-input"
                previewEmptyText="预览区：输入最终答案后会显示 Markdown 排版效果。"
                previewNote="这是 Markdown 渲染预览，提交时仍会使用原始最终答案内容。"
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
              <strong>{answerStatus || '最终答案状态'}</strong>
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
                <strong>{clueLabel(clue.action)}</strong>
              </div>
              <p>{snapshotText(clue.result)}</p>
            </article>
          ))}
        </div>
      ) : (
        <p className="clue-release-empty">关键线索会在形成后固定显示在这里。</p>
      )}
    </section>
  )
}

function InvestigationStatePanel({
  state,
  clueCount,
  observedCount,
}: {
  state?: {
    current_focus?: string
    has_current_hypothesis: boolean
    collected_evidence_count: number
  }
  clueCount: number
  observedCount: number
}) {
  const focusLabel = scenarioFocusLabel(state?.current_focus)
  const evidenceCount = Math.max(state?.collected_evidence_count ?? 0, clueCount, observedCount)
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
          <span>已形成证据</span>
          <strong>{evidenceCount} 条</strong>
        </div>
        <div>
          <span>调查假设</span>
          <strong>{state?.has_current_hypothesis ? '已形成' : '尚未形成'}</strong>
        </div>
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
  return focus ? labels[focus] ?? '' : ''
}

function clueLabel(action: string) {
  if (action.startsWith('clue:')) return '主动线索'
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
