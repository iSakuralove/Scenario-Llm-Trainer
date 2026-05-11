import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { BookOpenCheck, CheckCircle2, ClipboardList, Gauge, Radar, Sparkles } from 'lucide-react'
import { PolarAngleAxis, PolarGrid, PolarRadiusAxis, Radar as RadarShape, RadarChart, ResponsiveContainer } from 'recharts'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/authStore'
import type { LearningPlan, ReviewCalendar, TotalStats } from '../../types'
import { HeaderBlock, Loading, Metric } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'

export function DashboardPage() {
  const token = useToken()
  const refreshToken = useAuthStore((state) => state.refreshToken)
  const setSession = useAuthStore((state) => state.setSession)
  const [data, setData] = useState<Awaited<ReturnType<typeof api.dashboard>> | null>(null)
  const [isCheckingIn, setCheckingIn] = useState(false)
  const [checkinMessage, setCheckinMessage] = useState('')

  useEffect(() => {
    void api.dashboard(token).then(setData)
  }, [token])

  if (!data) return <Loading title="读取仪表盘" />

  const radar = Object.entries(data.capability_radar ?? {}).map(([domain, score]) => ({ domain, score }))
  const learningPlan = data.learning_plan ?? fallbackLearningPlan(data)
  const reviewCalendar = data.review_calendar ?? fallbackReviewCalendar(data, learningPlan)
  const insightByDomain = new Map((learningPlan.domain_insights ?? []).map((item) => [item.domain, item]))
  const checkinText = reviewCalendar.today_checked ? '今日已打卡' : '完成今日打卡'
  const stats = dashboardStats(data)

  async function checkin() {
    setCheckingIn(true)
    setCheckinMessage('')
    try {
      const res = await api.checkin(token)
      setSession(res.user, token, refreshToken)
      setData((prev) => prev
        ? {
            ...prev,
            user: res.user,
            stats: res.user.profile.total_stats,
            review_calendar: {
              ...prev.review_calendar,
              today_checked: true,
              streak_days: res.checkin.streak_days,
              checkin_dates: res.user.profile.checkin_dates ?? prev.review_calendar?.checkin_dates ?? [],
              next_action: res.checkin.next_action,
            },
          }
        : prev)
      setCheckinMessage(res.checkin.already_checked_in ? '今天已经打过卡' : '已记录今日训练打卡')
    } catch (err) {
      setCheckinMessage(err instanceof Error ? err.message : '打卡失败')
    } finally {
      setCheckingIn(false)
    }
  }

  return (
    <section className="page-stack dashboard-page">
      <HeaderBlock
        icon={<Gauge size={22} />}
        title="学习仪表盘"
        description="汇总排查、面试和能力画像，帮助你快速进入当天的训练节奏。"
      />
      <section className="dashboard-hero panel">
        <div className="dashboard-hero-copy">
          <div className="dashboard-hero-kicker">Learning Workspace</div>
          <h2>今日要做什么</h2>
          <p>{learningPlan.summary}</p>
          <strong>{reviewCalendar.next_action}</strong>
        </div>
        <div className="dashboard-hero-checkin">
          <span>{reviewCalendar.today}</span>
          <button className="primary-button compact" type="button" disabled={reviewCalendar.today_checked || isCheckingIn} onClick={() => void checkin()}>
            <CheckCircle2 size={16} />{isCheckingIn ? '记录中' : checkinText}
          </button>
          {checkinMessage && <small>{checkinMessage}</small>}
        </div>
      </section>
      <div className="metric-row dashboard-hero-metrics">
        <Metric label="排查次数" value={stats.scenarios_solved} />
        <Metric label="面试次数" value={stats.interviews_taken} />
        <Metric label="平均得分" value={stats.average_score || '--'} />
        <Metric label="连续打卡" value={`${reviewCalendar.streak_days} 天`} />
      </div>
      <div className="two-column dashboard-feature-grid">
        <section className="panel learning-brief">
          <div>
            <div className="panel-title"><BookOpenCheck size={18} /> 今日学习闭环</div>
            <p>{learningPlan.summary}</p>
            <strong>{reviewCalendar.next_action}</strong>
          </div>
          <div className="dashboard-loop-meta">
            <span>目标职级为 {data.user.profile.target_level || '中级'}</span>
            <span>建议先围绕 {learningPlan.focus_domains.map(domainLabel).join('、') || '排障基础'} 建立训练样本。</span>
          </div>
        </section>
        <section className="panel dashboard-recommend-panel">
          <div className="panel-title"><Sparkles size={18} /> 今日推荐</div>
          <div className="list-stack">
            {(learningPlan.recommendations ?? []).map((item) => (
              <Link className="scenario-line" key={item.id} to={item.action_path || '/scenarios'}>
                <strong>{item.title}</strong>
                <span>{domainLabel(item.domain)} · {item.difficulty} · 优先级 {item.priority}</span>
                <small>{item.reason}</small>
              </Link>
            ))}
          </div>
        </section>
      </div>
      <div className="two-column dashboard-analysis-grid">
        <section className="panel dashboard-radar-panel">
          <div className="panel-title"><Radar size={18} /> 能力雷达</div>
          <div className="chart-box">
            <ResponsiveContainer width="100%" height="100%">
              <RadarChart data={radar}>
                <PolarGrid />
                <PolarAngleAxis dataKey="domain" />
                <PolarRadiusAxis domain={[0, 100]} tick={false} axisLine={false} />
                <RadarShape dataKey="score" fill="#0f766e" fillOpacity={0.18} stroke="#0f766e" strokeWidth={2} />
              </RadarChart>
            </ResponsiveContainer>
          </div>
        </section>
        <section className="panel">
          <div className="panel-title"><ClipboardList size={18} /> 三天复习计划</div>
          <div className="review-plan-grid">
            {(reviewCalendar.review_plan ?? []).map((item) => (
              <div className="review-plan-item" key={`${item.day_label}-${item.domain}`}>
                <span>{item.day_label} · {item.estimated_minutes} 分钟</span>
                <strong>{item.focus}</strong>
                <ul>
                  {(item.actions ?? []).map((action) => <li key={action}>{action}</li>)}
                </ul>
                {(item.reason || item.source_kind) && <small>{item.source_kind || 'plan'} · {item.reason || '基于当前画像生成'}</small>}
              </div>
            ))}
          </div>
        </section>
      </div>
      <section className="panel">
        <div className="panel-title"><ClipboardList size={18} /> 薄弱点</div>
        <div className="weak-grid">
          {(data.weak_points ?? []).map((point) => (
            <div className="weak-item" key={`${point.domain}-${point.topic}`}>
              <strong>{domainLabel(point.domain)} · {point.topic}</strong>
              <span>{insightByDomain.get(point.domain)?.reason ?? `最近得分 ${point.last_score}，建议进入排查工坊专项训练。`}</span>
            </div>
          ))}
        </div>
      </section>
    </section>
  )
}

