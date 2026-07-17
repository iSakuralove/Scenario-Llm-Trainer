import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronDown, ChevronRight, ChevronUp, History, MessageSquareText, Play, ShieldAlert, Trash2 } from 'lucide-react'
import { api, type InterviewLaunchpadSummary, type InterviewLaunchpadTrack } from '../../api/client'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { formatDateTime } from '../../lib/format'
import type { InterviewFocusArea, InterviewQuestion, InterviewSession } from '../../types'
import {
  defaultInterviewFocusAreas,
  interviewDifficultyLevelOptions,
  interviewFocusAreaOptions,
  interviewLaunchTracks as fallbackInterviewLaunchTracks,
  type InterviewLaunchTrack,
} from './launchpadConfig'
import './InterviewsPage.css'

type LaunchpadFilterState = {
  category: string
  difficulty: string
}

export function InterviewsPage() {
  const token = useToken()
  const navigate = useNavigate()
  const [launchTracks, setLaunchTracks] = useState<InterviewLaunchTrack[]>(fallbackInterviewLaunchTracks)
  const [launchSummary, setLaunchSummary] = useState<InterviewLaunchpadSummary>(() => fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
  const [trackFilters, setTrackFilters] = useState<LaunchpadFilterState>({ category: '', difficulty: '' })
  const [isLaunchpadLoading, setLaunchpadLoading] = useState(true)
  const [selectedTrackId, setSelectedTrackId] = useState('')
  const [difficultyLevel, setDifficultyLevel] = useState(interviewDifficultyLevelOptions[0]?.value ?? 'standard')
  const [selectedFocusAreas, setSelectedFocusAreas] = useState<InterviewFocusArea[]>(defaultInterviewFocusAreas)
  const [startError, setStartError] = useState('')
  const [isStarting, setIsStarting] = useState(false)
  const [historySessions, setHistorySessions] = useState<InterviewSession[]>([])
  const [historyError, setHistoryError] = useState('')
  const [isHistoryLoading, setHistoryLoading] = useState(true)
  const [expandedHistoryId, setExpandedHistoryId] = useState('')
  const [historyQuestionDetails, setHistoryQuestionDetails] = useState<Record<string, InterviewQuestion>>({})
  const [loadingHistoryQuestionId, setLoadingHistoryQuestionId] = useState('')
  const [deletingHistoryId, setDeletingHistoryId] = useState('')
  const setupPanelRef = useRef<HTMLElement | null>(null)

  const trackFilterOptions = useMemo(() => ({
    categories: uniqueTrackValues(launchTracks.map((track) => track.category)),
    difficulties: uniqueTrackValues(launchTracks.map((track) => track.difficulty)),
  }), [launchTracks])

  const visibleLaunchTracks = useMemo(() => filterLaunchTracks(launchTracks, trackFilters), [launchTracks, trackFilters])

  const selectedTrack = useMemo(
    () => visibleLaunchTracks.find((track) => track.id === selectedTrackId),
    [selectedTrackId, visibleLaunchTracks],
  )

  const hasActiveFilters = Boolean(trackFilters.category || trackFilters.difficulty)
  const launchpadTrackEmptyText = launchpadTrackEmptyMessage(launchSummary, hasActiveFilters)

  useEffect(() => {
    let ignore = false
    const applyFallbackLaunchpad = () => {
      setLaunchTracks(fallbackInterviewLaunchTracks)
      setLaunchSummary(fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
      setSelectedTrackId((current) => fallbackInterviewLaunchTracks.some((track) => track.id === current) ? current : '')
    }

    void api.interviewLaunchpad(token)
      .then((res) => {
        if (ignore) return
        const tracks = (res.open_tracks ?? []).map(launchpadTrackToView).filter((track): track is InterviewLaunchTrack => Boolean(track))
        if (tracks.length === 0 && res.fallback_mode) {
          applyFallbackLaunchpad()
          setLaunchpadLoading(false)
          return
        }
        setLaunchTracks(tracks)
        setLaunchSummary(res.summary ?? fallbackLaunchpadSummary(tracks))
        setSelectedTrackId((current) => tracks.some((track) => track.id === current) ? current : '')
        setStartError('')
        setLaunchpadLoading(false)
      })
      .catch(() => {
        if (ignore) return
        applyFallbackLaunchpad()
        setLaunchpadLoading(false)
      })

    return () => {
      ignore = true
    }
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

  function selectTrack(trackId: string) {
    const willOpenSetup = selectedTrackId !== trackId
    setSelectedTrackId(willOpenSetup ? trackId : '')
    setStartError('')
    if (!willOpenSetup) return
    window.requestAnimationFrame(() => {
      const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
      setupPanelRef.current?.scrollIntoView({ behavior: reduceMotion ? 'auto' : 'smooth', block: 'nearest' })
    })
  }

  function updateTrackFilter<K extends keyof LaunchpadFilterState>(key: K, value: LaunchpadFilterState[K]) {
    const nextFilters = { ...trackFilters, [key]: value }
    const nextTracks = filterLaunchTracks(launchTracks, nextFilters)
    setTrackFilters(nextFilters)
    setSelectedTrackId((current) => nextTracks.some((track) => track.id === current) ? current : '')
    setStartError('')
  }

  function clearTrackFilters() {
    setTrackFilters({ category: '', difficulty: '' })
    setStartError('')
  }

  function handleTrackKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (!['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const trackGrid = event.currentTarget.parentElement
    const lastIndex = visibleLaunchTracks.length - 1
    if (lastIndex < 0) return
    const nextIndex = (() => {
      if (event.key === 'Home') return 0
      if (event.key === 'End') return lastIndex
      if (event.key === 'ArrowDown' || event.key === 'ArrowRight') return index === lastIndex ? 0 : index + 1
      return index === 0 ? lastIndex : index - 1
    })()
    const nextTrack = visibleLaunchTracks[nextIndex]
    if (!nextTrack) return
    setSelectedTrackId(nextTrack.id)
    setStartError('')
    window.requestAnimationFrame(() => {
      const radios = trackGrid?.querySelectorAll<HTMLButtonElement>('[role="radio"]')
      const nextRadio = radios?.[nextIndex]
      nextRadio?.focus()
      nextRadio?.scrollIntoView({ block: 'nearest' })
    })
  }

  function toggleFocusArea(value: InterviewFocusArea) {
    setSelectedFocusAreas((current) => {
      if (current.includes(value)) {
        return current.filter((item) => item !== value)
      }
      return [...current, value]
    })
  }

  async function start() {
    if (!selectedTrack || isStarting) return
    setStartError('')
    setIsStarting(true)
    try {
      // 先确认会话页代码可加载，再创建后端会话，避免资源失败时留下无效历史记录。
      await import('./InterviewSessionRoute')
      const res = await api.createInterview(token, {
        domain: selectedTrack.domain,
        difficulty: selectedTrack.difficulty,
        question_type: selectedTrack.questionType,
        difficulty_level: difficultyLevel,
        focus_areas: selectedFocusAreas,
        setup_notes: '',
      })
      if (!matchesSelectedTrack(res.question, selectedTrack)) {
        setStartError('题目与所选面试题不一致，请稍后重试。')
        return
      }
      navigate(`/interviews/session/${res.session_id}`, { state: { question: res.question, session: res.session } })
    } catch (err) {
      const message = err instanceof Error ? err.message : ''
      const isRouteLoadError = message.includes('dynamically imported module') || message.includes('Failed to fetch')
      setStartError(isRouteLoadError ? '面试页面资源加载失败，请刷新后重试。本次未创建面试记录。' : (message || '面试启动失败，请稍后重试。'))
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

  return (
    <section className="page-stack interview-launchpad">
      <header className="interview-question-hero" aria-labelledby="interview-command-title">
        <div className="interview-question-eyebrow"><MessageSquareText size={16} />技术面试舱</div>
        <h1 id="interview-command-title">选择一道题，开始真实追问</h1>
        <p>先选题，再按你的目标调整这场面试。</p>
      </header>

      <section className="interview-question-stage" aria-label="面试题目">
        {isLaunchpadLoading ? (
          <div className="interview-question-loading" data-testid="launchpad-track-loading-skeleton">
            <div className="interview-track-filter-bar skeleton">
              <div className="interview-panel-skeleton-line" />
              <div className="interview-panel-skeleton-line" />
            </div>
            <div className="interview-question-grid" data-testid="interview-question-loading-grid">
              {Array.from({ length: 5 }, (_, index) => <div className="interview-question-card skeleton" key={index} />)}
            </div>
          </div>
        ) : (
          <>
            <div className="interview-track-filter-bar">
              <label>
                <span>领域</span>
                <select value={trackFilters.category} onChange={(event) => updateTrackFilter('category', event.target.value)} aria-label="按领域筛选面试题">
                  <option value="">全部领域</option>
                  {trackFilterOptions.categories.map((value) => <option value={value} key={`category-${value}`}>{domainLabel(value)}</option>)}
                </select>
              </label>
              <label>
                <span>难度</span>
                <select value={trackFilters.difficulty} onChange={(event) => updateTrackFilter('difficulty', event.target.value)} aria-label="按难度筛选面试题">
                  <option value="">全部难度</option>
                  {trackFilterOptions.difficulties.map((value) => <option value={value} key={`difficulty-${value}`}>{value}</option>)}
                </select>
              </label>
              {hasActiveFilters && (
                <button type="button" className="ghost-button compact interview-filter-clear" onClick={clearTrackFilters}>清空筛选</button>
              )}
            </div>

            <div className="interview-question-grid" role="radiogroup" aria-label="选择一道面试题" data-testid="interview-track-grid">
              {visibleLaunchTracks.length > 0 ? visibleLaunchTracks.map((track, index) => {
                const isSelected = selectedTrackId === track.id
                return (
                  <button
                    key={track.id}
                    type="button"
                    role="radio"
                    aria-checked={isSelected}
                    aria-controls="interview-shared-setup"
                    tabIndex={selectedTrackId ? (isSelected ? 0 : -1) : (index === 0 ? 0 : -1)}
                    className={`interview-question-card ${isSelected ? 'selected' : ''}`}
                    data-testid={`interview-question-card-${track.id}`}
                    onClick={() => selectTrack(track.id)}
                    onKeyDown={(event) => handleTrackKeyDown(event, index)}
                  >
                    <span className="interview-question-card-topline">
                      <small>{String(index + 1).padStart(2, '0')}</small>
                      <span>{isSelected ? <><Check size={14} />已选择</> : <>选择题目<ChevronRight size={14} /></>}</span>
                    </span>
                    <strong>{track.summary}</strong>
                    <span className="interview-question-card-meta">
                      <em>{track.domainLabel || domainLabel(track.category)}</em>
                      <em>{track.difficulty}</em>
                      <em>{interviewQuestionTypeLabel(track.questionType)}</em>
                    </span>
                  </button>
                )
              }) : (
                <div className="empty-inline interview-question-empty">{launchpadTrackEmptyText}</div>
              )}
            </div>

            {selectedTrack && (
              <section id="interview-shared-setup" className="interview-setup-panel" ref={setupPanelRef} aria-labelledby="interview-setup-title">
                <div className="interview-setup-heading">
                  <div>
                    <h2 id="interview-setup-title">本场面试设置</h2>
                    <p>正在设置：<strong>{selectedTrack.summary}</strong></p>
                  </div>
                  <button type="button" className="ghost-button compact" onClick={() => setSelectedTrackId('')}>取消选择</button>
                </div>

                <fieldset className="interview-setup-fieldset">
                  <legend>面试方式</legend>
                  <div className="interview-segmented-control" role="radiogroup" aria-label="选择面试方式">
                    {interviewDifficultyLevelOptions.map((option) => (
                      <button
                        key={option.value}
                        type="button"
                        role="radio"
                        aria-checked={difficultyLevel === option.value}
                        className={difficultyLevel === option.value ? 'active' : ''}
                        onClick={() => setDifficultyLevel(option.value)}
                        title={option.note}
                      >
                        {interviewDifficultyPromptLabel(option.value)}
                      </button>
                    ))}
                  </div>
                </fieldset>

                <fieldset className="interview-setup-fieldset">
                  <legend>希望面试官重点追问</legend>
                  <div className="interview-focus-grid">
                    {interviewFocusAreaOptions.map((option) => (
                      <label key={option.value} title={option.note}>
                        <input
                          type="checkbox"
                          checked={selectedFocusAreas.includes(option.value)}
                          onChange={() => toggleFocusArea(option.value)}
                        />
                        <span>{interviewFocusAreaPromptLabel(option.value)}</span>
                      </label>
                    ))}
                  </div>
                </fieldset>

                {startError && (
                  <div className="launch-error" role="alert">
                    <ShieldAlert size={16} />
                    {startError}
                  </div>
                )}

                <div className="interview-setup-footer">
                  <p>面试官会根据你的回答继续追问，不是固定问卷。</p>
                  <button className="primary-button interview-start-button" type="button" onClick={() => void start()} disabled={isStarting}>
                    <Play size={18} />
                    {isStarting ? '正在进入面试…' : '开始这场面试'}
                  </button>
                </div>
              </section>
            )}
          </>
        )}
      </section>

      {historyPanel}
    </section>
  )
}

function matchesSelectedTrack(question: { domain: string; difficulty: string; question_type: string }, track: InterviewLaunchTrack) {
  return question.domain === track.domain && question.difficulty === track.difficulty && question.question_type === track.questionType
}

function launchpadTrackToView(track: InterviewLaunchpadTrack): InterviewLaunchTrack | null {
  if (!track.id || !track.domain || !track.difficulty) return null
  const questionType = track.question_type === 'scenario_analysis' ? 'scenario_analysis' : 'principle'
  return {
    id: track.id,
    title: track.title || `${track.domain_label || domainLabel(track.domain)} ${track.difficulty}`,
    domain: track.domain,
    domainLabel: track.domain_label || domainLabel(track.domain),
    category: track.category || track.domain,
    difficulty: normalizeLaunchpadDifficulty(track.difficulty),
    questionType,
    questionRole: normalizeQuestionRole(track.question_role),
    tags: Array.isArray(track.tags) ? track.tags.filter(Boolean) : [],
    availabilityState: normalizeLaunchpadAvailabilityState(track.availability_state, track.vector_status_summary),
    vectorStatusSummary: track.vector_status_summary || 'compatibility_seed',
    unavailableReason: track.unavailable_reason,
    summary: track.summary || track.title || '选择这道题开始面试。',
    details: normalizeTrackDetails(track),
  }
}

function normalizeLaunchpadDifficulty(difficulty: string): InterviewLaunchTrack['difficulty'] {
  if (difficulty === 'L2' || difficulty === 'L3' || difficulty === 'L4' || difficulty === 'L5') return difficulty
  return 'L2'
}

function fallbackLaunchpadSummary(tracks: InterviewLaunchTrack[]): InterviewLaunchpadSummary {
  return {
    open_track_count: tracks.length,
    published_atom_count: 0,
    indexed_atom_count: 0,
    fallback_mode: true,
    state: 'compatibility_fallback',
    message: '',
  }
}

function filterLaunchTracks(tracks: InterviewLaunchTrack[], filters: LaunchpadFilterState) {
  return tracks.filter((track) => {
    if (filters.category && track.category !== filters.category) return false
    if (filters.difficulty && track.difficulty !== filters.difficulty) return false
    return true
  })
}

function uniqueTrackValues(values: string[]) {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b))
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

function normalizeTrackDetails(track: InterviewLaunchpadTrack) {
  const details = Array.isArray(track.details) ? track.details.filter(Boolean) : []
  if (details.length > 0) return details
  const questionTypeLabel = track.question_type === 'scenario_analysis' ? '情景分析' : '原理问答'
  return [questionTypeLabel]
}

function interviewStatusLabel(status: string) {
  const labels: Record<string, string> = {
    question_presented: '待作答',
    answer_submitted: '已作答',
    follow_up_1_presented: '追问中',
    follow_up_2_presented: '追问中',
    final_evaluated: '已完成',
    invalidated: '已作废',
  }
  return labels[status] ?? status
}

function interviewQuestionTypeLabel(type: string) {
  const labels: Record<string, string> = {
    scenario_analysis: '情景分析',
    principle: '原理问答',
  }
  return labels[type] ?? type
}

function interviewDifficultyPromptLabel(value: string) {
  const labels: Record<string, string> = {
    standard: '平衡追问',
    foundation: '从基础聊起',
    challenge: '高压深挖',
  }
  return labels[value] ?? value
}

function interviewFocusAreaPromptLabel(value: InterviewFocusArea) {
  const labels: Record<InterviewFocusArea, string> = {
    technical_accuracy: '技术与原理',
    logical_completeness: '排查路径',
    solution_feasibility: '验证与回滚',
    depth_breadth: '边界与权衡',
    expression_structure: '表达组织',
  }
  return labels[value]
}

function launchpadTrackEmptyMessage(summary: InterviewLaunchpadSummary, hasFilters: boolean) {
  if (hasFilters) {
    return '没有符合当前筛选条件的题目，清空筛选后重新选择。'
  }
  if (summary.published_atom_count === 0 && !summary.fallback_mode) {
    return '暂时没有可用于面试的题目。'
  }
  return '当前没有可用面试题。'
}
