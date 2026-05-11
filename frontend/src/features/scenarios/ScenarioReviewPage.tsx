import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, CheckCircle2, ClipboardList, FileSearch, MessageSquareText, ShieldCheck } from 'lucide-react'
import { api } from '../../api/client'
import { HeaderBlock, Loading, Metric, PrintButton } from '../../components/common'
import { useToken } from '../../lib/auth'
import './ScenarioSessionPage.css'

export function ScenarioReviewPage() {
  const token = useToken()
  const { id = '' } = useParams()
  const [review, setReview] = useState<Awaited<ReturnType<typeof api.scenarioReview>> | null>(null)

  useEffect(() => {
    void api.scenarioReview(token, id).then(setReview)
  }, [token, id])

  if (!review) return <Loading title="生成复盘" />

  const scoringReport = review.session.evaluation_result?.scoring_report
  const reportMetrics = scoringReport
    ? [
        { label: 'Overall', value: scoringReport.overall_score },
        { label: 'Root', value: scoringReport.root_cause_similarity },
        { label: 'Evidence', value: scoringReport.evidence_chain_score },
        { label: 'Procedure', value: scoringReport.procedure_coverage_score },
        { label: 'Clue', value: scoringReport.clue_usage_score },
        { label: 'Reasoning', value: scoringReport.reasoning_depth_score },
        { label: 'Efficiency', value: scoringReport.efficiency_score },
      ]
    : []

  return (
    <section className="page-stack printable-page scenario-review-page">
      <HeaderBlock
        icon={<CheckCircle2 size={22} />}
        title="排查复盘"
        description="对话记录、线索使用和标准排查路径集中展示。"
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
      <div className="metric-row">
        <Metric label="综合分" value={review.session.score?.total ?? '--'} />
        <Metric label="答案匹配" value={`${review.session.evaluation_result?.match_degree ?? 0}%`} />
        <Metric label="效率" value={review.session.score?.efficiency ?? '--'} />
        <Metric label="线索利用" value={`${review.session.score?.clue_usage ?? 0}%`} />
      </div>
      <section className="panel report-overview">
        <div>
          <span>最终提交</span>
          <strong>{review.session.user_answer || '未记录最终答案'}</strong>
        </div>
        <div>
          <span>会话状态</span>
          <strong>{review.session.status}</strong>
        </div>
        <div>
          <span>已释放线索</span>
          <strong>{(review.session.revealed_clue_ids ?? []).length} 条</strong>
        </div>
      </section>
      <section className="panel scoring-report-panel">
        <div className="panel-title"><FileSearch size={18} /> 评分报告</div>
        {scoringReport ? (
          <div className="scoring-report">
            <div className="score-metric-grid">
              {reportMetrics.map((metric) => (
                <Metric key={metric.label} label={metric.label} value={formatScore(metric.value)} variant="compact" />
              ))}
            </div>
            <div className="report-explanation">
              <span>评分说明</span>
              <p>{scoringReport.score_explanation || '暂无评分说明'}</p>
            </div>
            <div className="two-column report-detail-columns">
              <div className="report-detail-block">
                <span>扣分项</span>
                {scoringReport.penalties.length ? (
                  <ul className="report-list">
                    {scoringReport.penalties.map((item) => <li key={item}>{item}</li>)}
                  </ul>
                ) : (
                  <p className="empty-inline">暂无扣分项</p>
                )}
              </div>
              <div className="report-detail-block">
                <span>证据事件</span>
                {scoringReport.evidence_events.length ? (
                  <div className="evidence-event-list">
                    {scoringReport.evidence_events.map((event, index) => (
                      <div className="evidence-event" key={`${event.turn_number}-${event.event_type}-${index}`}>
                        <strong>第 {event.turn_number} 轮 · {event.event_type}</strong>
                        <p>{event.text}</p>
                        <small>
                          匹配 {event.best_doc_type || '--'} / {event.best_doc_key || '--'} · {formatScore(event.score)}
                        </small>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="empty-inline">暂无证据事件</p>
                )}
              </div>
            </div>
            <div className="report-detail-block">
              <span>匹配文档</span>
              {scoringReport.matched_documents.length ? (
                <div className="matched-document-list">
                  {scoringReport.matched_documents.map((document, index) => (
                    <div className="matched-document" key={`${document.doc_type}-${document.doc_key}-${index}`}>
                      <div>
                        <strong>{document.doc_type} / {document.doc_key}</strong>
                        <small>{formatScore(document.score)}</small>
                      </div>
                      <p>{document.snippet}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="empty-inline">暂无匹配文档</p>
              )}
            </div>
          </div>
        ) : (
          <p className="empty-inline">当前复盘暂无评分报告。</p>
        )}
      </section>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><MessageSquareText size={18} /> 对话记录</div>
          <div className="review-thread">
            {(review.messages ?? []).map((message) => (
              <div className="review-turn" key={message.id}>
                <strong>Q{message.turn_number}: {message.user_content}</strong>
                <span>{message.assistant_content}</span>
              </div>
            ))}
          </div>
        </section>
        <section className="panel">
          <div className="panel-title"><ShieldCheck size={18} /> 标准答案</div>
          <p className="standard-answer">{review.standard_answer}</p>
          <ol className="step-list">
            {(review.standard_steps ?? []).map((step) => <li key={step}>{step}</li>)}
          </ol>
        </section>
      </div>
      <section className="panel">
        <div className="panel-title"><ClipboardList size={18} /> 关键证据</div>
        <ul className="evidence-list">
          {(review.key_evidence ?? []).map((item) => <li key={item}>{item}</li>)}
        </ul>
      </section>
    </section>
  )
}

function formatScore(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) return '--'
  return Number.isInteger(value) ? value : value.toFixed(1)
}
