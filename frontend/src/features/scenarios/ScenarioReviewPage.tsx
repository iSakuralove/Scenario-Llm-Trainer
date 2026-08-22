import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  ClipboardList,
  FileSearch,
  GitBranch,
  MessageSquareText,
  ShieldCheck,
  TestTube2,
  Wrench,
} from 'lucide-react'
import { api } from '../../api/client'
import { EmptyState, HeaderBlock, Loading, Metric, PrintButton } from '../../components/common'
import { useToken } from '../../lib/auth'
import { resolveQuickActionUserLabel } from './agentrun'
import './ScenarioSessionPage.css'

export function ScenarioReviewPage() {
  const token = useToken()
  const { id = '' } = useParams()
  const [review, setReview] = useState<Awaited<ReturnType<typeof api.scenarioReview>> | null>(null)
  const [loadError, setLoadError] = useState('')

  useEffect(() => {
    let cancelled = false
    setReview(null)
    setLoadError('')
    void api.scenarioReview(token, id)
      .then((data) => {
        if (!cancelled) setReview(data)
      })
      .catch((error) => {
        if (!cancelled) setLoadError(error instanceof Error ? error.message : '复盘读取失败')
      })
    return () => {
      cancelled = true
    }
  }, [token, id])

  if (loadError) {
    return (
      <EmptyState
        title="复盘读取失败"
        description={loadError}
        action={<Link className="primary-button" to="/scenarios">返回排查工坊</Link>}
      />
    )
  }
  if (!review) return <Loading title="生成复盘" />

  const scoringReport = review.session.evaluation_result?.scoring_report
  const reportMetrics = scoringReport
    ? [
        { label: '综合', value: scoringReport.overall_score },
        { label: '直接原因', value: scoringReport.root_cause_similarity },
        { label: '证据链', value: scoringReport.evidence_chain_score },
        { label: '排查步骤', value: scoringReport.procedure_coverage_score },
        { label: '线索使用', value: scoringReport.clue_usage_score },
        { label: '推理完整度', value: scoringReport.reasoning_depth_score },
        { label: '排查效率', value: scoringReport.efficiency_score },
      ]
    : []

  return (
    <section className="page-stack printable-page scenario-review-page">
      <HeaderBlock
        icon={<CheckCircle2 size={22} />}
        title="排查复盘"
        description="先还原事故因果链，再对照你的结论、证据和修复方案。"
        action={(
          <div className="scenario-review-actions">
            <Link className="ghost-button compact no-print" to="/scenarios">
              <ArrowLeft size={16} />
              返回排查工坊
            </Link>
            <PrintButton />
          </div>
        )}
      />
      <section className="panel scenario-debrief-panel">
        <div className="panel-title"><GitBranch size={18} /> 故障因果复盘</div>
        {Object.values(review.debrief).some((items) => items.length > 0) ? (
          <div className="scenario-debrief-grid">
            <DebriefCard title="直接触发" items={review.debrief.direct_trigger} icon={<AlertTriangle size={16} />} emphasis />
            <DebriefCard title="潜在问题" items={review.debrief.latent_issues} icon={<Activity size={16} />} />
            <DebriefCard title="可见现象" items={review.debrief.phenomenon} icon={<FileSearch size={16} />} />
            <DebriefCard title="衍生风险" items={review.debrief.derived_risks} icon={<ShieldCheck size={16} />} />
            <DebriefCard title="完整因果链" items={review.debrief.causal_chain} icon={<GitBranch size={16} />} ordered />
            <DebriefCard title="修复方案" items={review.debrief.solutions} icon={<Wrench size={16} />} />
            <DebriefCard title="验证与观察" items={review.debrief.verification} icon={<TestTube2 size={16} />} ordered />
          </div>
        ) : (
          <p className="empty-inline">当前复盘没有形成可展示的结构化结论。</p>
        )}
      </section>
      <section className="panel report-overview user-conclusion-overview">
        <div>
          <span>你提交的排查结论</span>
          <strong>{review.session.user_answer || '未记录排查结论'}</strong>
        </div>
      </section>
      <div className="metric-row scenario-review-score-summary">
        <Metric label="综合分" value={review.session.score?.total ?? '--'} />
        <Metric
          label="结论匹配"
          value={review.session.evaluation_result ? `${review.session.evaluation_result.match_degree}%` : '--'}
        />
        <Metric label="排查效率" value={review.session.score?.efficiency ?? '--'} />
        <Metric label="线索使用" value={review.session.score ? `${review.session.score.clue_usage}%` : '--'} />
      </div>
      <section className="panel scoring-report-panel">
        <div className="panel-title"><FileSearch size={18} /> 评分参考</div>
        {scoringReport ? (
          <div className="scoring-report">
            <div className="score-metric-grid">
              {reportMetrics.map((metric) => (
                <Metric key={metric.label} label={metric.label} value={formatScore(metric.value)} variant="compact" />
              ))}
            </div>
            <div className="report-explanation">
              <span>为什么得到这个评分</span>
              <p>{scoringReport.score_explanation || '暂无评分说明'}</p>
            </div>
            <div className="two-column report-detail-columns">
              <div className="report-detail-block">
                <span>仍需补足</span>
                {scoringReport.penalties.length ? (
                  <ul className="report-list">
                    {scoringReport.penalties.map((item) => <li key={item}>{item}</li>)}
                  </ul>
                ) : (
                  <p className="empty-inline">没有额外扣分项。</p>
                )}
              </div>
              <div className="report-detail-block">
                <span>本次排查中采用的证据</span>
                {scoringReport.evidence_events.length ? (
                  <div className="evidence-event-list">
                    {scoringReport.evidence_events.map((event, index) => (
                      <div className="evidence-event" key={`${event.turn_number}-${index}`}>
                        <strong>第 {event.turn_number} 轮</strong>
                        <p>{event.text}</p>
                        <small>证据关联度 {formatScore(event.score)}</small>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="empty-inline">暂无证据事件</p>
                )}
              </div>
            </div>
            <div className="report-detail-block">
              <span>评分参考片段</span>
              {scoringReport.matched_documents.length ? (
                <div className="matched-document-list">
                  {scoringReport.matched_documents.map((document, index) => (
                    <div className="matched-document" key={`${document.snippet}-${index}`}>
                      <div>
                        <strong>参考片段 {index + 1}</strong>
                        <small>{formatScore(document.score)}</small>
                      </div>
                      <p>{document.snippet}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="empty-inline">暂无可展示的评分参考片段。</p>
              )}
            </div>
          </div>
        ) : (
          <p className="empty-inline">当前复盘没有额外评分说明。</p>
        )}
      </section>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><MessageSquareText size={18} /> 对话记录</div>
          <div className="review-thread">
            {review.messages.length > 0 ? review.messages.map((message) => (
              <div className="review-turn" key={message.id}>
                <strong>
                  第 {message.turn_number} 轮：{' '}
                  {resolveQuickActionUserLabel(message.user_content, { events: message.response_meta.run_events ?? [] })}
                </strong>
                <span>{message.assistant_content}</span>
              </div>
            )) : <p className="empty-inline">本次会话没有可展示的对话记录。</p>}
          </div>
        </section>
        <section className="panel">
          <div className="panel-title"><ShieldCheck size={18} /> 标准答案</div>
          <p className="standard-answer">{review.standard_answer || '当前题目没有可展示的标准答案。'}</p>
          {review.standard_steps.length > 0 ? (
            <ol className="step-list">
              {review.standard_steps.map((step) => <li key={step}>{step}</li>)}
            </ol>
          ) : <p className="empty-inline">当前题目没有补充标准步骤。</p>}
        </section>
      </div>
      <section className="panel">
        <div className="panel-title"><ClipboardList size={18} /> 关键证据</div>
        {review.key_evidence.length > 0 ? (
          <ul className="evidence-list">
            {review.key_evidence.map((item) => <li key={item}>{item}</li>)}
          </ul>
        ) : <p className="empty-inline">当前复盘没有单独列出的关键证据。</p>}
      </section>
    </section>
  )
}

function DebriefCard({
  title,
  items,
  icon,
  ordered = false,
  emphasis = false,
}: {
  title: string
  items: string[]
  icon: ReactNode
  ordered?: boolean
  emphasis?: boolean
}) {
  if (items.length === 0) return null
  return (
    <article className={`scenario-debrief-card ${emphasis ? 'is-trigger' : ''}`}>
      <div className="scenario-debrief-card-title">
        {icon}
        <strong>{title}</strong>
      </div>
      {ordered ? (
        <ol>{items.map((item, index) => <li key={`${index}-${item}`}>{item}</li>)}</ol>
      ) : (
        <ul>{items.map((item, index) => <li key={`${index}-${item}`}>{item}</li>)}</ul>
      )}
    </article>
  )
}

function formatScore(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) return '--'
  return Number.isInteger(value) ? value : value.toFixed(1)
}
