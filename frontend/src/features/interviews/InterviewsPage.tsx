import { type KeyboardEvent, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { BadgeCheck, BarChart3, ChevronDown, ChevronUp, ClipboardList, FileText, History, MessageSquareText, Play, Route, ShieldAlert, Trash2 } from 'lucide-react'
import { api, type InterviewLaunchpadDomain, type InterviewLaunchpadTrack } from '../../api/client'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { formatDateTime } from '../../lib/format'
import { useAuthStore } from '../../stores/authStore'
import type { InterviewFocusArea, InterviewQuestion, InterviewSession, User } from '../../types'
import {
  defaultInterviewFocusAreas,
  interviewDomains as fallbackInterviewDomains,
  interviewDifficultyLevelOptions,
  interviewFlowSteps,
  interviewFocusAreaOptions,
  interviewLaunchTracks as fallbackInterviewLaunchTracks,
  interviewLevels,
  interviewReportOutputs,
  interviewScoreDimensions,
  type InterviewDomainOption,
  type InterviewLaunchTrack,
} from './launchpadConfig'
import './InterviewsPage.css'

type LaunchpadSource = 'loading' | 'api' | 'fallback'

export function InterviewsPage() {
  const token = useToken()
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.user)
  const [launchTracks, setLaunchTracks] = useState<InterviewLaunchTrack[]>(fallbackInterviewLaunchTracks)
  const [launchDomains, setLaunchDomains] = useState<InterviewDomainOption[]>(fallbackInterviewDomains)
  const [launchpadSource, setLaunchpadSource] = useState<LaunchpadSource>('loading')
  const [launchpadNotice, setLaunchpadNotice] = useState('正在读取后端开放组合，当前页面会在接口异常时自动使用兼容轨道。')
  const [selectedTrackId, setSelectedTrackId] = useState(fallbackInterviewLaunchTracks[0]?.id ?? '')
  const [difficultyLevel, setDifficultyLevel] = useState(interviewDifficultyLevelOptions[0]?.value ?? 'standard')
  const [selectedFocusAreas, setSelectedFocusAreas] = useState<InterviewFocusArea[]>(defaultInterviewFocusAreas)
  const [setupNotes, setSetupNotes] = useState(() => interviewSetupNotesFromProfile(user))
  const [startError, setStartError] = useState('')
  const [isStarting, setIsStarting] = useState(false)
  const [historySessions, setHistorySessions] = useState<InterviewSession[]>([])
  const [historyError, setHistoryError] = useState('')
  const [expandedHistoryId, setExpandedHistoryId] = useState('')
  const [historyQuestionDetails, setHistoryQuestionDetails] = useState<Record<string, InterviewQuestion>>({})
  const [loadingHistoryQuestionId, setLoadingHistoryQuestionId] = useState('')
  const [deletingHistoryId, setDeletingHistoryId] = useState('')

  const selectedTrack = useMemo(
    () => launchTracks.find((track) => track.id === selectedTrackId) ?? launchTracks[0],
    [launchTracks, selectedTrackId],
  )

  const profileSetupNotes = useMemo(() => interviewSetupNotesFromProfile(user), [user])

  useEffect(() => {
    let ignore = false
    const applyFallbackLaunchpad = (message: string) => {
      setLaunchTracks(fallbackInterviewLaunchTracks)
      setLaunchDomains(fallbackInterviewDomains)
      setSelectedTrackId((current) => fallbackInterviewLaunchTracks.some((track) => track.id === current) ? current : fallbackInterviewLaunchTracks[0]?.id ?? '')
      setLaunchpadSource('fallback')
      setLaunchpadNotice(message)
    }
    void api.interviewLaunchpad(token)
      .then((res) => {
        if (ignore) return
        const tracks = (res.open_tracks ?? []).map(launchpadTrackToView).filter((track): track is InterviewLaunchTrack => Boolean(track))
        if (tracks.length === 0) {
          applyFallbackLaunchpad('后端暂未返回可启动组合，已使用本地兼容轨道保持训练入口可用。')
          return
        }
        setLaunchTracks(tracks)
        setLaunchDomains(launchpadDomainsToView(res.domains ?? [], tracks))
        setSelectedTrackId((current) => tracks.some((track) => track.id === current) ? current : tracks[0]?.id ?? '')
        setLaunchpadSource(res.fallback_mode ? 'fallback' : 'api')
        setLaunchpadNotice(res.summary?.message || '当前训练轨道由后端开放组合接口返回。')
        setStartError('')
      })
      .catch((err) => {
        if (ignore) return
        const message = err instanceof Error ? err.message : '读取面试启动台失败'
        applyFallbackLaunchpad(`Launchpad 接口暂不可用，已使用本地兼容轨道。原因：${message}`)
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
      })
      .catch((err) => {
        if (ignore) return
        setHistoryError(err instanceof Error ? err.message : '读取历史面试失败')
      })
    return () => {
      ignore = true
    }
  }, [token])

  function selectTrack(trackId: string) {
    setSelectedTrackId(trackId)
    setStartError('')
  }

  function scrollTrackIntoView(trackButton: HTMLButtonElement) {
    trackButton.scrollIntoView({ block: 'nearest' })
  }

  function handleTrackKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (!['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const trackGrid = event.currentTarget.parentElement
    const lastIndex = launchTracks.length - 1
    if (lastIndex < 0) return
    const nextIndex = (() => {
      if (event.key === 'Home') return 0
      if (event.key === 'End') return lastIndex
      if (event.key === 'ArrowDown' || event.key === 'ArrowRight') return index === lastIndex ? 0 : index + 1
      return index === 0 ? lastIndex : index - 1
    })()
    const nextTrack = launchTracks[nextIndex]
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

  async function start() {
    if (!selectedTrack || isStarting) return
    setStartError('')
    setIsStarting(true)
    try {
      const res = await api.createInterview(token, {
        domain: selectedTrack.domain,
        difficulty: selectedTrack.difficulty,
        question_type: selectedTrack.questionType,
        difficulty_level: difficultyLevel,
        focus_areas: selectedFocusAreas,
        setup_notes: setupNotes.trim(),
      })
      if (!matchesSelectedTrack(res.question, selectedTrack)) {
        setStartError('题目与所选训练轨道不一致，请稍后重试或联系管理员补齐题库。')
        return
      }
      navigate(`/interviews/session/${res.session_id}`, { state: { question: res.question, session: res.session } })
    } catch (err) {
      setStartError(err instanceof Error ? err.message : '面试启动失败，请稍后重试。')
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

  return (
    <section className="page-stack interview-launchpad">
      <section className="interview-command-hero" aria-labelledby="interview-command-title">
        <div className="interview-poster-meta">
          <span>INTERVIEW CABIN / 2026</span>
          <small>COMMAND SCREEN</small>
        </div>

        <div className="interview-title-line">
          <div className="title-icon interview-title-icon"><MessageSquareText size={22} /></div>
          <h1 id="interview-command-title">技术面试舱</h1>
        </div>

        <div className="interview-hero-grid">
          <div className="interview-command-copy">
            <span className="interview-super-title">INTERVIEW</span>
            <span className="interview-subtitle">L2-L3 OPEN TRACKS</span>
            <h2>面试启动台</h2>
            <div className="checked-action-list interview-chip-row">
              <span>首期开放 L2-L3</span>
              <span>五维评分</span>
              <span>最多 3 轮追问</span>
            </div>
          </div>

          <div className="launch-summary interview-start-panel" data-testid="interview-launch-summary">
            <div className="interview-start-content">
              <div className="interview-start-copy">
                <span>本轮配置</span>
                <strong>{selectedTrack ? `${selectedTrack.domainLabel} / ${selectedTrack.difficulty}` : '暂无可启动轨道'}</strong>
                <p>{selectedTrack?.summary}</p>
              </div>
              <div className="interview-dimension-meter" aria-label="五维评分维度">
                <span>五维评分维度</span>
                <div>
                  {interviewScoreDimensions.map((item) => (
                    <small key={item}>{item}</small>
                  ))}
                </div>
              </div>
            </div>
            <div className="interview-session-setup" aria-label="本场会话输入">
              <div className="interview-setup-group">
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
              <fieldset className="interview-setup-group interview-focus-fieldset">
                <legend>考察重点</legend>
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
              </fieldset>
              <label className="interview-setup-group interview-setup-notes">
                <span>准备备注</span>
                <textarea
                  value={setupNotes}
                  onChange={(event) => setSetupNotes(event.target.value)}
                  maxLength={2000}
                  placeholder={profileSetupNotes || '补充本场希望强调的简历或项目背景。'}
                />
              </label>
            </div>
            {startError && (
              <div className="launch-error" role="alert">
                <ShieldAlert size={16} />
                {startError}
              </div>
            )}
            <button className="primary-button launch-start-button" type="button" onClick={() => void start()} disabled={!selectedTrack || isStarting}>
              <Play size={18} />
              {isStarting ? '启动中' : '开始面试'}
            </button>
          </div>
        </div>
      </section>

      <section className="interview-command-grid">
        <section className="panel launch-section interview-track-panel launch-section-primary" data-testid="interview-track-section">
          <div className="interview-track-panel-head">
            <div>
              <div className="panel-title"><Route size={18} />可启动训练轨道</div>
              <p className="launch-section-note">当前题库入口已完成入库校验，可直接选择轨道进入面试流程。</p>
              <div className={`interview-launchpad-status ${launchpadSource}`} role={launchpadSource === 'fallback' ? 'status' : undefined}>
                <span>{launchpadSource === 'loading' ? '同步中' : launchpadSource === 'api' ? '后端开放组合' : '兼容轨道'}</span>
                <small>{launchpadNotice}</small>
              </div>
            </div>
            <button className="primary-button compact interview-track-start-button" type="button" onClick={() => void start()} disabled={!selectedTrack || isStarting}>
              <Play size={16} />
              {isStarting ? '启动中' : '开始面试'}
            </button>
          </div>
          <div className="track-grid" role="radiogroup" aria-label="可启动训练轨道" data-testid="interview-track-grid">
            {launchTracks.map((track, index) => (
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
                onKeyDown={(event) => handleTrackKeyDown(event, index)}
              >
                <span>{track.title}</span>
                <strong>{track.summary}</strong>
                <small>{track.details.join(' · ')}</small>
              </button>
            ))}
          </div>
        </section>

        <section className="panel launch-section interview-pipeline-panel">
          <div className="panel-title"><BarChart3 size={18} />ANSWER PIPELINE</div>
          <h3>结构化问答 / AI 追问 / 能力画像</h3>
          <div className="interview-signal-map" aria-hidden="true">
            <span />
            <span />
            <span className="alert" />
            <span />
            <span />
          </div>
          <div className="interview-score-bars" aria-hidden="true">
            <span />
            <span />
            <span />
          </div>
          <p>{interviewScoreDimensions.join('、')}</p>
        </section>

        <section className="panel launch-section launch-section-compact interview-level-panel" data-testid="interview-level-section">
          <div className="panel-title"><BadgeCheck size={18} />岗位级别模型</div>
          <div className="level-table" data-testid="interview-level-table">
            {interviewLevels.map((level) => (
              <div key={level.value} className="level-row">
                <strong>{level.value}</strong>
                <span>{level.role}</span>
                <span>{level.audience}</span>
                <p>{level.focus}</p>
              </div>
            ))}
          </div>
        </section>

        <section className="panel launch-section interview-domain-panel">
          <div className="panel-title"><ClipboardList size={18} />专业领域</div>
          <div className="domain-cluster-grid">
            {launchDomains.map((domain) => {
              const hasLaunchTrack = launchTracks.some((item) => item.domain === domain.value)
              return hasLaunchTrack ? (
                <button
                  key={domain.value}
                  type="button"
                  className="domain-chip enabled launchable"
                  data-testid={`interview-domain-${domain.value}`}
                  onClick={() => {
                    const track = launchTracks.find((item) => item.domain === domain.value)
                    if (track) {
                      setSelectedTrackId(track.id)
                      setStartError('')
                    }
                  }}
                >
                  <span>{domain.label}</span>
                  <small>{domain.group} · {domain.note}</small>
                </button>
              ) : (
                <div key={domain.value} className="domain-chip enabled catalogued" data-testid={`interview-domain-${domain.value}`}>
                  <span>{domain.label}</span>
                  <small>{domain.group} · {domain.note}</small>
                </div>
              )
            })}
          </div>
        </section>

        <section className="panel launch-section interview-report-panel">
          <div className="panel-title"><FileText size={18} />评分与报告产物</div>
          <div className="dimension-list">
            {interviewScoreDimensions.map((item) => <span key={item}>{item}</span>)}
          </div>
          <ul className="report-output-list">
            {interviewReportOutputs.map((item) => <li key={item}>{item}</li>)}
          </ul>
        </section>

        <section className="panel launch-section interview-flow-panel">
          <div className="panel-title"><BarChart3 size={18} />面试流程</div>
          <div className="launch-step-list">
            {interviewFlowSteps.map((step, index) => (
              <div key={step.title} className="launch-step">
                <span>{index + 1}</span>
                <strong>{step.title}</strong>
                <p>{step.description}</p>
              </div>
            ))}
          </div>
        </section>
      </section>

      <section className="panel launch-section interview-history-panel" data-testid="interview-history-panel">
        <div className="interview-history-head">
          <div>
            <div className="panel-title"><History size={18} />历史面试</div>
            <p>默认收起题目正文，保留复盘入口和继续训练路径。</p>
          </div>
          <span>{historySessions.length} 条记录</span>
        </div>
        {historyError && <div className="launch-error" role="alert"><ShieldAlert size={16} />{historyError}</div>}
        {historySessions.length > 0 ? (
          <div className="interview-history-list">
            {historySessions.map((session) => {
              const question = historyQuestionDetails[session.id]
              const isExpanded = expandedHistoryId === session.id
              const isFinal = session.status === 'final_evaluated'
              return (
                <article className="interview-history-item" key={session.id}>
                  <div className="interview-history-summary">
                    <div>
                      <span>{interviewStatusLabel(session.status)}</span>
                      <strong>面试 #{session.id.slice(0, 8)}</strong>
                      <small>{formatDateTime(session.ended_at || session.started_at || '')}</small>
                    </div>
                    <div className="interview-history-metrics">
                      <span>{session.current_round}/{session.max_rounds} 轮</span>
                      <span>{typeof session.final_score === 'number' ? `${session.final_score} 分` : '未出分'}</span>
                    </div>
                    <div className="interview-history-actions">
                      <button className="ghost-button compact" type="button" onClick={() => void toggleHistoryQuestion(session.id)}>
                        {isExpanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
                        {isExpanded ? '收起题目' : '查看题目'}
                      </button>
                      <button
                        className="ghost-button compact interview-history-delete"
                        type="button"
                        onClick={() => void deleteHistorySession(session.id)}
                        disabled={deletingHistoryId === session.id}
                      >
                        <Trash2 size={16} />
                        {deletingHistoryId === session.id ? '删除中' : '删除记录'}
                      </button>
                      {isFinal ? (
                        <a className="ghost-button compact" href={`/interviews/session/${session.id}/report`}>历史报告</a>
                      ) : (
                        <a className="ghost-button compact" href={`/interviews/session/${session.id}`}>继续面试</a>
                      )}
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
          <div className="empty-inline">暂无历史面试记录。完成一次面试后可在这里复盘题目和报告。</div>
        )}
      </section>
    </section>
  )
}

function matchesSelectedTrack(question: { domain: string; difficulty: string; question_type: string }, track: InterviewLaunchTrack) {
  return question.domain === track.domain && question.difficulty === track.difficulty && question.question_type === track.questionType
}

function interviewSetupNotesFromProfile(user: User | null) {
  const parts = [
    user?.profile.resume_summary ? `简历摘要：${user.profile.resume_summary}` : '',
    user?.profile.project_summary ? `项目摘要：${user.profile.project_summary}` : '',
  ].filter(Boolean)
  return parts.join('\n')
}

function launchpadTrackToView(track: InterviewLaunchpadTrack): InterviewLaunchTrack | null {
  if (!track.id || !track.domain || !track.difficulty) return null
  const questionType = track.question_type === 'scenario_analysis' ? 'scenario_analysis' : 'principle'
  return {
    id: track.id,
    title: track.title || `${track.domain_label || domainLabel(track.domain)} ${track.difficulty}`,
    domain: track.domain,
    domainLabel: track.domain_label || domainLabel(track.domain),
    difficulty: normalizeLaunchpadDifficulty(track.difficulty),
    questionType,
    summary: track.summary || '后端开放组合已就绪，可进入一场完整面试训练。',
    details: normalizeTrackDetails(track),
  }
}

function normalizeLaunchpadDifficulty(difficulty: string): InterviewLaunchTrack['difficulty'] {
  if (difficulty === 'L2' || difficulty === 'L3' || difficulty === 'L4' || difficulty === 'L5') return difficulty
  return 'L2'
}

function normalizeTrackDetails(track: InterviewLaunchpadTrack) {
  const details = Array.isArray(track.details) ? track.details.filter(Boolean) : []
  if (details.length > 0) return details
  const questionTypeLabel = track.question_type === 'scenario_analysis' ? '情景分析' : '原理问答'
  return [questionTypeLabel, track.availability_state === 'indexing' ? '追问增强准备中' : '可启动训练', `题库状态：${track.vector_status_summary || '兼容模式'}`]
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
    final_evaluated: '最终评价',
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
