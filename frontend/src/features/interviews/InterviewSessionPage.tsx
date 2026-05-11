import { useLayoutEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ChevronDown, Code2, Eye, List, Maximize2, Mic, Minimize2, Pencil, Quote, Radar, Send, Trash2, UploadCloud } from 'lucide-react'
import type { AgentTrace, InterviewQuestion, InterviewSession, TranscriptSuggestion, VoiceQualityResult } from '../../types'
import { EmptyState, MarkdownPreview, Metric } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { useInterviewSessionStore } from '../../stores/interviewSessionStore'
import './InterviewSessionPage.css'

const VOICE_AUDIO_ACCEPT = 'audio/aac,audio/flac,audio/mp4,audio/mpeg,audio/ogg,audio/wav,audio/webm,.aac,.flac,.m4a,.mp3,.ogg,.opus,.wav,.webm'
const VOICE_AUDIO_EXTENSIONS = new Set(['aac', 'flac', 'm4a', 'mp3', 'ogg', 'opus', 'wav', 'webm'])
const MARKDOWN_FILE_ACCEPT = '.md,.markdown,text/markdown,text/plain'
const MARKDOWN_FILE_EXTENSIONS = new Set(['md', 'markdown'])
const MARKDOWN_FILE_MAX_BYTES = 512 * 1024

