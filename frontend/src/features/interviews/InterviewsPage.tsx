import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, ChevronUp, ClipboardList, History, MessageSquareText, Play, Route, ShieldAlert, Trash2 } from 'lucide-react'
import { api, type InterviewLaunchpadDomain, type InterviewLaunchpadSummary, type InterviewLaunchpadTrack } from '../../api/client'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { formatDateTime } from '../../lib/format'
import type { InterviewFocusArea, InterviewQuestion, InterviewSession } from '../../types'
import {
  defaultInterviewFocusAreas,
  interviewDomains as fallbackInterviewDomains,
  interviewDifficultyLevelOptions,
  interviewFocusAreaOptions,
  interviewLaunchTracks as fallbackInterviewLaunchTracks,
  interviewScoreDimensions,
  type InterviewDomainOption,
  type InterviewLaunchTrack,
} from './launchpadConfig'
import './InterviewsPage.css'

type LaunchpadSource = 'loading' | 'api' | 'fallback'
type LaunchpadFilterState = {
  category: string
  difficulty: string
  questionRole: string
  tag: string
}

export function InterviewsPage() {
  const token = useToken()
  const navigate = useNavigate()
  const [launchTracks, setLaunchTracks] = useState<InterviewLaunchTrack[]>(fallbackInterviewLaunchTracks)
  const [launchDomains, setLaunchDomains] = useState<InterviewDomainOption[]>(fallbackInterviewDomains)
  const [launchSummary, setLaunchSummary] = useState<InterviewLaunchpadSummary>(() => fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
  const [trackFilters, setTrackFilters] = useState<LaunchpadFilterState>({ category: '', difficulty: '', questionRole: '', tag: '' })
	const [launchpadSource, setLaunchpadSource] = useState<LaunchpadSource>('loading')
	const [launchpadNotice, setLaunchpadNotice] = useState('正在读取可启动的训练组合。')
	const [isLaunchpadLoading, setLaunchpadLoading] = useState(true)
	const [selectedTrackId, setSelectedTrackId] = useState(fallbackInterviewLaunchTracks[0]?.id ?? '')
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
  const trackSectionRef = useRef<HTMLElement | null>(null)

  const trackFilterOptions = useMemo(() => ({
    categories: uniqueTrackValues(launchTracks.map((track) => track.category)),
    difficulties: uniqueTrackValues(launchTracks.map((track) => track.difficulty)),
    questionRoles: uniqueTrackValues(launchTracks.map((track) => track.questionRole)),
    tags: uniqueTrackValues(launchTracks.flatMap((track) => track.tags ?? [])),
  }), [launchTracks])

  const visibleLaunchTracks = useMemo(() => filterLaunchTracks(launchTracks, trackFilters), [launchTracks, trackFilters])

  const selectedTrack = useMemo(
    () => visibleLaunchTracks.find((track) => track.id === selectedTrackId) ?? visibleLaunchTracks[0],
    [selectedTrackId, visibleLaunchTracks],
  )

  const hasActiveFilters = Boolean(trackFilters.category || trackFilters.difficulty || trackFilters.questionRole || trackFilters.tag)

  useEffect(() => {
    let ignore = false
    const applyFallbackLaunchpad = (message: string) => {
      setLaunchTracks(fallbackInterviewLaunchTracks)
      setLaunchDomains(fallbackInterviewDomains)
      setLaunchSummary(fallbackLaunchpadSummary(fallbackInterviewLaunchTracks))
      setSelectedTrackId((current) => fallbackInterviewLaunchTracks.some((track) => track.id === current) ? current : fallbackInterviewLaunchTracks[0]?.id ?? '')
      setLaunchpadSource('fallback')
      setLaunchpadNotice(message)
    }
    void api.interviewLaunchpad(token)
      .then((res) => {
        if (ignore) return
        const tracks = (res.open_tracks ?? []).map(launchpadTrackToView).filter((track): track is InterviewLaunchTrack => Boolean(track))
        if (tracks.length === 0 && res.fallback_mode) {
          applyFallbackLaunchpad('后端暂未返回可启动组合，已使用本地兼容轨道保持训练入口可用。')
          return
        }
        setLaunchTracks(tracks)
        setLaunchDomains(launchpadDomainsToView(res.domains ?? [], tracks))
		setLaunchSummary(res.summary ?? fallbackLaunchpadSummary(tracks))
		setLaunchpadSource(res.fallback_mode ? 'fallback' : 'api')
		setLaunchpadNotice(res.summary?.message || '训练轨道已就绪，选择后即可开始。')
		setSelectedTrackId((current) => {
          if (tracks.length === 0) return ''
          return tracks.some((track) => track.id === current) ? current : tracks[0]?.id ?? ''
        })
        setStartError('')
        setLaunchpadLoading(false)
      })
      .catch((err) => {
        if (ignore) return
		const message = err instanceof Error ? err.message : '读取面试启动台失败'
		applyFallbackLaunchpad(`接口暂不可用，已使用本地兼容轨道。原因：${message}`)
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
    setSelectedTrackId(trackId)
    setStartError('')
  }

  function updateTrackFilter<K extends keyof LaunchpadFilterState>(key: K, value: LaunchpadFilterState[K]) {
    const nextFilters = { ...trackFilters, [key]: value }
    const nextTracks = filterLaunchTracks(launchTracks, nextFilters)
    setTrackFilters(nextFilters)
    setSelectedTrackId((current) => (nextTracks.some((track) => track.id === current) ? current : nextTracks[0]?.id ?? ''))
    setStartError('')
  }

  function clearTrackFilters() {
    setTrackFilters({ category: '', difficulty: '', questionRole: '', tag: '' })
    setStartError('')
  }

  function toggleDomainFilter(domainValue: string) {
    const nextCategory = trackFilters.category === domainValue ? '' : domainValue
    const nextFilters = { ...trackFilters, category: nextCategory }
    const nextTracks = filterLaunchTracks(launchTracks, nextFilters)
    const preferredTrackId = nextCategory
      ? launchTracks.find((track) => track.domain === domainValue || track.category === domainValue)?.id
      : undefined
    const retainedTrackId = nextTracks.some((track) => track.id === selectedTrackId) ? selectedTrackId : ''
    setTrackFilters(nextFilters)
    setSelectedTrackId(preferredTrackId || retainedTrackId || nextTracks[0]?.id || '')
    setStartError('')
    window.requestAnimationFrame(() => {
      trackSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    })
  }

  const launchpadStatusText = launchpadStatusSummaryText(launchSummary, launchpadSource)
  const launchpadTrackEmptyText = launchpadTrackEmptyMessage(launchSummary, hasActiveFilters)

  function scrollTrackIntoView(trackButton: HTMLButtonElement) {
    trackButton.scrollIntoView({ block: 'nearest' })
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
    selectTrack(nextTrack.id)
    window.requestAnimationFrame(() => {
      const radios = trackGrid?.querySelectorAll<HTMLButtonElement>('[role="radio"]')
      const nextRadio = radios?.[nextIndex]
      nextRadio?.focus()
      if (nextRadio) scrollTrackIntoView(nextRadio)
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

  async function start(trackOverride?: InterviewLaunchTrack) {
    const track = trackOverride ?? selectedTrack
    if (!track || isStarting) return
    setSelectedTrackId(track.id)
    setStartError('')
    setIsStarting(true)
    try {
      // 先确认会话页代码可加载，再创建后端会话，避免页面资源失败时留下无效历史记录。
      await import('./InterviewSessionRoute')
      const res = await api.createInterview(token, {
        domain: track.domain,
        difficulty: track.difficulty,
        question_type: track.questionType,
        difficulty_level: difficultyLevel,
        focus_areas: selectedFocusAreas,
        setup_notes: '',
      })
      if (!matchesSelectedTrack(res.question, track)) {
        setStartError('题目与所选训练轨道不一致，请稍后重试或联系管理员补齐题库。')
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
    <section className="panel interview-history-panel" data-testid="interview-history-panel">
      <div className="interview-history-head">
        <div className="panel-title"><History size={18} />历史面试</div>
        <span>{historySessions.length} 条记录</span>
      </div>
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
                      {isExpanded ? '收起' : '题目'}
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
        <div className="empty-inline">暂无历史面试记录，完成一场后会出现在这里。</div>
      )}
    </section>
  )

  return (
    <section className="page-stack interview-launchpad">
      <section className="interview-command-hero" aria-labelledby="interview-command-title">
        <div className="interview-page-head">
          <div className="interview-title-line">
            <div className="title-icon interview-title-icon"><MessageSquareText size={22} /></div>
            <div>
              <h1 id="interview-command-title">技术面试舱</h1>
              <p>选好训练轨道，一键开始；历史面试与报告都在同一屏。</p>
            </div>
          </div>
          <div className={`interview-launchpad-status ${launchpadSource}`} role={launchpadSource === 'fallback' ? 'status' : undefined}>
            <span>{launchpadSource === 'loading' ? '同步中' : launchpadSource === 'api' ? '题库已连接' : '兼容模式'}</span>
            <small>{launchpadStatusText || launchpadNotice}</small>
          </div>
        </div>

        <div className="interview-hero-grid">
          <div className="launch-summary interview-start-panel" data-testid="interview-launch-summary">
            <div className="interview-start-top">
              <span className="interview-start-eyebrow">本轮训练</span>
              <strong className="interview-start-track">{selectedTrack ? `${selectedTrack.domainLabel} · ${selectedTrack.difficulty}` : '暂无可启动轨道'}</strong>
              <p className="interview-start-summary">{selectedTrack?.summary ?? '连接题库后即可选择训练轨道。'}</p>
            </div>

            <div className="interview-start-facts">
              <div className="interview-start-fact">
                <span>可面试题目</span>
                <strong>{launchSummary.published_atom_count}</strong>
              </div>
              <div className="interview-start-fact">
                <span>可启动组合</span>
                <strong>{launchSummary.open_track_count}</strong>
              </div>
              <div className="interview-start-fact">
                <span>预计用时</span>
                <strong>{selectedTrack ? interviewTrackDurationLabel(selectedTrack) : '—'}</strong>
              </div>
            </div>

            <div className="interview-start-dimensions" aria-label="五维评分维度">
              <span>五维评分维度</span>
              <div className="interview-dimension-tags">
                {interviewScoreDimensions.map((item) => (
                  <small key={item}>{item}</small>
                ))}
              </div>
            </div>

            <div className="interview-start-difficulty">
              <span>难度倾向</span>
              <div className="interview-segmented-control" role="radiogroup" aria-label="本场难度倾向">
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
                    {option.label}
                  </button>
                ))}
              </div>
            </div>

            <details className="interview-start-more">
              <summary>考察重点（可选）</summary>
              <div className="interview-focus-grid">
                {interviewFocusAreaOptions.map((option) => (
                  <label key={option.value} title={option.note}>
                    <input
                      type="checkbox"
                      checked={selectedFocusAreas.includes(option.value)}
                      onChange={() => toggleFocusArea(option.value)}
                    />
                    <span>{option.label}</span>
                  </label>
                ))}
              </div>
            </details>

            {startError && (
              <div className="launch-error" role="alert">
                <ShieldAlert size={16} />
                {startError}
              </div>
            )}

            <button className="primary-button launch-start-button" type="button" onClick={() => void start()} disabled={!selectedTrack || isStarting}>
              <Play size={20} />
              {isStarting ? '启动中…' : '开始面试'}
            </button>
          </div>

          {historyPanel}
        </div>
      </section>

      <section className="interview-command-grid">
        <section className="panel launch-section interview-track-panel launch-section-primary" data-testid="interview-track-section" ref={trackSectionRef}>
          <div className="interview-track-panel-head">
            <div className="panel-title"><Route size={18} />选择训练轨道</div>
            <button className="primary-button compact interview-track-start-button" type="button" onClick={() => void start()} disabled={!selectedTrack || isStarting}>
              <Play size={16} />
              {isStarting ? '启动中…' : '开始面试'}
            </button>
          </div>
          {isLaunchpadLoading ? (
            <div data-testid="launchpad-track-loading-skeleton">
              <div className="interview-track-filter-bar skeleton">
                <div className="interview-panel-skeleton-line" />
                <div className="interview-panel-skeleton-line" />
                <div className="interview-panel-skeleton-line" />
                <div className="interview-panel-skeleton-line" />
              </div>
              <div className="interview-panel-skeleton-list">
                <div className="interview-panel-skeleton-wide" />
                <div className="interview-panel-skeleton-wide" />
              </div>
            </div>
          ) : (
            <>
              <div className="interview-track-filter-bar">
                <select value={trackFilters.category} onChange={(event) => updateTrackFilter('category', event.target.value)} aria-label="按分类筛选开放轨道">
                  <option value="">全部分类</option>
                  {trackFilterOptions.categories.map((value) => <option value={value} key={`category-${value}`}>{domainLabel(value)}</option>)}
                </select>
                <select value={trackFilters.difficulty} onChange={(event) => updateTrackFilter('difficulty', event.target.value)} aria-label="按难度筛选开放轨道">
                  <option value="">全部难度</option>
                  {trackFilterOptions.difficulties.map((value) => <option value={value} key={`difficulty-${value}`}>{value}</option>)}
                </select>
                <select value={trackFilters.questionRole} onChange={(event) => updateTrackFilter('questionRole', event.target.value)} aria-label="按题目角色筛选开放轨道">
                  <option value="">全部角色</option>
                  {trackFilterOptions.questionRoles.map((value) => <option value={value} key={`role-${value}`}>{questionRoleLabel(value)}</option>)}
                </select>
                {hasActiveFilters && (
                  <button type="button" className="ghost-button compact" onClick={clearTrackFilters}>清空</button>
                )}
              </div>
              <div className="track-grid" role="radiogroup" aria-label="可启动训练轨道" data-testid="interview-track-grid">
                {visibleLaunchTracks.length > 0 ? visibleLaunchTracks.map((track, index) => (
                  <button
                    key={track.id}
                    type="button"
                    role="radio"
                    aria-checked={selectedTrackId === track.id}
                    tabIndex={selectedTrackId === track.id ? 0 : -1}
                    className={`track-card ${selectedTrackId === track.id ? 'active' : ''}`}
                    onClick={(event) => {
                      selectTrack(track.id)
                      scrollTrackIntoView(event.currentTarget)
                    }}
                    onDoubleClick={() => void start(track)}
                    onKeyDown={(event) => handleTrackKeyDown(event, index)}
                  >
                    <div className="track-card-head">
                      <span>{track.title}</span>
                      <em className={`track-availability ${track.availabilityState}`}>{launchTrackAvailabilityLabel(track, launchpadSource)}</em>
                    </div>
                    <strong>{track.summary}</strong>
                    <small>{questionRoleLabel(track.questionRole)} · {domainLabel(track.category)} · {track.difficulty} · {interviewQuestionTypeLabel(track.questionType)}</small>
                  </button>
                )) : (
                  <div className="empty-inline">{launchpadTrackEmptyText}</div>
                )}
              </div>
            </>
          )}
        </section>

        <section className="panel launch-section interview-domain-panel" data-testid="interview-domain-section">
          <div className="panel-title"><ClipboardList size={18} />专业领域</div>
          <div className="domain-cluster-grid">
            {launchDomains.map((domain) => {
              const hasLaunchTrack = launchTracks.some((item) => item.domain === domain.value)
              return hasLaunchTrack ? (
                <button
                  key={domain.value}
                  type="button"
                  aria-pressed={trackFilters.category === domain.value}
                  className={`domain-chip enabled launchable ${trackFilters.category === domain.value ? 'active' : ''}`}
                  data-testid={`interview-domain-${domain.value}`}
                  onClick={() => toggleDomainFilter(domain.value)}
                >
                  <span>{domain.label}</span>
                  <small>{domain.note}</small>
                </button>
              ) : (
                <div key={domain.value} className="domain-chip enabled catalogued" data-testid={`interview-domain-${domain.value}`}>
                  <span>{domain.label}</span>
                  <small>{domain.note}</small>
                </div>
              )
            })}
          </div>
        </section>
      </section>
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
    summary: track.summary || '训练组合已就绪，可进入一场完整面试训练。',
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
    message: '当前使用兼容轨道，正式题库聚合不可用时仍可继续启动面试。',
  }
}

function filterLaunchTracks(tracks: InterviewLaunchTrack[], filters: LaunchpadFilterState) {
  return tracks.filter((track) => {
    if (filters.category && track.category !== filters.category) return false
    if (filters.difficulty && track.difficulty !== filters.difficulty) return false
    if (filters.questionRole && track.questionRole !== filters.questionRole) return false
    if (filters.tag && !(track.tags ?? []).includes(filters.tag)) return false
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
  return [questionTypeLabel, track.availability_state === 'indexing' ? '追问增强准备中' : '可启动训练']
}

function launchpadDomainsToView(domains: InterviewLaunchpadDomain[], tracks: InterviewLaunchTrack[]): InterviewDomainOption[] {
  if (domains.length > 0) {
    return domains.map((item) => ({
      value: item.value,
      label: item.label || domainLabel(item.value),
      group: item.group || '后端开放',
      note: item.note || `${item.open_track_count ?? tracks.filter((track) => track.domain === item.value).length} 个训练入口`,
    }))
  }
  const seen = new Set<string>()
  return tracks.flatMap((track) => {
    if (seen.has(track.domain)) return []
    seen.add(track.domain)
    return [{
      value: track.domain,
      label: track.domainLabel,
      group: '后端开放',
      note: `${tracks.filter((item) => item.domain === track.domain).length} 个训练入口`,
    }]
  })
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

function questionRoleLabel(value: string) {
  const labels: Record<string, string> = {
    opening: '开场',
    followup: '追问',
    mixed: '混合',
  }
  return labels[value] ?? value
}

function launchTrackAvailabilityLabel(track: InterviewLaunchTrack, source: LaunchpadSource) {
  if (source === 'fallback' || track.availabilityState === 'fallback') return '兼容轨道'
  if (track.availabilityState === 'indexing') return '增强中'
  return '可训练'
}

function launchpadStatusSummaryText(summary: InterviewLaunchpadSummary, source: LaunchpadSource) {
  if (source === 'fallback' || summary.state === 'compatibility_fallback') {
    return '当前仍可训练，使用的是兼容轨道；正式题库恢复后会自动切回。'
  }
  if (summary.state === 'retrieval_degraded') {
    return '开场题可用，追问增强降级中，会自动回退到规则链路。'
  }
  if (summary.state === 'retrieval_partial') {
    return '部分组合完成追问增强，其余组合会按可用状态降级。'
  }
  if (summary.state === 'empty') {
    return '题库已接入，但暂时没有满足启动条件的训练组合。'
  }
  return summary.message
}

function launchpadTrackEmptyMessage(summary: InterviewLaunchpadSummary, hasFilters: boolean) {
  if (hasFilters) {
    return '没有符合当前筛选条件的训练组合，清空筛选后重新选择。'
  }
  if (summary.published_atom_count === 0 && !summary.fallback_mode) {
    return '还没有可用于面试的已发布题库，请等待管理员完成导入和发布。'
  }
  if (summary.open_track_count === 0 && !summary.fallback_mode) {
    return '题库已发布，但暂未满足任何训练组合的启动条件。'
  }
  return '当前没有可用训练组合。'
}

function interviewTrackDurationLabel(track: InterviewLaunchTrack) {
  const baseMinutes = track.questionType === 'scenario_analysis' ? 28 : 20
  const difficultyOffset: Record<InterviewLaunchTrack['difficulty'], number> = {
    L2: 0,
    L3: 5,
    L4: 10,
    L5: 15,
  }
  return `${baseMinutes + difficultyOffset[track.difficulty]} 分钟`
}
