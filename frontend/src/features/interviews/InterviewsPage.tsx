import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Check,
  ChevronDown,
  ChevronUp,
  Edit3,
  FileText,
  Filter,
  History,
  Minus,
  Play,
  Plus,
  Settings2,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react'
import { MarkdownComposer } from '../../components/common/MarkdownComposer'
import { MarkdownPreview } from '../../components/common/MarkdownPreview'
import { api, type InterviewLaunchpadSummary, type InterviewLaunchpadTrack } from '../../api/client'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { formatDateTime } from '../../lib/format'
import type { InterviewFocusArea, InterviewQuestion, InterviewSession, ResumeDocument } from '../../types'
import {
  defaultInterviewFocusAreas,
  interviewDifficultyLevelOptions,
  interviewFocusAreaOptions,
  interviewLaunchTracks as fallbackInterviewLaunchTracks,
  type InterviewLaunchTrack,
} from './launchpadConfig'
import './InterviewsPage.css'

type InterviewMode = 'free' | 'role' | 'resume'
type QuestionSort = 'recommended' | 'unpracticed' | 'latest' | 'all'

type LaunchpadFilterState = {
  category: string
  difficulty: string
  questionType: string
  query: string
}

const roleOptions = [
  { id: 'python', label: 'Python 工程师', domains: ['backend', 'database'], scopes: ['Python 基础', 'Web 服务', '数据库', '部署与排障'] },
  { id: 'java', label: 'Java 全栈工程师', domains: ['java', 'database', 'frontend'], scopes: ['Java / JVM', 'Spring', '数据库', '前端协作'] },
  { id: 'crawler', label: '爬虫工程师', domains: ['backend', 'network', 'database'], scopes: ['网络协议', '任务调度', '反爬策略', '数据存储'] },
  { id: 'llm', label: '大模型算法工程师', domains: ['ai_llm', 'system_design'], scopes: ['模型基础', 'RAG', '评测', '工程部署'] },
] as const

const resumeFocusOptions = ['项目职责', '技术决策', '难点与故障', '性能与优化', '协作与推动', '结果与复盘'] as const