export function InterviewSessionPage() {
  const token = useToken()
  const navigate = useNavigate()
  const { id = '' } = useParams()
  const location = useLocation()
  const state = location.state as { question?: InterviewQuestion; session?: InterviewSession } | null
  const answerEditorRef = useRef<HTMLTextAreaElement | null>(null)
  const pendingSelectionRef = useRef<{ start: number; end: number } | null>(null)
  const [answer, setAnswer] = useState('')
  const [voiceObjectUrl, setVoiceObjectUrl] = useState('')
  const [previewMode, setPreviewMode] = useState<'edit' | 'preview'>('edit')
  const [isMarkdownExpanded, setMarkdownExpanded] = useState(false)
  const [isTemplateExpanded, setTemplateExpanded] = useState(false)
  const question = useInterviewSessionStore((store) => store.question)
  const session = useInterviewSessionStore((store) => store.session)
  const isLoading = useInterviewSessionStore((store) => store.isLoading)
  const lastEvaluation = useInterviewSessionStore((store) => store.lastEvaluation)
  const agentTrace = useInterviewSessionStore((store) => store.agentTrace)
  const agentStageMessages = useInterviewSessionStore((store) => store.agentStageMessages)
  const isSubmitting = useInterviewSessionStore((store) => store.isSubmitting)
  const submitStatus = useInterviewSessionStore((store) => store.submitStatus)
  const streamPreview = useInterviewSessionStore((store) => store.streamPreview)
  const submitError = useInterviewSessionStore((store) => store.submitError)
  const voiceAsset = useInterviewSessionStore((store) => store.voiceAsset)
  const voiceStatus = useInterviewSessionStore((store) => store.voiceStatus)
  const voiceQuality = useInterviewSessionStore((store) => store.voiceQuality)
  const voiceTranscript = useInterviewSessionStore((store) => store.voiceTranscript)
  const voiceConfirmed = useInterviewSessionStore((store) => store.voiceConfirmed)
  const voiceDuration = useInterviewSessionStore((store) => store.voiceDuration)
  const uploadProgress = useInterviewSessionStore((store) => store.uploadProgress)
  const isVoiceBusy = useInterviewSessionStore((store) => store.isVoiceBusy)
  const submitType = useInterviewSessionStore((store) => store.submitType)
  const voiceSessionExpired = useInterviewSessionStore((store) => store.voiceSessionExpired)
  const hydrateSession = useInterviewSessionStore((store) => store.hydrate)
  const submitAnswer = useInterviewSessionStore((store) => store.submitAnswer)
  const uploadVoiceFile = useInterviewSessionStore((store) => store.uploadVoiceFile)
  const confirmVoiceTranscript = useInterviewSessionStore((store) => store.confirmVoiceTranscript)
  const updateVoiceEditedState = useInterviewSessionStore((store) => store.updateVoiceEditedState)
  const rejectVoiceDraft = useInterviewSessionStore((store) => store.rejectVoiceDraft)
  const setSubmitFeedback = useInterviewSessionStore((store) => store.setSubmitFeedback)
  const clearVoiceDraftState = useInterviewSessionStore((store) => store.clearVoiceDraft)
  const clearInterviewSession = useInterviewSessionStore((store) => store.clear)

  useLayoutEffect(() => {
    const selection = pendingSelectionRef.current
    const textarea = answerEditorRef.current
    if (!selection || !textarea) return
    pendingSelectionRef.current = null
    textarea.focus()
    textarea.setSelectionRange(selection.start, selection.end)
  }, [answer])

  useLayoutEffect(() => {
    void hydrateSession(token, id, {
      question: state?.question ?? null,
      session: state?.session ?? null,
    }).catch(() => {})

    return () => {
      clearInterviewSession()
    }
  }, [clearInterviewSession, hydrateSession, id, state?.question, state?.session, token])

  useLayoutEffect(() => {
    return () => {
      if (voiceObjectUrl) {
        URL.revokeObjectURL(voiceObjectUrl)
      }
    }
  }, [voiceObjectUrl])

  if (isLoading && !question) {
    return <EmptyState title="恢复面试会话中" description="正在拉取会话详情，请稍候。" action={<Link className="primary-button" to="/interviews">返回面试舱</Link>} />
  }

  if (!question || !session) {
    return <EmptyState title="缺少面试上下文" description="请从面试舱创建一次新面试。" action={<Link className="primary-button" to="/interviews">返回面试舱</Link>} />
  }

  async function submit() {
    try {
      const res = await submitAnswer(token, id, {
        answer,
        voiceAsset,
        voiceQuality,
        voiceTranscript,
        voiceDuration,
      })
      if (!res.session_status) return
      setAnswer('')
      clearVoiceDraft()
      if (res.session_status === 'final_evaluated') {
        navigate(`/interviews/session/${id}/report`)
      }
    } catch (err) {
      void err
    }
  }

  async function handleVoiceFile(file: File | null) {
    if (!file) return
    clearVoiceDraft()
    const validationError = validateVoiceFile(file)
    if (validationError) {
      rejectVoiceDraft(validationError)
      return
    }
    try {
      const objectUrl = URL.createObjectURL(file)
      setVoiceObjectUrl(objectUrl)
      const transcript = await uploadVoiceFile(token, id, file)
      setAnswer(transcript)
    } catch (err) {
      const message = voiceUploadErrorMessage(err)
      if (isInterviewSessionUnavailableError(message)) {
        clearVoiceDraft()
        return
      }
      rejectVoiceDraft(message)
    }
  }

  async function handleMarkdownFile(file: File | null) {
    if (!file) return
    const validationError = validateMarkdownFile(file)
    if (validationError) {
      setSubmitFeedback({ submitError: validationError, submitStatus: 'Markdown 导入失败' })
      return
    }
    try {
      const content = await file.text()
      setAnswer(content)
      setPreviewMode('preview')
      setSubmitFeedback({
        submitError: '',
        streamPreview: '',
        submitStatus: `已导入 Markdown：${file.name}`,
      })
      clearVoiceDraft()
    } catch {
      setSubmitFeedback({ submitError: 'Markdown 文件读取失败，请重新选择', submitStatus: 'Markdown 导入失败' })
    }
  }

  function applyEditorSnippet(snippet: string) {
    const textarea = answerEditorRef.current
    if (!textarea) {
      setAnswer((prev) => insertMarkdown(prev, snippet))
      return
    }
    const result = insertSnippetAtSelection(textarea.value, textarea.selectionStart, textarea.selectionEnd, snippet)
    commitAnswerValue(result.value, result.selectionStart, result.selectionEnd)
    if (submitType === 'voice') {
      updateVoiceEditedState()
    }
  }

  function handleAnswerKeyDown(event: import('react').KeyboardEvent<HTMLTextAreaElement>) {
    const transform = getEditorKeyTransform(event)
    if (!transform) return

    const textarea = event.currentTarget
    const result = transformAnswerText(textarea.value, textarea.selectionStart, textarea.selectionEnd, transform)
    if (!result) return

    event.preventDefault()
    commitAnswerValue(result.value, result.selectionStart, result.selectionEnd)
    if (submitType === 'voice') {
      updateVoiceEditedState()
    }
  }

  function commitAnswerValue(value: string, selectionStart: number, selectionEnd: number) {
    pendingSelectionRef.current = { start: selectionStart, end: selectionEnd }
    setAnswer(value)
  }

  function clearVoiceDraft() {
    if (voiceObjectUrl) {
      URL.revokeObjectURL(voiceObjectUrl)
    }
    setVoiceObjectUrl('')
    clearVoiceDraftState()
  }

  const voiceRejected = voiceQuality?.status === 'rejected'
  const voiceNeedsConfirm = submitType === 'voice' && Boolean(voiceAsset) && !voiceConfirmed
  const rejectedVoiceDraftNeedsEdit = Boolean(voiceTranscript) && voiceRejected && answer.trim() === voiceTranscript.trim()
  const canSubmit = Boolean(answer.trim()) && !isVoiceBusy && !isSubmitting && !rejectedVoiceDraftNeedsEdit && !(submitType === 'voice' && (voiceRejected || voiceNeedsConfirm))
  const transcriptSuggestions = voiceQuality?.transcript_suggestions ?? voiceQuality?.suggestions ?? []
  const voiceTranscriptEdited = Boolean(voiceTranscript) && answer.trim() !== voiceTranscript.trim()

  function applyTranscriptSuggestion(suggestion: TranscriptSuggestion) {
    const nextValue = replaceSuggestionText(answer, suggestion)
    if (nextValue === answer) return
    setAnswer(nextValue)
    setSubmitFeedback({ submitError: '' })
    updateVoiceEditedState(`已应用术语建议：${suggestion.original} → ${suggestion.suggested}，请重新确认转写`)
  }

  function applyAllTranscriptSuggestions() {
    const nextValue = transcriptSuggestions.reduce((current, suggestion) => replaceSuggestionText(current, suggestion), answer)
    if (nextValue === answer) return
    setAnswer(nextValue)
    setSubmitFeedback({ submitError: '' })
    updateVoiceEditedState('已应用全部术语建议，请重新确认转写')
  }

  return (
    <section className="interview-layout interview-session-page">
      <div className="interview-main-column">
        <section className="panel interview-question">
          <div className="scenario-meta">
            <span>{domainLabel(question.domain)}</span>
            <span>{question.difficulty}</span>
            <span>第 {session.current_round} 轮</span>
          </div>
          <h2>{question.title}</h2>
          <p>{session.status.startsWith('follow_up') ? session.follow_up_question : question.description}</p>
        </section>
      </div>
      <section className="panel answer-panel">
        <div className="panel-title"><Send size={18} /> 回答</div>
        <div className="voice-upload-row">
          <label className="ghost-button compact">
            <UploadCloud size={16} />
            {voiceAsset ? '重新上传' : '上传语音'}
            <input
              type="file"
              accept={VOICE_AUDIO_ACCEPT}
              disabled={isVoiceBusy}
              data-testid="voice-file-input"
              onChange={(event) => void handleVoiceFile(event.target.files?.[0] ?? null)}
            />
          </label>
          <span><Mic size={14} />{voiceStatus || '可上传音频生成转写草稿'}</span>
          {voiceAsset && (
            <button className="icon-button compact-icon" type="button" onClick={clearVoiceDraft} aria-label="清除语音草稿">
              <Trash2 size={15} />
            </button>
          )}
        </div>
        {(isVoiceBusy || uploadProgress > 0) && (
          <div className="voice-progress" aria-label="语音处理进度">
            <span style={{ width: `${uploadProgress}%` }} />
          </div>
        )}
        {(voiceAsset || voiceQuality) && (
          <div className={`voice-quality ${voiceQuality?.status ?? 'draft_ready'}`}>
            <div>
              <strong>{voiceQualityLabel(voiceQuality)}</strong>
              <span>{voiceQualityHint(voiceQuality)}</span>
            </div>
            {voiceObjectUrl && (
              <audio controls src={voiceObjectUrl} aria-label="语音答案回放" />
            )}
            {voiceQuality?.keyword_hits?.length ? <small>命中：{voiceQuality.keyword_hits.join('、')}</small> : null}
            {voiceQuality?.reasons?.length ? <small>{voiceQuality.reasons.join('；')}</small> : null}
            {voiceTranscript && (
              <div className="voice-suggestion-box" data-testid="voice-transcript-suggestions">
                <div className="voice-suggestion-header">
                  <div className="voice-suggestion-copy">
                    <strong>术语建议</strong>
                    <span>
                      {transcriptSuggestions.length > 0
                        ? '检测到疑似技术术语谐音或大小写不规范。可逐条应用建议，原始转写会继续保留在证据链中。'
                        : '当前未检测到需要确认的术语，你可以核对转写后直接确认提交。'}
                    </span>
                    <small className={`voice-suggestion-state ${voiceConfirmed ? 'confirmed' : 'pending'}`}>
                      {voiceConfirmed
                        ? '当前转写已确认，可直接提交评分。'
                        : transcriptSuggestions.length > 0
                          ? (voiceTranscriptEdited ? '转写文本已修改，请重新确认' : '可先应用建议，再确认当前转写文本。')
                          : '核对无误后点击“确认转写文本”。'}
                    </small>
                  </div>
                  {transcriptSuggestions.length > 0 && (
                    <button
                      className="ghost-button compact"
                      type="button"
                      disabled={voiceRejected || isSubmitting}
                      data-testid="apply-all-transcript-suggestions"
                      onClick={applyAllTranscriptSuggestions}
                    >
                      全部应用建议
                    </button>
                  )}
                </div>
                {transcriptSuggestions.length > 0 ? (
                  <div className="voice-suggestion-list">
                    {transcriptSuggestions.map((suggestion, index) => (
                      <div
                        className="voice-suggestion-item"
                        key={`${suggestion.original}-${suggestion.suggested}-${index}`}
                        data-testid={`transcript-suggestion-item-${index}`}
                      >
                        <div>
                          <strong>{suggestion.original} → {suggestion.suggested}</strong>
                          <span>{suggestion.reason || '建议按术语原文确认后再提交。'}</span>
                        </div>
                        <button
                          className="ghost-button compact"
                          type="button"
                          disabled={voiceRejected || isSubmitting}
                          data-testid={`apply-transcript-suggestion-${index}`}
                          aria-label={`应用建议 ${suggestion.suggested}`}
                          onClick={() => applyTranscriptSuggestion(suggestion)}
                        >
                          应用
                        </button>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="voice-suggestion-empty" data-testid="voice-transcript-suggestions-empty">
                    未检测到需要确认的术语。
                  </div>
                )}
              </div>
            )}
            {voiceTranscript && (
              <div className="voice-confirm-box">
                <strong>请确认转写文本</strong>
                <span>下方回答框中的内容会作为评分依据。请先核对并按需修改，再确认提交。</span>
                <button
                  className="ghost-button compact"
                  type="button"
                  disabled={voiceRejected || isSubmitting}
                  data-testid="confirm-voice-transcript"
                  onClick={confirmVoiceTranscript}
                >
                  确认转写文本
                </button>
              </div>
            )}
          </div>
        )}
        <div className={`markdown-composer ${isMarkdownExpanded ? 'expanded' : ''}`}>
          <div className="markdown-toolbar" aria-label="Markdown 工具栏">
            <ToolbarSelect
              icon={<Pencil size={14} />}
              label="标题"
              ariaLabel="选择标题级别"
              options={headingOptions}
              onSelect={applyEditorSnippet}
            />
            <ToolbarSelect
              icon={<List size={14} />}
              label="列表"
              ariaLabel="选择列表类型"
              options={listOptions}
              onSelect={applyEditorSnippet}
            />
            <ToolbarSelect
              icon={<Code2 size={14} />}
              label="代码块"
              ariaLabel="选择代码块语言"
              options={codeBlockOptions}
              onSelect={applyEditorSnippet}
            />
            <ToolbarSelect
              icon={<Code2 size={14} />}
              label="Mermaid"
              ariaLabel="选择 Mermaid 图类型"
              options={mermaidOptions}
              onSelect={applyEditorSnippet}
            />
            <button type="button" onClick={() => applyEditorSnippet('>')}>
              <Quote size={14} />引用
            </button>
            <label className="markdown-file-button">
              <UploadCloud size={14} />导入 MD
              <input
                type="file"
                accept={MARKDOWN_FILE_ACCEPT}
                disabled={isSubmitting}
                aria-label="导入 Markdown 文件"
                data-testid="markdown-file-input"
                onChange={(event) => {
                  void handleMarkdownFile(event.target.files?.[0] ?? null)
                  event.target.value = ''
                }}
              />
            </label>
            <button className="preview-toggle" type="button" onClick={() => setPreviewMode((mode) => (mode === 'edit' ? 'preview' : 'edit'))}>
              <Eye size={14} />{previewMode === 'edit' ? '预览' : '编辑'}
            </button>
            <button
              className="expand-toggle"
              type="button"
              aria-pressed={isMarkdownExpanded}
              onClick={() => setMarkdownExpanded((expanded) => !expanded)}
            >
              {isMarkdownExpanded ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
              {isMarkdownExpanded ? '退出全屏' : '全屏'}
            </button>
          </div>
          {previewMode === 'preview' && (
            <div className="markdown-preview-hint" role="note">
              这是 Markdown 渲染预览，提交时仍会使用原始回答内容。
            </div>
          )}
          <div className={`markdown-workspace ${previewMode === 'preview' ? 'preview-only' : ''}`}>
            <textarea
              ref={answerEditorRef}
              value={answer}
              onChange={(event) => {
                setAnswer(event.target.value)
                if (submitType === 'voice') {
                  updateVoiceEditedState()
                }
              }}
              placeholder="用结构化方式回答，支持 Markdown：定位路径、关键命令、处理方案、回滚验证..."
              disabled={isSubmitting}
              aria-label="Markdown 回答"
              data-testid="interview-answer-editor"
              onKeyDown={handleAnswerKeyDown}
            />
            <div className="markdown-preview-panel">
              <div className="markdown-preview-header">
                <span><Eye size={14} /> Markdown 实时预览</span>
                <small>{previewMode === 'preview' ? '当前仅显示排版效果' : '编辑内容会同步渲染到这里'}</small>
              </div>
              <MarkdownPreview content={answer} emptyText="预览区：导入或输入 Markdown 后会显示排版效果。" />
            </div>
          </div>
        </div>
        {(isSubmitting || submitStatus || submitError) && (
          <div className={`stream-status ${submitError ? 'error' : ''}`} role="status" aria-live="polite" data-testid="interview-stream-feedback">
            <details className="interview-feedback-details">
              <summary>
                <span>
                  <strong>{submitStatus || 'AI 正在生成本轮反馈'}</strong>
                  {streamPreview && <small>{compactFeedbackSummary(streamPreview)}</small>}
                </span>
                <span className="feedback-toggle-text" data-open-label="收起评分详情" data-closed-label="展开评分详情" />
              </summary>
              <div className="interview-feedback-body">
                {streamPreview && <pre>{streamPreview}</pre>}
                {agentStageMessages.length > 0 && (
                  <ul className="stream-step-list agent-stage-progress" data-testid="interview-agent-stage-list">
                    {agentStageMessages.map((message) => (
                      <li key={message} className="stream-step stream-step-active">
                        {sanitizeAgentText(message)}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </details>
            {submitError && <span>{submitError}</span>}
            {voiceSessionExpired && (
              <button
                className="ghost-button compact"
                type="button"
                data-testid="interview-session-restart"
                onClick={() => navigate('/interviews')}
              >
                重新开始面试
              </button>
            )}
          </div>
        )}
        <button
          className="primary-button"
          disabled={!canSubmit}
          onClick={() => void submit()}
          aria-busy={isSubmitting}
          data-testid="submit-interview-answer"
        >
          <Send size={18} />{isSubmitting ? `生成${submitType === 'voice' ? '语音' : ''}评分中` : `提交${submitType === 'voice' ? '语音' : ''}回答`}
        </button>
      </section>
      {lastEvaluation && (
        <section className="panel">
          <div className="panel-title"><Radar size={18} /> 本轮评分</div>
          <div className="metric-row compact-metrics">
            <Metric label="总分" value={lastEvaluation.total_score} />
            <Metric label="是否通过" value={lastEvaluation.is_passed ? '通过' : '待改进'} />
            <Metric label="追问" value={lastEvaluation.follow_up_triggered ? '已触发' : '无'} />
          </div>
          <SafeAgentSummary trace={agentTrace} stageMessages={agentStageMessages} />
        </section>
      )}
      <section className={`panel answer-template-panel ${isTemplateExpanded ? 'expanded' : ''}`}>
        <button
          className="answer-template-toggle"
          type="button"
          aria-expanded={isTemplateExpanded}
          aria-controls="answer-template-grid"
          onClick={() => setTemplateExpanded((expanded) => !expanded)}
        >
          <span><Pencil size={18} /> 回答模板</span>
          <span className="template-toggle-hint">{isTemplateExpanded ? '收起' : '展开定位路径、关键命令、处理与回滚提示'}</span>
          <ChevronDown size={16} aria-hidden="true" />
        </button>
        <div
          id="answer-template-grid"
          className="answer-template-grid"
          data-testid="answer-template-grid"
          hidden={!isTemplateExpanded}
        >
          {answerTemplates.map((item) => {
            const applied = isTemplateApplied(answer, item)
            return (
              <button
                key={item.title}
                className={`template-step ${applied ? 'active' : ''}`}
                type="button"
                disabled={applied || isSubmitting}
                aria-pressed={applied}
                onClick={() => {
                  setAnswer((prev) => (isTemplateApplied(prev, item) ? prev : insertMarkdown(prev, item.markdown)))
                  if (submitType === 'voice') {
                    updateVoiceEditedState()
                  }
                }}
              >
                <strong>{item.title}</strong>
                <span>{applied ? '已添加到回答区，可直接编辑内容。' : item.hint}</span>
              </button>
            )
          })}
        </div>
      </section>
    </section>
  )
}

function ToolbarSelect({
  icon,
  label,
  ariaLabel,
  options,
  onSelect,
}: {
  icon: import('react').ReactNode
  label: string
  ariaLabel: string
  options: MarkdownToolbarOption[]
  onSelect: (snippet: string) => void
}) {
  const [open, setOpen] = useState(false)

  return (
    <div
      className={`toolbar-menu ${open ? 'open' : ''}`}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setOpen(false)
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          setOpen(false)
        }
      }}
    >
      <button
        className="toolbar-menu-trigger"
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{icon}{label}</span>
        <ChevronDown size={14} />
      </button>
      {open && (
        <div className="toolbar-menu-options" role="menu">
          {options.map((option) => (
            <button
              key={option.label}
              type="button"
              role="menuitem"
              aria-label={option.label}
              onClick={() => {
                onSelect(option.snippet)
                setOpen(false)
              }}
            >
              <span className="menu-option-label">
                {option.badge && <span className={`language-badge ${option.badgeTone ?? 'neutral'}`} aria-hidden="true">{option.badge}</span>}
                <span>{option.displayLabel ?? option.label}</span>
              </span>
              {option.shortcut && <kbd aria-hidden="true">{option.shortcut}</kbd>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

type MarkdownToolbarOption = {
  label: string
  snippet: string
  displayLabel?: string
  badge?: string
  badgeTone?: string
  shortcut?: string
}

const headingOptions: MarkdownToolbarOption[] = [
  { label: '一级标题', displayLabel: '一级', snippet: '#', shortcut: 'Ctrl+1' },
  { label: '二级标题', displayLabel: '二级', snippet: '##', shortcut: 'Ctrl+2' },
  { label: '三级标题', displayLabel: '三级', snippet: '###', shortcut: 'Ctrl+3' },
]

const listOptions: MarkdownToolbarOption[] = [
  { label: '无序列表', displayLabel: '● 无序', snippet: '- ', shortcut: 'Ctrl+Shift+]' },
  { label: '有序列表', displayLabel: '1. 有序', snippet: '1. ', shortcut: 'Ctrl+Shift+[' },
]

const codeBlockOptions: MarkdownToolbarOption[] = [
  { label: '纯文本', displayLabel: '文本', badge: '文', badgeTone: 'neutral', snippet: '```\n\n```', shortcut: 'Ctrl+Shift+K' },
  { label: 'SQL', badge: 'SQL', badgeTone: 'blue', snippet: '```sql\n\n```' },
  { label: 'Python', badge: 'PY', badgeTone: 'yellow', snippet: '```python\n\n```' },
  { label: 'Java', badge: 'JV', badgeTone: 'red', snippet: '```java\n\n```' },
  { label: 'JavaScript', displayLabel: 'JS', badge: 'JS', badgeTone: 'yellow', snippet: '```javascript\n\n```' },
  { label: 'TypeScript', displayLabel: 'TS', badge: 'TS', badgeTone: 'blue', snippet: '```typescript\n\n```' },
  { label: 'Go', badge: 'GO', badgeTone: 'cyan', snippet: '```go\n\n```' },
  { label: 'Shell', badge: 'SH', badgeTone: 'green', snippet: '```bash\n\n```' },
  { label: 'JSON', badge: '{}', badgeTone: 'neutral', snippet: '```json\n\n```' },
  { label: 'YAML', badge: 'YML', badgeTone: 'purple', snippet: '```yaml\n\n```' },
]

const mermaidOptions: MarkdownToolbarOption[] = [
  { label: '流程图', snippet: '```mermaid\ngraph LR\n\n```' },
  { label: '时序图', snippet: '```mermaid\nsequenceDiagram\n\n```' },
  { label: '状态图', snippet: '```mermaid\nstateDiagram-v2\n\n```' },
  { label: '思维导图', snippet: '```mermaid\nmindmap\n\n```' },
]

const answerTemplates = [
  {
    title: '定位路径',
    hint: '先说从哪些现象和指标开始。',
    markdown: '## 定位路径\n- 确认影响范围和时间窗口\n- 对比慢查询日志、连接池、CPU、IO、变更记录\n',
  },
  {
    title: '关键命令',
    hint: '写出可执行的排查动作。',
    markdown: '## 关键命令\n```sql\nSHOW FULL PROCESSLIST;\nEXPLAIN SELECT ...;\n```\n',
  },
  {
    title: '处理与回滚',
    hint: '补齐修复、验证和兜底方案。',
    markdown: '## 处理与回滚\n- 先限流或降级保护接口\n- 灰度修复索引或 SQL\n- 观察 P95、错误率和慢查询数量\n',
  },
]

function insertMarkdown(current: string, snippet: string) {
  const trimmed = current.trimEnd()
  return `${trimmed}${trimmed ? '\n\n' : ''}${snippet}`
}

function insertSnippetAtSelection(value: string, selectionStart: number, selectionEnd: number, snippet: string) {
  const before = value.slice(0, selectionStart)
  const selected = value.slice(selectionStart, selectionEnd)
  const after = value.slice(selectionEnd)
  const prefix = before && !before.endsWith('\n') ? '\n\n' : ''
  const suffix = after && !after.startsWith('\n') ? '\n\n' : ''
  const content = selected ? snippet.replace(/\n\n(?=```$)/, `\n${selected}\n`) : snippet
  const nextValue = `${before}${prefix}${content}${suffix}${after}`
  const insertedStart = before.length + prefix.length
  const cursor = insertedStart + cursorOffsetForSnippet(content)
  return {
    value: nextValue,
    selectionStart: cursor,
    selectionEnd: cursor,
  }
}

function cursorOffsetForSnippet(snippet: string) {
  const doubleNewline = snippet.indexOf('\n\n')
  if (doubleNewline >= 0) return snippet.length
  return snippet.length
}

type EditorKeyTransform =
  | { type: 'indent'; outdent: boolean }
  | { type: 'heading'; level: 1 | 2 | 3 }
  | { type: 'list'; ordered: boolean }
  | { type: 'code' }
  | { type: 'quote' }
  | { type: 'listEnter' }

function getEditorKeyTransform(event: import('react').KeyboardEvent<HTMLTextAreaElement>): EditorKeyTransform | null {
  const key = event.key.toLowerCase()
  const commandKey = event.ctrlKey || event.metaKey
  if (event.key === 'Tab') {
    return { type: 'indent', outdent: event.shiftKey }
  }
  if (event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) {
    return { type: 'listEnter' }
  }
  if (!commandKey || event.altKey) return null
  if (!event.shiftKey && (event.key === '1' || event.key === '2' || event.key === '3')) {
    return { type: 'heading', level: Number(event.key) as 1 | 2 | 3 }
  }
  if (event.shiftKey && (event.key === ']' || event.code === 'BracketRight')) {
    return { type: 'list', ordered: false }
  }
  if (event.shiftKey && (event.key === '[' || event.code === 'BracketLeft')) {
    return { type: 'list', ordered: true }
  }
  if (event.shiftKey && key === 'k') {
    return { type: 'code' }
  }
  if (event.shiftKey && key === 'q') {
    return { type: 'quote' }
  }
  return null
}

function transformAnswerText(value: string, selectionStart: number, selectionEnd: number, transform: EditorKeyTransform) {
  switch (transform.type) {
    case 'indent':
      return transform.outdent
        ? outdentAnswerText(value, selectionStart, selectionEnd)
        : indentAnswerText(value, selectionStart, selectionEnd)
    case 'heading':
      return transformCurrentLine(value, selectionStart, '#'.repeat(transform.level), /^#{1,6}\s*/)
    case 'list':
      return transformCurrentLine(value, selectionStart, transform.ordered ? '1.' : '-', /^(\s*)(?:[-*+]\s*|\d+\.\s*)/)
    case 'code':
      return wrapSelectionOrLine(value, selectionStart, selectionEnd, '```\n', '\n```')
    case 'quote':
      return transformCurrentLine(value, selectionStart, '>', /^>\s*/)
    case 'listEnter':
      return continueListOnEnter(value, selectionStart, selectionEnd)
    default:
      return { value, selectionStart, selectionEnd }
  }
}

function continueListOnEnter(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart !== selectionEnd) return null
  if (selectionStart < value.length && value[selectionStart] !== '\n') return null
  const range = getLineRange(value, selectionStart, selectionEnd)
  const beforeCursor = value.slice(range.start, selectionStart)
  const ordered = /^(\s*)(\d+)\.\s?(.*)$/.exec(beforeCursor)
  if (ordered) {
    const [, indent, numberText, content] = ordered
    if (!content.trim()) {
      return handleEmptyListEnter(value, range, indent, {
        markerPattern: /^(\s*)(\d+)\.\s?(.*)$/,
      })
    }
    const nextMarker = `${indent}${Number(numberText) + 1}. `
    return insertAtCursor(value, selectionStart, `\n${nextMarker}`)
  }
  const unordered = /^(\s*)([-*+])\s?(.*)$/.exec(beforeCursor)
  if (unordered) {
    const [, indent, bullet, content] = unordered
    if (!content.trim()) {
      return handleEmptyListEnter(value, range, indent, {
        markerPattern: /^(\s*)([-*+])\s?(.*)$/,
      })
    }
    return insertAtCursor(value, selectionStart, `\n${indent}${bullet} `)
  }
  return null
}

function handleEmptyListEnter(
  value: string,
  range: { start: number; end: number },
  indent: string,
  list: { markerPattern: RegExp },
) {
  const previous = getPreviousLine(value, range.start)
  if (previous) {
    const previousMatch = list.markerPattern.exec(previous.text)
    if (previousMatch && !String(previousMatch[3] ?? '').trim()) {
      const nextValue = `${value.slice(0, previous.start)}${indent}${value.slice(range.end)}`
      const nextCursor = previous.start + indent.length
      return { value: nextValue, selectionStart: nextCursor, selectionEnd: nextCursor }
    }
    if (previousMatch) {
      return removeCurrentLineMarker(value, range.start, range.end, indent)
    }
  }
  return removeCurrentLineMarker(value, range.start, range.end, indent)
}

function getPreviousLine(value: string, lineStart: number) {
  if (lineStart <= 0) return null
  const end = lineStart - 1
  const start = value.lastIndexOf('\n', Math.max(0, end - 1)) + 1
  return { start, end, text: value.slice(start, end) }
}

function insertAtCursor(value: string, cursor: number, inserted: string) {
  const nextCursor = cursor + inserted.length
  return {
    value: `${value.slice(0, cursor)}${inserted}${value.slice(cursor)}`,
    selectionStart: nextCursor,
    selectionEnd: nextCursor,
  }
}

function removeCurrentLineMarker(value: string, lineStart: number, lineEnd: number, replacement: string) {
  const nextValue = `${value.slice(0, lineStart)}${replacement}${value.slice(lineEnd)}`
  const nextCursor = lineStart + replacement.length
  return {
    value: nextValue,
    selectionStart: nextCursor,
    selectionEnd: nextCursor,
  }
}

function indentAnswerText(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart === selectionEnd) {
    return {
      value: `${value.slice(0, selectionStart)}  ${value.slice(selectionEnd)}`,
      selectionStart: selectionStart + 2,
      selectionEnd: selectionStart + 2,
    }
  }
  return transformSelectedLines(value, selectionStart, selectionEnd, (line) => `  ${line}`)
}

function outdentAnswerText(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart === selectionEnd) {
    const beforeCursor = value.slice(Math.max(0, selectionStart - 2), selectionStart)
    if (beforeCursor === '  ') {
      return {
        value: `${value.slice(0, selectionStart - 2)}${value.slice(selectionStart)}`,
        selectionStart: selectionStart - 2,
        selectionEnd: selectionStart - 2,
      }
    }
    if (value[selectionStart - 1] === '\t') {
      return {
        value: `${value.slice(0, selectionStart - 1)}${value.slice(selectionStart)}`,
        selectionStart: selectionStart - 1,
        selectionEnd: selectionStart - 1,
      }
    }
  }
  return transformSelectedLines(value, selectionStart, selectionEnd, (line) => line.replace(/^( {1,2}|\t)/, ''))
}

function transformSelectedLines(value: string, selectionStart: number, selectionEnd: number, transformLine: (line: string) => string) {
  const range = getLineRange(value, selectionStart, selectionEnd)
  const block = value.slice(range.start, range.end)
  const nextBlock = block.split('\n').map(transformLine).join('\n')
  const nextValue = `${value.slice(0, range.start)}${nextBlock}${value.slice(range.end)}`
  const delta = nextBlock.length - block.length
  return {
    value: nextValue,
    selectionStart: Math.max(range.start, selectionStart + Math.min(0, delta)),
    selectionEnd: Math.max(range.start, selectionEnd + delta),
  }
}

function transformCurrentLine(value: string, cursor: number, marker: string, stripPattern: RegExp) {
  const range = getLineRange(value, cursor, cursor)
  const line = value.slice(range.start, range.end)
  const stripped = line.replace(stripPattern, '')
  const nextLine = stripped ? `${marker} ${stripped}` : `${marker} `
  const nextValue = `${value.slice(0, range.start)}${nextLine}${value.slice(range.end)}`
  const nextCursor = range.start + nextLine.length
  return { value: nextValue, selectionStart: nextCursor, selectionEnd: nextCursor }
}

function wrapSelection(value: string, selectionStart: number, selectionEnd: number, before: string, after: string) {
  const selected = value.slice(selectionStart, selectionEnd)
  const nextValue = `${value.slice(0, selectionStart)}${before}${selected}${after}${value.slice(selectionEnd)}`
  const innerStart = selectionStart + before.length
  const innerEnd = innerStart + selected.length
  return {
    value: nextValue,
    selectionStart: selected ? innerStart : innerStart,
    selectionEnd: selected ? innerEnd : innerStart,
  }
}

function wrapSelectionOrLine(value: string, selectionStart: number, selectionEnd: number, before: string, after: string) {
  if (selectionStart !== selectionEnd) {
    return wrapSelection(value, selectionStart, selectionEnd, before, after)
  }
  const range = getLineRange(value, selectionStart, selectionEnd)
  if (range.start === range.end) {
    return wrapSelection(value, selectionStart, selectionEnd, before, after)
  }
  return wrapSelection(value, range.start, range.end, before, after)
}

function getLineRange(value: string, selectionStart: number, selectionEnd: number) {
  const start = value.lastIndexOf('\n', Math.max(0, selectionStart - 1)) + 1
  const endLineBreak = value.indexOf('\n', selectionEnd)
  const end = endLineBreak === -1 ? value.length : endLineBreak
  return { start, end }
}

function isTemplateApplied(answer: string, template: { title: string; markdown: string }) {
  const heading = `## ${template.title}`
  return answer.includes(heading) || answer.includes(template.markdown.trim())
}

function validateMarkdownFile(file: File) {
  if (file.size <= 0) {
    return 'Markdown 文件为空，请重新选择'
  }
  if (file.size > MARKDOWN_FILE_MAX_BYTES) {
    return 'Markdown 文件不能超过 512KB'
  }
  const extension = file.name.split('.').pop()?.toLowerCase()
  const mimeType = file.type.trim().toLowerCase()
  if (!extension || !MARKDOWN_FILE_EXTENSIONS.has(extension)) {
    return '请上传 .md 或 .markdown 文件'
  }
  if (mimeType && mimeType !== 'text/markdown' && mimeType !== 'text/plain') {
    return '请上传 Markdown 文本文件'
  }
  return ''
}

function SafeAgentSummary({
  trace,
  stageMessages,
}: {
  trace: AgentTrace | null
  stageMessages: string[]
}) {
  const safeStages = trace?.steps?.length
    ? trace.steps.map((step, index) => ({
      label: `安全步骤 ${index + 1}`,
      detail: sanitizeAgentText(step.summary),
    }))
    : stageMessages.map((message, index) => ({
      label: `安全步骤 ${index + 1}`,
      detail: sanitizeAgentText(message),
    }))

  if (!safeStages.length) return null

  const count = trace?.steps?.length ?? safeStages.length
  return (
    <div className="agent-stage-list completed" data-testid="interview-agent-summary">
      <strong className="agent-trace-summary">Agent 已执行 {count} 个安全步骤</strong>
      {safeStages.map((item) => (
        <span key={item.label}>
          {item.label}
          {item.detail ? ` · ${item.detail}` : ''}
        </span>
      ))}
    </div>
  )
}

function compactFeedbackSummary(value: string) {
  const normalized = value.replace(/\s+/g, ' ').trim()
  const score = normalized.match(/总分[：:\s]*([0-9]+)\s*分?/)
  const highlight = normalized.match(/亮点[：:]\s*([^。；;]+)/)
  if (score?.[1] && highlight?.[1]) {
    return `总分 ${score[1]} 分 · ${highlight[1]}`
  }
  if (score?.[1]) return `总分 ${score[1]} 分`
  return normalized.slice(0, 72)
}

function sanitizeAgentText(value: string) {
  return value
    .replace(/\b(?:reference_answer|standard_procedure|root_cause|metadata|tool|tool_args|arguments|agent_trace)\b/gi, '安全信息')
    .replace(/\s+/g, ' ')
    .trim()
}

function voiceQualityLabel(quality: VoiceQualityResult | null) {
  if (!quality) return '等待语音质检'
  if (quality.status === 'rejected') return '语音不可提交'
  if (quality.status === 'needs_review') return '语音需确认'
  return '语音可提交'
}

function voiceQualityHint(quality: VoiceQualityResult | null) {
  if (!quality) return '上传后会检查语言、转写质量和题目相关性。'
  if (quality.status === 'rejected') {
    const reasonText = quality.reasons?.join('；') ?? ''
    if (isSTTServiceIssue(reasonText)) return '转写服务暂不可用，本次不会生成评分或追问；请稍后重试或改为文本回答。'
    if (reasonText.includes('未生成转写文本')) return '本次没有可用转写草稿，请重新上传或改为文本回答。'
    return '请先修改下方转写文本或重新上传，原始草稿不会直接评分。'
  }
  return `相关性 ${quality.topic_relevance_score} · 语言 ${quality.detected_language || 'unknown'} · 置信度 ${Math.round((quality.stt_confidence || 0) * 100)}%`
}

function replaceSuggestionText(value: string, suggestion: TranscriptSuggestion) {
  if (!suggestion.original || !suggestion.suggested) return value
  return value.split(suggestion.original).join(suggestion.suggested)
}

function validateVoiceFile(file: File) {
  const mimeType = file.type.trim().toLowerCase()
  if (file.size <= 0) {
    return '音频文件为空，请重新选择'
  }
  if (mimeType.startsWith('video/')) {
    return '该文件是视频，请上传音频文件'
  }
  if (mimeType && !mimeType.startsWith('audio/') && mimeType !== 'application/ogg') {
    return '该文件不是音频，请上传音频文件'
  }
  const extension = file.name.split('.').pop()?.toLowerCase()
  if (!extension || !VOICE_AUDIO_EXTENSIONS.has(extension)) {
    return '该文件扩展名不支持，请上传 mp3、wav、webm、m4a 等音频文件'
  }
  return ''
}

function voiceUploadErrorMessage(err: unknown) {
  const message = err instanceof Error ? err.message : ''
  if (err instanceof TypeError && err.message === 'Failed to fetch') {
    return '无法连接后端 API，请确认服务已启动后刷新页面重试'
  }
  if (message.includes('Failed to fetch')) {
    return '无法连接后端 API，请确认服务已启动后刷新页面重试'
  }
  if (message.includes('unsupported_media_type') && message.includes('audio extension')) {
    return '该文件扩展名不支持，请上传 mp3、wav、webm、m4a 等音频文件'
  }
  if (message.includes('unsupported_media_type')) {
    return '该文件是视频或非音频格式，请上传音频文件'
  }
  if (message.includes('uploaded audio is empty')) {
    return '音频文件为空，请重新选择'
  }
  if (message.includes('uploaded audio is too large')) {
    return '音频文件过大，请上传 20MB 以内的音频文件'
  }
  if (isSTTServiceIssue(message)) {
    return message
  }
  if (message.includes('empty transcription')) {
    return '语音转写未生成文本，请确认文件包含可识别语音后重新上传'
  }
  if (message === 'voice transcription failed') {
    return '语音转写失败，请确认文件包含可识别语音后重新上传'
  }
  return message || '语音上传失败'
}

function isSTTServiceIssue(message: string) {
  const normalized = message.toLowerCase()
  return normalized.includes('转写服务') ||
    normalized.includes('stt_model') ||
    normalized.includes('stt_base_url') ||
    normalized.includes('zeta_key') ||
    normalized.includes('jianyi_api_key') ||
    normalized.includes('stt_api_key') ||
    normalized.includes('中转站') ||
    normalized.includes('无可用通道') ||
    normalized.includes('distributor') ||
    normalized.includes('stt provider returned status') ||
    normalized.includes('请求超时')
}

function isInterviewSessionUnavailableError(message: string) {
  const normalized = message.trim().toLowerCase()
  return normalized === 'interview session not found' || normalized === 'interview session is already completed'
}
