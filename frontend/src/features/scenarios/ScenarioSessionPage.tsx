import { useEffect, useState } from 'react'
import type { CSSProperties, PointerEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { Bot, CheckCircle2, ChevronDown, ChevronUp, FileText, Send } from 'lucide-react'
import { api } from '../../api/client'
import type { ScenarioQuestion } from '../../types'
import { EmptyState, Loading, MarkdownComposer } from '../../components/common'
import { MermaidRenderer } from '../../components/common/MermaidRenderer'
import { useToken } from '../../lib/auth'
import { redactSensitiveText } from '../../lib/redaction'
import { useScenarioSessionStore } from '../../stores/scenarioSessionStore'
import './ScenarioSessionPage.css'

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
  const streamingTurn = useScenarioSessionStore((store) => store.streamingTurn)
  const agentStages = useScenarioSessionStore((store) => store.agentStages)
  const completedAgentStages = useScenarioSessionStore((store) => store.completedAgentStages)
  const hydrateSession = useScenarioSessionStore((store) => store.hydrate)
  const sendMessage = useScenarioSessionStore((store) => store.sendMessage)
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

  useEffect(() => {
    void hydrateSession(token, id, { question: state?.question ?? null }).catch(() => {})
    return () => {
      clearScenarioSession()
    }
  }, [clearScenarioSession, hydrateSession, id, state?.question, token])

  if (isLoading && !question) {
    return <Loading title="恢复排查会话" />
  }

  if (!question) {
    return <EmptyState title="缺少会话上下文" description="请从排查工坊选择题目后进入会话。" action={<Link className="primary-button" to="/scenarios">返回题目列表</Link>} />
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
    hint_level: 1,
    status: 'active',
  }
  const snapshotText = (value = '') => question.is_sanitized ? redactSensitiveText(value) : value
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
          <MermaidRenderer code={snapshotText(question.content.architecture_diagram)} />
          <div className="clue-status">
            <span>轮次 {activeSession.current_turn}/{activeSession.max_turns}</span>
            <span>提示等级 {activeSession.hint_level}</span>
            <span>已揭示 {(activeSession.revealed_clue_ids ?? []).length}</span>
          </div>
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
            <span>AI 只按 Reveal Strategy 释放线索，不直接确认根因。</span>
          </div>
          <button className="ghost-button compact" type="button" onClick={() => void quitSession()} disabled={isQuitting}>
            {isQuitting ? '放弃中' : '放弃会话'}
          </button>
        </div>
        <div className="message-thread" data-testid="session-message-thread">
          {messages.map((message) => (
            <div className="turn" key={message.id}>
              <div className="user-msg">{message.user_content}</div>
              <div className={`assistant-msg ${message.response_meta.is_distractor ? 'distractor' : ''}`}>
                <Bot size={18} />
                <span>
                  {message.assistant_content}
                  {(completedAgentStages[message.id] ?? []).length > 0 && (
                    <span className="agent-stage-list completed" data-testid="agent-stage-list">
                      {completedAgentStages[message.id].map((stage) => (
                        <span key={stage.step || stage.message}>{stage.message || agentStageLabel(stage.step)}</span>
                      ))}
                    </span>
                  )}
                  {message.response_meta.agent_trace && (
                    <small className="agent-trace-summary">
                      Agent 已执行 {message.response_meta.agent_trace.tool_count} 个安全步骤
                    </small>
                  )}
                </span>
              </div>
            </div>
          ))}
          {streamingTurn && (
            <div className="turn">
              <div className="user-msg">{streamingTurn.userContent}</div>
              {agentStages.length > 0 && (
                <div className="agent-stage-list" data-testid="agent-stage-list" aria-live="polite">
                  {agentStages.map((stage) => (
                    <span key={stage.step || stage.message}>{stage.message || agentStageLabel(stage.step)}</span>
                  ))}
                </div>
              )}
              <div className="assistant-msg streaming">
                <Bot size={18} />
                <span>{streamingTurn.assistantContent || 'Agent 正在组织已允许释放的线索...'}</span>
              </div>
            </div>
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

function agentStageLabel(step: string) {
  switch (step) {
    case 'agent_intent':
      return '正在分析你的排查意图'
    case 'agent_policy':
      return '正在检查是否会泄露根因'
    case 'agent_clue':
      return '正在匹配可释放线索'
    case 'agent_hint':
      return '正在判断是否需要升级提示'
    case 'agent_reply':
      return '正在生成教学化回复'
    default:
      return 'Agent 正在处理'
  }
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
