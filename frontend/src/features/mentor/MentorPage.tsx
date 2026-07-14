import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Bot, ClipboardList, Compass, Sparkles } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState, HeaderBlock, Loading, Metric } from '../../components/common'
import { api, type MentorSnapshot } from '../../api/client'
import { useToken } from '../../lib/auth'
import './MentorPage.css'

type MentorRisk = {
  level: 'info' | 'warning' | 'danger'
  title: string
  message: string
}

type MentorAction = {
  title: string
  detail: string
  actionLabel: string
  actionPath: string
}

export function MentorPage() {
  const token = useToken()
  const [data, setData] = useState<Awaited<ReturnType<typeof api.mentor>> | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let ignore = false
    void api.mentor(token)
      .then((snapshot) => {
        if (ignore) return
        setData(snapshot)
        setError('')
      })
      .catch((err) => {
        if (ignore) return
        const message = err instanceof Error ? err.message : '读取 Mentor 数据失败'
        if (isMentorEndpointUnavailable(message)) {
          setData(mentorCompatibilitySnapshot())
          setError('')
          return
        }
        setError(message)
      })
    return () => {
      ignore = true
    }
  }, [token])

  const diagnosis = useMemo(() => {
    if (!data) return { overview: '', strengths: [] as string[], weaknesses: [] as string[] }
    return {
      overview: data.overview || '完成更多排查和面试样本后，这里会形成更稳定的综合诊断。',
      strengths: data.strengths ?? [],
      weaknesses: data.weaknesses ?? [],
    }
  }, [data])

  const risks = useMemo(() => {
    return (data?.risks ?? []) as MentorRisk[]
  }, [data])

  const actions = useMemo(() => {
    if (!data) return [] as MentorAction[]
    const mapped = (data.actions ?? []).map((item) => ({
      title: item.title,
      detail: item.detail,
      actionLabel: item.action_label || '进入训练',
      actionPath: item.action_path || '/dashboard',
    }))
    if (mapped.length === 0) {
      mapped.push({
        title: '补齐第一轮训练样本',
        detail: '先完成一轮排查题或面试题，Mentor 才能给出更稳定的诊断。',
        actionLabel: '进入面试舱',
        actionPath: '/interviews',
      })
    }
    return mapped
  }, [data])

  const uncoveredTracks = useMemo(() => {
    return data?.coverage?.uncovered_tracks ?? []
  }, [data])

  if (!data && !error) return <Loading title="读取 AI Mentor" />

  if (error) {
    return (
      <section className="page-stack mentor-page">
        <HeaderBlock
          icon={<Bot size={22} />}
          title="AI Mentor"
          description="集中查看当前训练诊断、风险提醒和下一步行动。"
        />
        <div className="panel mentor-error-panel">
          <strong>读取 AI Mentor 失败</strong>
          <p>{error}</p>
          <Link className="primary-button compact" to="/dashboard">返回仪表盘</Link>
        </div>
      </section>
    )
  }

  if (!data) return null

  const snapshot = data
  const interviewHistory = snapshot.sample_ready
  const coverageStats = snapshot.coverage

  return (
    <section className="page-stack mentor-page">
      <HeaderBlock
        icon={<Bot size={22} />}
        title="AI Mentor"
        description="把面试、排查、题库覆盖和个人画像收成一页，集中给出当前诊断。"
      />
      <section className="mentor-hero-grid">
        <div className="panel mentor-hero-card">
          <span>当前综合结论</span>
          <strong>{diagnosis.overview}</strong>
          <small>目标职级：{mentorTargetLevelLabel(snapshot.profile.target_level)} · 目标岗位：{snapshot.profile.target_role?.trim() || '待补充'}</small>
        </div>
        <div className="metric-row mentor-metrics">
          <Metric label="开放轨道覆盖率" value={`${coverageStats?.coverage_percent ?? 0}%`} />
          <Metric label="已完成面试" value={coverageStats?.completed_sessions ?? 0} />
          <Metric label="知识点样本" value={coverageStats?.subject_count ?? 0} />
        </div>
      </section>

      {!interviewHistory ? (
        <EmptyState
          title="还没有足够的面试样本"
          description="先完成一次面试后，AI Mentor 会开始聚合你的薄弱维度、知识覆盖和行动建议。"
          action={<Link className="primary-button compact" to="/interviews">进入面试舱</Link>}
        />
      ) : null}

      <div className="two-column mentor-diagnosis-grid">
        <section className="panel mentor-panel">
          <div className="panel-title"><Sparkles size={18} /> 综合诊断</div>
          <div className="mentor-list-block">
            <span>优势</span>
            <ul>
              {diagnosis.strengths.length > 0 ? diagnosis.strengths.map((item) => <li key={item}>{item}</li>) : <li>当前还没有稳定优势结论。</li>}
            </ul>
          </div>
          <div className="mentor-list-block">
            <span>待提升</span>
            <ul>
              {diagnosis.weaknesses.length > 0 ? diagnosis.weaknesses.map((item) => <li key={item}>{item}</li>) : <li>当前还没有明确的弱项结论。</li>}
            </ul>
          </div>
        </section>

        <section className="panel mentor-panel">
          <div className="panel-title"><AlertTriangle size={18} /> 风险预警</div>
          <div className="mentor-risk-list">
            {risks.length > 0 ? risks.map((risk) => (
              <div key={`${risk.level}-${risk.title}`} className={`mentor-risk-item ${risk.level}`}>
                <strong>{risk.title}</strong>
                <p>{risk.message}</p>
              </div>
            )) : (
              <div className="empty-inline">当前没有需要额外提醒的风险项。</div>
            )}
          </div>
        </section>
      </div>

      <div className="two-column mentor-action-grid">
        <section className="panel mentor-panel">
          <div className="panel-title"><Compass size={18} /> 建议行动</div>
          <div className="mentor-action-list">
            {actions.map((item) => (
              <div key={`${item.actionPath}-${item.title}`} className="mentor-action-item">
                <div>
                  <strong>{item.title}</strong>
                  <p>{item.detail}</p>
                </div>
                <Link className="ghost-button compact" to={item.actionPath}>{item.actionLabel}</Link>
              </div>
            ))}
          </div>
        </section>

        <section className="panel mentor-panel">
          <div className="panel-title"><ClipboardList size={18} /> 知识覆盖</div>
          <div className="mentor-list-block">
            <span>高频知识点</span>
            <div className="dimension-list">
              {(coverageStats?.top_subjects?.length ? coverageStats.top_subjects : ['暂无']).map((item) => <span key={item}>{item}</span>)}
            </div>
          </div>
          <div className="mentor-list-block">
            <span>待补方向</span>
            <div className="dimension-list">
              {(uncoveredTracks.length > 0 ? uncoveredTracks : ['当前开放轨道已全部覆盖']).map((item) => <span key={item}>{item}</span>)}
            </div>
          </div>
          <div className="mentor-footer-actions">
            <Link className="primary-button compact" to="/interviews">去面试舱</Link>
            <Link className="ghost-button compact" to="/profile">完善档案</Link>
          </div>
        </section>
      </div>
    </section>
  )
}