export function InterviewsPage() {
  const token = useToken()
  const navigate = useNavigate()
  const [mode, setMode] = useState<InterviewMode>('free')
  const [launchTracks, setLaunchTracks] = useState<InterviewLaunchTrack[]>(fallbackInterviewLaunchTracks)
  const [launchSummary, setLaunchSummary] = useState<InterviewLaunchpadSummary>(() => fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
  const [isLaunchpadLoading, setLaunchpadLoading] = useState(true)
  const [launchpadError, setLaunchpadError] = useState('')
  const [selectedTrackId, setSelectedTrackId] = useState('')
  const [questionSort, setQuestionSort] = useState<QuestionSort>('recommended')
  const [trackFilters, setTrackFilters] = useState<LaunchpadFilterState>({ category: '', difficulty: '', questionType: '', query: '' })
  const [trackPage, setTrackPage] = useState(1)
  const [filterDialogOpen, setFilterDialogOpen] = useState(false)

  const [selectedRoleId, setSelectedRoleId] = useState('')
  const [roleQuery, setRoleQuery] = useState('')
  const [roleLevel, setRoleLevel] = useState('初级')
  const [roleScopes, setRoleScopes] = useState<string[]>([])
  const [roleFocus, setRoleFocus] = useState('项目实战')
  const [roleKeywords, setRoleKeywords] = useState('')

  const [resumeDocuments, setResumeDocuments] = useState<ResumeDocument[]>([])
  const [isResumeLoading, setResumeLoading] = useState(true)
  const [resumeError, setResumeError] = useState('')
  const [previewResumeId, setPreviewResumeId] = useState('')
  const [selectedResumeIds, setSelectedResumeIds] = useState<string[]>([])
  const [resumeEditValue, setResumeEditValue] = useState('')
  const [editingResumeId, setEditingResumeId] = useState('')
  const [isSavingResume, setSavingResume] = useState(false)
  const [resumeFocus, setResumeFocus] = useState<string[]>([...resumeFocusOptions])
  const [pdfPreviewURL, setPdfPreviewURL] = useState('')

  const [difficultyLevel, setDifficultyLevel] = useState(interviewDifficultyLevelOptions[0]?.value ?? 'standard')
  const [maxRounds, setMaxRounds] = useState(3)
  const [smartClose, setSmartClose] = useState(true)
  const [selectedFocusAreas, setSelectedFocusAreas] = useState<InterviewFocusArea[]>(defaultInterviewFocusAreas)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [startError, setStartError] = useState('')
  const [isStarting, setIsStarting] = useState(false)

  const [historySessions, setHistorySessions] = useState<InterviewSession[]>([])
  const [historyError, setHistoryError] = useState('')
  const [isHistoryLoading, setHistoryLoading] = useState(true)
  const [expandedHistoryId, setExpandedHistoryId] = useState('')
  const [historyQuestionDetails, setHistoryQuestionDetails] = useState<Record<string, InterviewQuestion>>({})
  const [loadingHistoryQuestionId, setLoadingHistoryQuestionId] = useState('')
  const [deletingHistoryId, setDeletingHistoryId] = useState('')
  const resumeReaderRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let ignore = false
    const applyFallback = () => {
      setLaunchTracks(fallbackInterviewLaunchTracks)
      setLaunchSummary(fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
    }
    void api.interviewLaunchpad(token)
      .then((response) => {
        if (ignore) return
        const responseTracks = response.open_tracks ?? []
        const tracks = responseTracks.map(launchpadTrackToView).filter((track): track is InterviewLaunchTrack => Boolean(track))
        if (tracks.length === 0) {
          applyFallback()
          setLaunchpadError(responseTracks.length > 0 ? '题库正在更新，当前显示可用基础题目。' : '')
        }
        else {
          setLaunchTracks(tracks)
          setLaunchSummary(response.summary ?? fallbackLaunchpadSummary(tracks))
          setLaunchpadError('')
        }
      })
      .catch((error) => {
        if (ignore) return
        applyFallback()
        setLaunchpadError(error instanceof Error ? error.message : '题目读取失败')
      })
      .finally(() => {
        if (!ignore) setLaunchpadLoading(false)
      })
    return () => { ignore = true }
  }, [token])

  useEffect(() => {
    let ignore = false
    void api.resumeDocuments(token)
      .then((response) => {
        if (ignore) return
        setResumeDocuments(response.list ?? [])
        setPreviewResumeId((current) => current || response.list?.[0]?.id || '')
        setResumeError('')
      })
      .catch((error) => {
        if (!ignore) setResumeError(error instanceof Error ? error.message : '简历读取失败')
      })
      .finally(() => {
        if (!ignore) setResumeLoading(false)
      })
    return () => { ignore = true }
  }, [token])

  useEffect(() => {
    let ignore = false
    void api.history(token)
      .then((res) => {
        if (ignore) return
        setHistorySessions((res.interviews ?? []).slice(0, 6))
        setHistoryError('')
        setHistoryLoading(false)
      })
      .catch((err) => {
        if (ignore) return
        const message = err instanceof Error ? err.message : '历史接口异常'
        setHistoryError(`读取历史面试失败：${message}`)
        setHistoryLoading(false)
      })

    return () => {
      ignore = true
    }
  }, [token])

  const previewResume = useMemo(
    () => resumeDocuments.find((document) => document.id === previewResumeId) ?? null,
    [previewResumeId, resumeDocuments],
  )

  useEffect(() => {
    resumeReaderRef.current?.scrollTo({ top: 0 })
  }, [previewResumeId])

  useEffect(() => {
    let disposed = false
    let objectURL = ''
    if (!previewResume || previewResume.format !== 'pdf' || !previewResume.content_url) {
      return undefined
    }
    void api.resumeDocumentContent(token, previewResume.content_url)
      .then((blob) => {
        if (disposed) return
        objectURL = URL.createObjectURL(blob)
        setPdfPreviewURL(objectURL)
      })
      .catch((error) => {
        if (!disposed) setResumeError(error instanceof Error ? error.message : 'PDF 读取失败')
      })
    return () => {
      disposed = true
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [previewResume, token])

  const filterOptions = useMemo(() => ({
    categories: uniqueTrackValues(launchTracks.map((track) => track.category)),
    difficulties: uniqueTrackValues(launchTracks.map((track) => track.difficulty)),
    questionTypes: uniqueTrackValues(launchTracks.map((track) => track.questionType)),
  }), [launchTracks])

  const visibleTracks = useMemo(() => sortAndFilterTracks(launchTracks, trackFilters, questionSort), [launchTracks, questionSort, trackFilters])
  const trackPageSize = 18
  const trackTotalPages = Math.max(1, Math.ceil(visibleTracks.length / trackPageSize))
  const currentTrackPage = Math.min(trackPage, trackTotalPages)
  const pagedTracks = useMemo(() => visibleTracks.slice((currentTrackPage - 1) * trackPageSize, currentTrackPage * trackPageSize), [currentTrackPage, visibleTracks])
  const selectedTrack = useMemo(() => visibleTracks.find((track) => track.id === selectedTrackId) ?? null, [selectedTrackId, visibleTracks])
  const selectedRole = roleOptions.find((role) => role.id === selectedRoleId)
  const normalizedRoleQuery = roleQuery.trim().toLowerCase()
  const visibleRoleOptions = normalizedRoleQuery
    ? roleOptions.filter((role) => [role.label, ...role.scopes].join(' ').toLowerCase().includes(normalizedRoleQuery))
    : roleOptions
  const roleTrack = selectedRole
    ? launchTracks.find((track) => selectedRole.domains.includes(track.domain as never) || selectedRole.domains.includes(track.category as never)) ?? launchTracks[0]
    : null
  const qualifiedSelectedResumes = selectedResumeIds.filter((id) => resumeDocuments.some((document) => document.id === id && document.quality_status === 'passed'))
  const canStart = mode === 'free' ? Boolean(selectedTrack) : mode === 'role' ? Boolean(selectedRole && roleTrack) : qualifiedSelectedResumes.length > 0
  const startHint = canStart
    ? ''
    : mode === 'free'
      ? '选择一道题后即可开始'
      : mode === 'role'
        ? '选择岗位方向后即可开始'
        : resumeDocuments.length === 0
          ? '请先在个人档案完善简历'
          : '勾选至少一份通过检查的简历'

  function changeMode(nextMode: InterviewMode) {
    setMode(nextMode)
    setStartError('')
  }

  function updateTrackFilter<K extends keyof LaunchpadFilterState>(key: K, value: LaunchpadFilterState[K]) {
    const nextFilters = { ...trackFilters, [key]: value }
    setTrackFilters(nextFilters)
    if (selectedTrackId && !sortAndFilterTracks(launchTracks, nextFilters, questionSort).some((track) => track.id === selectedTrackId)) setSelectedTrackId('')
    setTrackPage(1)
    setStartError('')
  }

  function changeQuestionSort(nextSort: QuestionSort) {
    setQuestionSort(nextSort)
    if (selectedTrackId && !sortAndFilterTracks(launchTracks, trackFilters, nextSort).some((track) => track.id === selectedTrackId)) setSelectedTrackId('')
    setTrackPage(1)
    setStartError('')
  }

  function clearTrackFilters() {
    setTrackFilters({ category: '', difficulty: '', questionType: '', query: '' })
    setTrackPage(1)
  }

  function handleTrackKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (!['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const lastIndex = pagedTracks.length - 1
    if (lastIndex < 0) return
    const nextIndex = event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? lastIndex
        : event.key === 'ArrowDown' || event.key === 'ArrowRight'
          ? (index === lastIndex ? 0 : index + 1)
          : (index === 0 ? lastIndex : index - 1)
    const next = pagedTracks[nextIndex]
    if (!next) return
    setSelectedTrackId(next.id)
    const buttons = event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="radio"]')
    window.requestAnimationFrame(() => buttons?.[nextIndex]?.focus())
  }

  function selectRole(roleId: string) {
    const role = roleOptions.find((item) => item.id === roleId)
    setSelectedRoleId(roleId)
    setRoleScopes(role ? [...role.scopes] : [])
    setStartError('')
  }

  function selectPreviewResume(resumeId: string) {
    setPreviewResumeId(resumeId)
    setEditingResumeId('')
    setResumeEditValue('')
    setPdfPreviewURL('')
  }

  function toggleResumeSelection(document: ResumeDocument) {
    if (document.quality_status !== 'passed') return
    setSelectedResumeIds((current) => current.includes(document.id) ? current.filter((id) => id !== document.id) : [...current, document.id])
    setStartError('')
  }

  function beginResumeEdit(document: ResumeDocument) {
    setEditingResumeId(document.id)
    setResumeEditValue(document.content || document.extracted_text || '')
  }

  async function saveResumeEdit() {
    if (!previewResume || !editingResumeId || isSavingResume) return
    setSavingResume(true)
    setResumeError('')
    try {
      const payload = previewResume.source_type === 'manual' ? splitManualResumeContent(resumeEditValue) : { content: resumeEditValue }
      const updated = await api.updateResumeDocument(token, previewResume.id, payload)
      setResumeDocuments((current) => current.map((document) => document.id === updated.id ? updated : document))
      setEditingResumeId('')
    } catch (error) {
      setResumeError(error instanceof Error ? error.message : '简历保存失败')
    } finally {
      setSavingResume(false)
    }
  }

  function toggleResumeFocus(value: string) {
    setResumeFocus((current) => {
      if (!current.includes(value)) return [...current, value]
      if (current.length === 1) return current
      return current.filter((item) => item !== value)
    })
  }

  function toggleFocusArea(value: InterviewFocusArea) {
    setSelectedFocusAreas((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value])
  }

  async function start() {
    if (!canStart || isStarting) return
    setStartError('')
    setIsStarting(true)
    try {
      await import('./InterviewSessionRoute')
      const common = {
        difficulty_level: difficultyLevel,
        focus_areas: selectedFocusAreas,
        max_rounds: maxRounds,
        smart_close: smartClose,
      }
      const response = mode === 'free' && selectedTrack
        ? await api.createInterview(token, { ...common, mode: 'free', question_id: selectedTrack.questionId })
        : mode === 'role' && selectedRole && roleTrack
          ? await api.createInterview(token, {
              ...common,
              mode: 'role',
              question_id: roleTrack.questionId,
              domain: roleTrack.domain,
              difficulty: roleTrack.difficulty,
              question_type: backendQuestionType(roleTrack.questionType),
              setup_notes: [`目标级别：${roleLevel}`, `技术范围：${roleScopes.join('、')}`, `面试重点：${roleFocus}`, roleKeywords ? `岗位要求：${roleKeywords}` : ''].filter(Boolean).join('\n'),
            })
          : await api.createInterview(token, {
              ...common,
              mode: 'resume_deep_dive',
              resume_ids: qualifiedSelectedResumes,
              setup_notes: `深挖重点：${resumeFocus.join('、')}`,
            })
      navigate(`/interviews/session/${response.session_id}`, { state: { question: response.question, session: response.session } })
    } catch (error) {
      const message = error instanceof Error ? error.message : ''
      const routeError = message.includes('dynamically imported module') || message.includes('Failed to fetch')
      setStartError(routeError ? '面试页面资源加载失败，本次未创建面试记录。' : (message || '面试启动失败，请稍后重试。'))
    } finally {
      setIsStarting(false)
    }
  }

  async function toggleHistoryQuestion(sessionId: string) {
    if (expandedHistoryId === sessionId) {
      setExpandedHistoryId('')
      return
    }
    setExpandedHistoryId(sessionId)
    if (historyQuestionDetails[sessionId]) return
    setLoadingHistoryQuestionId(sessionId)
    setHistoryError('')
    try {
      const detail = await api.interviewSessionDetail(token, sessionId)
      setHistoryQuestionDetails((current) => ({ ...current, [sessionId]: detail.question }))
    } catch (err) {
      setHistoryError(err instanceof Error ? err.message : '读取历史面试题目失败')
    } finally {
      setLoadingHistoryQuestionId('')
    }
  }

  async function deleteHistorySession(sessionId: string) {
    if (deletingHistoryId) return
    setDeletingHistoryId(sessionId)
    setHistoryError('')
    try {
      await api.deleteInterviewSession(token, sessionId)
      setHistorySessions((current) => current.filter((item) => item.id !== sessionId))
      setHistoryQuestionDetails((current) => {
        const next = { ...current }
        delete next[sessionId]
        return next
      })
      if (expandedHistoryId === sessionId) {
        setExpandedHistoryId('')
      }
    } catch (err) {
      setHistoryError(err instanceof Error ? err.message : '删除历史面试失败')
    } finally {
      setDeletingHistoryId('')
    }
  }

  const historyPanel = (
    <details className="interview-history-disclosure" data-testid="interview-history-panel">
      <summary className="interview-history-disclosure-summary">
        <span className="interview-history-disclosure-title"><History size={18} />历史面试</span>
        <span className="interview-history-disclosure-count">
          {isHistoryLoading ? '正在读取' : `${historySessions.length} 场`}
          <ChevronDown size={16} aria-hidden="true" />
        </span>
      </summary>
      <section className="interview-history-panel">
        {historyError && <div className="launch-error" role="alert"><ShieldAlert size={16} />{historyError}</div>}
        {isHistoryLoading ? (
          <div className="interview-panel-skeleton-list" data-testid="history-loading-skeleton">
            <div className="interview-panel-skeleton-wide" />
            <div className="interview-panel-skeleton-wide" />
          </div>
        ) : historySessions.length > 0 ? (
          <div className="interview-history-list">
            {historySessions.map((session) => {
              const question = historyQuestionDetails[session.id]
              const isExpanded = expandedHistoryId === session.id
              const isFinal = session.status === 'final_evaluated'
              return (
                <article className="interview-history-item" key={session.id}>
                  <div className="interview-history-summary">
                    <div className="interview-history-ident">
                      <span>{interviewStatusLabel(session.status)}</span>
                      <strong>面试 #{session.id.slice(0, 8)}</strong>
                      <small>{formatDateTime(session.ended_at || session.started_at || '')}</small>
                    </div>
                    <div className="interview-history-metrics">
                      <span>{session.current_round}/{session.max_rounds} 轮</span>
                      <span>{typeof session.final_score === 'number' ? `${session.final_score} 分` : '未出分'}</span>
                    </div>
                    <div className="interview-history-actions">
                      {isFinal ? (
                        <a className="primary-button compact" href={`/interviews/session/${session.id}/report`}>查看报告</a>
                      ) : (
                        <a className="primary-button compact" href={`/interviews/session/${session.id}`}>继续面试</a>
                      )}
                      <button className="ghost-button compact" type="button" onClick={() => void toggleHistoryQuestion(session.id)}>
                        {isExpanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
                        {isExpanded ? '收起题目' : '查看题目'}
                      </button>
                      <button
                        className="ghost-button compact interview-history-delete"
                        type="button"
                        onClick={() => void deleteHistorySession(session.id)}
                        disabled={deletingHistoryId === session.id}
                        aria-label="删除记录"
                      >
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </div>
                  {isExpanded && (
                    <div className="interview-history-question">
                      {loadingHistoryQuestionId === session.id && <span>正在加载题目...</span>}
                      {question && (
                        <>
                          <div className="scenario-meta">
                            <span>{domainLabel(question.domain)}</span>
                            <span>{question.difficulty}</span>
                            <span>{interviewQuestionTypeLabel(question.question_type)}</span>
                          </div>
                          <strong>{question.title}</strong>
                          <p>{question.description}</p>
                        </>
                      )}
                    </div>
                  )}
                </article>
              )
            })}
          </div>
        ) : (
          <div className="empty-inline">完成一场面试后，记录会出现在这里。</div>
        )}
      </section>
    </details>
  )

  const hasActiveFilters = Boolean(trackFilters.category || trackFilters.difficulty || trackFilters.questionType || trackFilters.query.trim())

  return (
    <section className="page-stack interview-launchpad interview-launchpad-v2">
      <header className="interview-command-bar">
        <div className="interview-mode-switch" role="tablist" aria-label="选择面试模式">
          {([
            ['free', '自由选题'],
            ['role', '岗位专项'],
            ['resume', '简历深挖'],
          ] as const).map(([value, label]) => (
            <button key={value} type="button" role="tab" aria-selected={mode === value} className={mode === value ? 'active' : ''} onClick={() => changeMode(value)}>{label}</button>
          ))}
        </div>
        <div className="interview-command-action">
          <span aria-live="polite">{startHint}</span>
          <button className="primary-button interview-start-button" type="button" onClick={() => void start()} disabled={!canStart || isStarting}>
            <Play size={18} />{isStarting ? '正在进入…' : '开始面试'}
          </button>
        </div>
      </header>

      <section className="interview-settings-bar" aria-label="本场面试设置">
        <div className="interview-mobile-settings-summary">
          <span>{interviewDifficultyPromptLabel(difficultyLevel)} · {maxRounds} 轮 · {smartClose ? '智能收束（由面试官判断）' : '按轮数结束'}</span>
          <button className="ghost-button compact" type="button" onClick={() => setSettingsOpen(true)}><Settings2 size={16} />调整</button>
        </div>
        <div className="interview-setting-group">
          <span>面试方式</span>
          <div className="interview-setting-segments" role="radiogroup" aria-label="面试方式">
            {interviewDifficultyLevelOptions.map((option) => (
              <button key={option.value} type="button" role="radio" aria-checked={difficultyLevel === option.value} className={difficultyLevel === option.value ? 'active' : ''} onClick={() => setDifficultyLevel(option.value)}>
                {interviewDifficultyPromptLabel(option.value)}
              </button>
            ))}
          </div>
        </div>
        <div className="interview-setting-group rounds">
          <span>最多作答</span>
          <button type="button" aria-label="减少一轮" onClick={() => setMaxRounds((value) => Math.max(1, value - 1))}><Minus size={14} /></button>
          <strong>{maxRounds}</strong>
          <button type="button" aria-label="增加一轮" onClick={() => setMaxRounds((value) => Math.min(15, value + 1))}><Plus size={14} /></button>
        </div>
        <label className="interview-setting-group smart-close">
          <input type="checkbox" checked={smartClose} onChange={(event) => setSmartClose(event.target.checked)} />
          <span>智能收束</span>
          <small>由面试官判断</small>
        </label>
        <button className="ghost-button compact interview-more-settings" type="button" onClick={() => setSettingsOpen(true)}><Settings2 size={16} />更多设置</button>
      </section>

      {startError && <div className="launch-error" role="alert"><ShieldAlert size={16} />{startError}</div>}

      {mode === 'free' && (
        <section className="interview-question-stage" aria-label="面试题目">
          <div className="interview-question-tabs" role="tablist" aria-label="题目排序">
            {([
              ['recommended', '推荐'],
              ['unpracticed', '未练过'],
              ['latest', '最新'],
              ['all', '全部'],
            ] as const).map(([value, label]) => (
              <button key={value} type="button" role="tab" aria-selected={questionSort === value} className={questionSort === value ? 'active' : ''} onClick={() => changeQuestionSort(value)}>{label}</button>
            ))}
          </div>
          <div className="interview-track-filter-bar">
            <label className="interview-track-search-field"><span>搜索</span><input value={trackFilters.query} onChange={(event) => updateTrackFilter('query', event.target.value)} placeholder="题干、领域或题号" /></label>
            <label className="desktop-filter"><span>领域</span><select value={trackFilters.category} onChange={(event) => updateTrackFilter('category', event.target.value)}><option value="">全部领域</option>{filterOptions.categories.map((value) => <option value={value} key={value}>{domainLabel(value)}</option>)}</select></label>
            <label className="desktop-filter"><span>难度</span><select value={trackFilters.difficulty} onChange={(event) => updateTrackFilter('difficulty', event.target.value)}><option value="">全部难度</option>{filterOptions.difficulties.map((value) => <option value={value} key={value}>{value}</option>)}</select></label>
            <label className="desktop-filter"><span>题型</span><select value={trackFilters.questionType} onChange={(event) => updateTrackFilter('questionType', event.target.value)}><option value="">全部题型</option>{filterOptions.questionTypes.map((value) => <option value={value} key={value}>{interviewQuestionTypeLabel(value)}</option>)}</select></label>
            <button className="ghost-button compact mobile-filter-button" type="button" onClick={() => setFilterDialogOpen(true)}><Filter size={16} />筛选</button>
            {hasActiveFilters && <button className="ghost-button compact interview-filter-clear" type="button" onClick={clearTrackFilters}>清空筛选</button>}
          </div>

          {launchpadError && <div className="interview-compat-notice" role="status">题库接口暂不可用，当前显示可启动的兼容题目。</div>}
          {isLaunchpadLoading ? (
            <div className="interview-question-grid">{Array.from({ length: 6 }, (_, index) => <div className="interview-question-card skeleton" key={index} />)}</div>
          ) : pagedTracks.length > 0 ? (
            <div className="interview-question-grid" role="radiogroup" aria-label="选择一道面试题">
              {pagedTracks.map((track, index) => {
                const selected = selectedTrackId === track.id
                return (
                  <button key={track.id} type="button" role="radio" aria-checked={selected} className={`interview-question-card ${selected ? 'selected' : ''}`} tabIndex={selectedTrackId ? (selected ? 0 : -1) : (index === 0 ? 0 : -1)} onClick={() => { setSelectedTrackId(selected ? '' : track.id); setStartError('') }} onKeyDown={(event) => handleTrackKeyDown(event, index)}>
                    <span className="interview-question-card-topline"><small>{track.stableCode}</small>{selected && <span aria-label="已选择"><Check size={16} /></span>}</span>
                    <strong>{track.openingQuestion}</strong>
                    <span className="interview-question-card-meta"><em>{track.domainLabel || domainLabel(track.category)}</em><em>{track.difficulty}</em><em>{interviewQuestionTypeLabel(track.questionType)}</em></span>
                  </button>
                )
              })}
            </div>
          ) : <div className="empty-inline interview-question-empty">{launchpadTrackEmptyMessage(launchSummary, hasActiveFilters)}</div>}
          {visibleTracks.length > trackPageSize && <div className="interview-track-pagination"><span>共 {visibleTracks.length} 题 · {currentTrackPage}/{trackTotalPages}</span><div><button className="ghost-button compact" type="button" disabled={currentTrackPage === 1} onClick={() => setTrackPage(currentTrackPage - 1)}>上一页</button><button className="ghost-button compact" type="button" disabled={currentTrackPage === trackTotalPages} onClick={() => setTrackPage(currentTrackPage + 1)}>下一页</button></div></div>}
        </section>
      )}

      {mode === 'role' && (
        <section className="interview-role-workspace" aria-label="选择岗位方向">
          <div className="interview-role-list" role="radiogroup">
            <label className="interview-role-search"><span>搜索岗位</span><input type="search" value={roleQuery} onChange={(event) => setRoleQuery(event.target.value)} placeholder="岗位名称或技术方向" /></label>
            {visibleRoleOptions.map((role) => {
              const selected = selectedRoleId === role.id
              return <button key={role.id} type="button" role="radio" aria-checked={selected} className={selected ? 'selected' : ''} onClick={() => selectRole(role.id)}><span>{role.label}</span>{selected && <Check size={17} />}</button>
            })}
            {visibleRoleOptions.length === 0 && <div className="empty-inline interview-role-empty">没有匹配的岗位方向。</div>}
          </div>
          <div className="interview-role-summary">
            {selectedRole ? <><small>本场方向</small><h2>{selectedRole.label}</h2><p>{roleScopes.join(' · ')}</p></> : <div className="empty-inline">选择一个岗位方向，题目会在开始面试后确定。</div>}
          </div>
        </section>
      )}

      {mode === 'resume' && (
        <section className="resume-interview-workspace" aria-label="简历深挖">
          {isResumeLoading ? <div className="interview-question-card skeleton" /> : resumeDocuments.length === 0 ? (
            <div className="resume-empty-state"><FileText size={30} /><strong>还没有可用简历</strong><span>完善个人档案后即可选择简历进行深挖。</span><button className="primary-button compact" type="button" onClick={() => navigate('/profile')}>完善个人档案</button></div>
          ) : (
            <>
              <select className="resume-mobile-select" value={previewResumeId} onChange={(event) => selectPreviewResume(event.target.value)} aria-label="切换简历">
                {resumeDocuments.map((document) => <option key={document.id} value={document.id}>{document.name}</option>)}
              </select>
              <aside className="resume-file-sidebar" aria-label="简历文件">
                {resumeDocuments.map((document) => {
                  const active = document.id === previewResumeId
                  const selected = selectedResumeIds.includes(document.id)
                  return (
                    <div className={`resume-file-row ${active ? 'active' : ''}`} key={document.id}>
                      <button type="button" onClick={() => selectPreviewResume(document.id)}><FileText size={16} /><span><strong>{document.name}</strong><small>{document.format.toUpperCase()} · {document.quality_status === 'passed' ? '可用' : '需完善'}</small></span></button>
                      <input type="checkbox" checked={selected} disabled={document.quality_status !== 'passed'} onChange={() => toggleResumeSelection(document)} aria-label={`本场${selected ? '取消' : '选择'}${document.name}`} />
                    </div>
                  )
                })}
              </aside>
              <div className="resume-reader" ref={resumeReaderRef}>
                {previewResume && (
                  <>
                    <header className="resume-reader-header"><div className="resume-reader-title"><strong>{previewResume.name}</strong><span>{previewResume.format.toUpperCase()} · {previewResume.quality_status === 'passed' ? '已通过检查' : previewResume.quality_reason}</span></div><div className="resume-reader-actions"><label className="resume-mobile-selection"><input type="checkbox" checked={selectedResumeIds.includes(previewResume.id)} disabled={previewResume.quality_status !== 'passed'} onChange={() => toggleResumeSelection(previewResume)} /><span>{selectedResumeIds.includes(previewResume.id) ? '本场已选' : '加入本场'}</span></label>{previewResume.editable && editingResumeId !== previewResume.id && <button className="ghost-button compact" type="button" onClick={() => beginResumeEdit(previewResume)}><Edit3 size={15} />编辑</button>}</div></header>
                    <div className="resume-reader-status" aria-live="polite">当前查看：{previewResume.name}</div>
                    {editingResumeId === previewResume.id ? (
                      <div className="resume-editor"><MarkdownComposer value={resumeEditValue} onChange={setResumeEditValue} placeholder="填写简历内容" editorLabel="编辑简历" editorTestId="resume-document-editor" previewEmptyText="暂无简历内容" /><div className="resume-editor-actions"><button className="ghost-button compact" type="button" onClick={() => setEditingResumeId('')}>取消</button><button className="primary-button compact" type="button" disabled={isSavingResume} onClick={() => void saveResumeEdit()}>{isSavingResume ? '保存中…' : '保存'}</button></div></div>
                    ) : previewResume.format === 'pdf' ? (
                      pdfPreviewURL ? <iframe className="resume-pdf-frame" src={pdfPreviewURL} title={previewResume.name} /> : <div className="empty-inline">正在读取 PDF…</div>
                    ) : <div className="resume-markdown-canvas"><MarkdownPreview content={previewResume.content || previewResume.extracted_text} emptyText="暂无可显示内容" /></div>}
                  </>
                )}
              </div>
            </>
          )}
          {resumeError && <div className="launch-error" role="alert"><ShieldAlert size={16} />{resumeError}</div>}
        </section>
      )}

      {historyPanel}

      {settingsOpen && <dialog className="interview-settings-dialog" open onCancel={() => setSettingsOpen(false)}><div className="interview-dialog-heading"><strong>更多设置</strong><button type="button" aria-label="关闭" onClick={() => setSettingsOpen(false)} autoFocus><X size={18} /></button></div><div className="interview-dialog-body">
        <div className="interview-mobile-basic-settings">
          <fieldset><legend>面试方式</legend><div className="interview-option-grid">{interviewDifficultyLevelOptions.map((option) => <label key={option.value}><input type="radio" name="mobile-difficulty-level" checked={difficultyLevel === option.value} onChange={() => setDifficultyLevel(option.value)} /><span>{interviewDifficultyPromptLabel(option.value)}</span></label>)}</div></fieldset>
          <fieldset><legend>最多作答轮数</legend><div className="interview-mobile-rounds"><button type="button" aria-label="减少一轮" onClick={() => setMaxRounds((value) => Math.max(1, value - 1))}><Minus size={15} /></button><strong>{maxRounds}</strong><button type="button" aria-label="增加一轮" onClick={() => setMaxRounds((value) => Math.min(15, value + 1))}><Plus size={15} /></button></div></fieldset>
          <label className="interview-mobile-smart-close"><input type="checkbox" checked={smartClose} onChange={(event) => setSmartClose(event.target.checked)} /><span>智能收束</span></label>
        </div>
        {mode === 'free' && <fieldset><legend>重点追问</legend><div className="interview-option-grid">{interviewFocusAreaOptions.map((option) => <label key={option.value}><input type="checkbox" checked={selectedFocusAreas.includes(option.value)} onChange={() => toggleFocusArea(option.value)} /><span>{interviewFocusAreaPromptLabel(option.value)}</span></label>)}</div></fieldset>}
        {mode === 'role' && <><fieldset><legend>目标级别</legend><div className="interview-option-grid">{['实习', '初级', '中级', '高级'].map((value) => <label key={value}><input type="radio" name="role-level" checked={roleLevel === value} onChange={() => setRoleLevel(value)} /><span>{value}</span></label>)}</div></fieldset><fieldset><legend>技术范围</legend><div className="interview-option-grid">{(selectedRole?.scopes ?? []).map((scope) => <label key={scope}><input type="checkbox" checked={roleScopes.includes(scope)} onChange={() => setRoleScopes((current) => current.includes(scope) ? current.filter((item) => item !== scope) : [...current, scope])} /><span>{scope}</span></label>)}</div></fieldset><label className="interview-dialog-field"><span>面试重点</span><select value={roleFocus} onChange={(event) => setRoleFocus(event.target.value)}><option>基础原理</option><option>项目实战</option><option>故障排查</option><option>架构设计</option></select></label><label className="interview-dialog-field"><span>岗位要求关键词</span><input value={roleKeywords} onChange={(event) => setRoleKeywords(event.target.value)} placeholder="可选" /></label></>}
        {mode === 'resume' && <fieldset><legend>深挖重点</legend><div className="interview-option-grid">{resumeFocusOptions.map((value) => <label key={value}><input type="checkbox" checked={resumeFocus.includes(value)} onChange={() => toggleResumeFocus(value)} /><span>{value}</span></label>)}</div></fieldset>}
      </div><div className="interview-dialog-footer"><button className="primary-button compact" type="button" onClick={() => setSettingsOpen(false)}>完成</button></div></dialog>}

      {filterDialogOpen && <dialog className="interview-filter-dialog" open onCancel={() => setFilterDialogOpen(false)}><div className="interview-dialog-heading"><strong>筛选题目</strong><button type="button" aria-label="关闭" onClick={() => setFilterDialogOpen(false)} autoFocus><X size={18} /></button></div><div className="interview-dialog-body"><label className="interview-dialog-field"><span>领域</span><select value={trackFilters.category} onChange={(event) => updateTrackFilter('category', event.target.value)}><option value="">全部领域</option>{filterOptions.categories.map((value) => <option value={value} key={value}>{domainLabel(value)}</option>)}</select></label><label className="interview-dialog-field"><span>难度</span><select value={trackFilters.difficulty} onChange={(event) => updateTrackFilter('difficulty', event.target.value)}><option value="">全部难度</option>{filterOptions.difficulties.map((value) => <option value={value} key={value}>{value}</option>)}</select></label><label className="interview-dialog-field"><span>题型</span><select value={trackFilters.questionType} onChange={(event) => updateTrackFilter('questionType', event.target.value)}><option value="">全部题型</option>{filterOptions.questionTypes.map((value) => <option value={value} key={value}>{interviewQuestionTypeLabel(value)}</option>)}</select></label></div><div className="interview-dialog-footer">{hasActiveFilters && <button className="ghost-button compact" type="button" onClick={clearTrackFilters}>清空</button>}<button className="primary-button compact" type="button" onClick={() => setFilterDialogOpen(false)}>查看结果</button></div></dialog>}
    </section>
  )
}

function launchpadTrackToView(track: InterviewLaunchpadTrack): InterviewLaunchTrack | null {
  if (!track.id || !track.question_id || !track.domain || !track.difficulty) return null
  return {
    id: track.id,
    questionId: track.question_id,
    stableCode: track.stable_code,
    openingQuestion: track.opening_question || track.summary,
    title: track.title,
    domain: track.domain,
    domainLabel: track.domain_label || domainLabel(track.domain),
    category: track.category || track.domain,
    difficulty: track.difficulty,
    questionType: normalizeQuestionType(track.question_type),
    questionRole: normalizeQuestionRole(track.question_role),
    tags: Array.isArray(track.tags) ? track.tags.filter(Boolean) : [],
    availabilityState: normalizeLaunchpadAvailabilityState(track.availability_state, track.vector_status_summary),
    vectorStatusSummary: track.vector_status_summary || 'compatibility_seed',
    unavailableReason: track.unavailable_reason,
    summary: track.opening_question || track.summary,
    details: Array.isArray(track.details) ? track.details.filter(Boolean) : [],
    practiced: Boolean(track.practiced),
    latestUpdatedAt: track.latest_updated_at,
  }
}

function fallbackLaunchpadSummary(tracks: InterviewLaunchTrack[]): InterviewLaunchpadSummary {
  return { open_track_count: tracks.length, published_atom_count: 0, indexed_atom_count: 0, fallback_mode: true, state: 'compatibility_fallback', message: '' }
}

function sortAndFilterTracks(tracks: InterviewLaunchTrack[], filters: LaunchpadFilterState, sort: QuestionSort) {
  const query = filters.query.trim().toLowerCase()
  const filtered = tracks.filter((track) => {
    if (filters.category && track.category !== filters.category) return false
    if (filters.difficulty && track.difficulty !== filters.difficulty) return false
    if (filters.questionType && track.questionType !== filters.questionType) return false
    if (!query) return true
    return [track.openingQuestion, track.title, track.stableCode, track.domain, track.domainLabel, track.category, ...track.tags].join(' ').toLowerCase().includes(query)
  })
  if (sort === 'unpracticed') return filtered.filter((track) => !track.practiced)
  if (sort === 'latest') return [...filtered].sort((left, right) => Date.parse(right.latestUpdatedAt || '') - Date.parse(left.latestUpdatedAt || ''))
  if (sort === 'all') return [...filtered].sort((left, right) => left.stableCode.localeCompare(right.stableCode))
  return filtered
}

function uniqueTrackValues(values: string[]) {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort((left, right) => left.localeCompare(right))
}

function normalizeQuestionType(value: string): InterviewLaunchTrack['questionType'] {
  if (value === 'troubleshooting' || value === 'architecture' || value === 'behavioral') return value
  return 'principle'
}

function backendQuestionType(value: string) {
  return value === 'troubleshooting' ? 'scenario_analysis' : value
}

function normalizeQuestionRole(value: string): InterviewLaunchTrack['questionRole'] {
  if (value === 'followup' || value === 'mixed') return value
  return 'opening'
}

function normalizeLaunchpadAvailabilityState(value: string, vectorStatusSummary: string): InterviewLaunchTrack['availabilityState'] {
  if (value === 'indexing') return 'indexing'
  if (value === 'fallback' || vectorStatusSummary === 'compatibility_seed') return 'fallback'
  return 'available'
}

function splitManualResumeContent(content: string) {
  const normalized = content.replace(/\r\n/g, '\n')
  const projectMarker = /^#{1,6}\s*项目经历\s*$/m
  const match = projectMarker.exec(normalized)
  if (!match || match.index === undefined) return { resume_summary: normalized.replace(/^#{1,6}\s*简历摘要\s*$/m, '').trim(), project_summary: '' }
  return {
    resume_summary: normalized.slice(0, match.index).replace(/^#{1,6}\s*简历摘要\s*$/m, '').trim(),
    project_summary: normalized.slice(match.index + match[0].length).trim(),
  }
}

function interviewStatusLabel(status: string) {
  return ({ question_presented: '待作答', answer_submitted: '已作答', follow_up_1_presented: '追问中', follow_up_2_presented: '追问中', final_evaluated: '已完成', invalidated: '已作废' } as Record<string, string>)[status] ?? status
}

function interviewQuestionTypeLabel(type: string) {
  return ({ scenario_analysis: '故障排查', troubleshooting: '故障排查', principle: '原理问答', architecture: '架构设计', behavioral: '行为面试', resume_deep_dive: '简历深挖' } as Record<string, string>)[type] ?? type
}

function interviewDifficultyPromptLabel(value: string) {
  return ({ standard: '平衡追问', foundation: '从基础聊起', challenge: '高压深挖' } as Record<string, string>)[value] ?? value
}

function interviewFocusAreaPromptLabel(value: InterviewFocusArea) {
  return ({ technical_accuracy: '技术与原理', logical_completeness: '排查路径', solution_feasibility: '验证与回滚', depth_breadth: '边界与权衡', expression_structure: '表达组织' } as Record<InterviewFocusArea, string>)[value]
}

function launchpadTrackEmptyMessage(summary: InterviewLaunchpadSummary, hasFilters: boolean) {
  if (hasFilters) return '没有符合当前条件的题目。'
  if (summary.published_atom_count === 0 && !summary.fallback_mode) return '暂时没有可用于面试的题目。'
  return '当前没有可用面试题。'
}