function fallbackLearningPlan(data: Awaited<ReturnType<typeof api.dashboard>>): LearningPlan {
  const stats = dashboardStats(data)
  const weakDomains = (data.weak_points ?? []).map((point) => point.domain)
  const focusDomains = weakDomains.length > 0 ? Array.from(new Set(weakDomains)) : data.user.profile.preferred_domains ?? []
  const recommendations = (data.recommendations ?? []).map((question, index) => ({
    id: question.id,
    kind: 'scenario',
    domain: question.domain,
    title: question.title,
    description: question.description,
    difficulty: question.difficulty,
    priority: index + 1,
    reason: `${domainLabel(question.domain)} 方向可用于补齐当前训练闭环。`,
    action_label: '开始训练',
    action_path: '/scenarios',
    question,
  }))

  return {
    generated_at: new Date().toISOString(),
    summary: '当前后端暂未返回完整学习计划，已根据能力雷达和推荐题生成本地复习建议。',
    target_level: data.user.profile.target_level,
    focus_domains: focusDomains,
    domain_insights: Object.entries(data.capability_radar ?? {}).map(([domain, score]) => ({
      domain,
      score,
      level: score >= 80 ? '熟练' : score >= 60 ? '稳定' : '待加强',
      trend: 'stable',
      completed_count: stats.scenarios_solved,
      reason: score >= 70
        ? `${domainLabel(domain)} 当前表现稳定，建议保持周期复盘。`
        : `${domainLabel(domain)} 雷达分偏低，建议优先安排专项排查训练。`,
    })),
    recommendations,
    review_plan: fallbackReviewPlan(focusDomains),
  }
}

function fallbackReviewCalendar(data: Awaited<ReturnType<typeof api.dashboard>>, plan: LearningPlan): ReviewCalendar {
  const today = new Date().toISOString().slice(0, 10)
  const checkinDates = data.user.profile.checkin_dates ?? []
  return {
    generated_at: new Date().toISOString(),
    checkin_dates: checkinDates,
    streak_days: checkinDates.includes(today) ? 1 : 0,
    today_checked: checkinDates.includes(today),
    today,
    review_plan: (plan.review_plan ?? []).length > 0 ? plan.review_plan : fallbackReviewPlan(plan.focus_domains ?? []),
    focus_domains: plan.focus_domains ?? [],
    next_action: plan.recommendations[0]?.title
      ? `优先完成：${plan.recommendations[0].title}`
      : '完成一轮排查题或面试题，生成更准确的复习计划。',
  }
}

function dashboardStats(data: Awaited<ReturnType<typeof api.dashboard>>): TotalStats {
  return data.stats ?? data.user.profile.total_stats ?? {
    scenarios_solved: 0,
    interviews_taken: 0,
    average_score: 0,
    streak_days: 0,
  }
}

function fallbackReviewPlan(focusDomains: string[]) {
  const domainsForPlan = focusDomains.length > 0 ? focusDomains : ['database', 'network', 'os']
  return domainsForPlan.slice(0, 3).map((domain, index) => ({
    day_label: index === 0 ? '今天' : `第 ${index + 1} 天`,
    domain,
    focus: `${domainLabel(domain)} 专项复习`,
    actions: ['复盘最近一次训练记录', '完成一道同域排查题', '整理 3 条标准排查步骤'],
    estimated_minutes: 25 + index * 5,
    target_score: 80,
    question_ids: [],
  }))
}