function mentorTargetLevelLabel(level: string) {
  const labels: Record<string, string> = {
    junior: '初级',
    intermediate: '中级',
    senior: '高级',
    architect: '架构师',
  }
  return labels[level] ?? '中级'
}

function isMentorEndpointUnavailable(message: string) {
  return message.trim().toLowerCase() === 'not found'
}

function mentorCompatibilitySnapshot(): MentorSnapshot {
  return {
    generated_at: new Date().toISOString(),
    overview: 'AI Mentor 聚合服务尚未启用。你仍可继续完成面试和排查训练，服务升级后会自动形成综合诊断。',
    strengths: [],
    weaknesses: [],
    risks: [{ level: 'info', title: 'Mentor 服务等待升级', message: '当前环境使用兼容模式，不影响面试、排查和个人档案功能。' }],
    actions: [
      { title: '积累面试样本', detail: '完成一轮面试，为后续综合诊断准备训练数据。', action_label: '进入面试舱', action_path: '/interviews' },
      { title: '完善目标画像', detail: '补充目标岗位和项目经历，让后续建议更贴近你的方向。', action_label: '完善档案', action_path: '/profile' },
    ],
    coverage: { coverage_percent: 0, completed_sessions: 0, subject_count: 0, top_subjects: [], uncovered_tracks: [] },
    profile: { target_level: 'intermediate', target_role: '', preferred_domains: [], has_resume_summary: false, has_project_summary: false },
    sample_ready: false,
  }
}
