import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ClipboardList, FileText, MessageSquareText, Mic, Radar } from 'lucide-react'
import { PolarAngleAxis, PolarGrid, PolarRadiusAxis, Radar as RadarShape, RadarChart, ResponsiveContainer } from 'recharts'
import { api } from '../../api/client'
import { HeaderBlock, Loading, Metric, PrintButton } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import type { AgentTrace } from '../../types'
import './InterviewReportPage.css'

export function InterviewReportPage() {
  const token = useToken()
  const { id = '' } = useParams()
  const [report, setReport] = useState<Awaited<ReturnType<typeof api.interviewReport>> | null>(null)
  const [audioUrls, setAudioUrls] = useState<Record<string, string>>({})

  useEffect(() => {
    void api.interviewReport(token, id).then(setReport)
  }, [token, id])

  useEffect(() => {
    if (!report) return
    let active = true
    const objectUrls: string[] = []
    const voiceSubmissions = normalizeSubmissionRounds(report.session.submissions ?? []).filter((submission) => submission.asset_id)

    void Promise.all(voiceSubmissions.map(async (submission) => {
      if (!submission.asset_id) return null
      try {
        const blob = await api.assetContentBlob(token, submission.asset_id)
        const objectUrl = URL.createObjectURL(blob)
        objectUrls.push(objectUrl)
        return [submission.asset_id, objectUrl] as const
      } catch {
        return [submission.asset_id, ''] as const
      }
    })).then((entries) => {
      if (!active) return
      const next: Record<string, string> = {}
      for (const entry of entries) {
        if (!entry) continue
        next[entry[0]] = entry[1]
      }
      setAudioUrls(next)
    })

    return () => {
      active = false
      objectUrls.forEach((value) => URL.revokeObjectURL(value))
      setAudioUrls({})
    }
  }, [report, token])

  if (!report) return <Loading title="生成面试报告" />
  const evaluations = normalizeEvaluationRounds(report.session.evaluations ?? [])
  const submissions = normalizeSubmissionRounds(report.session.submissions ?? [])
  const invalidSubmissionRounds = new Set(submissions.filter((item) => item.quality_flag === 'irrelevant').map((item) => item.displayRound))
  const reportAgentTrace = getReportAgentTrace(report.session.evaluations ?? [])
  const radarData = report.radar_data.map((item) => ({
    ...item,
    dimension: dimensionLabel(item.dimension),
  }))
  const isInvalidatedReport = report.final_report === '继续沉淀' && radarData.length === 0

  return (
    <section className="page-stack printable-page interview-report-page">
      <HeaderBlock
        icon={<FileText size={22} />}
        title="面试报告"
        description="五维评分、轮次对比、作答记录和综合评语集中展示。"
        action={<PrintButton />}
      />
      <div className="metric-row">
        <Metric label="最终得分" value={report.final_score ?? '--'} />
        <Metric label="轮次数" value={evaluations.length} />
        <Metric label="状态" value={sessionStatusLabel(report.session.status)} />
        <Metric label="面试域" value={domainLabel(report.question.domain)} />
      </div>
      <section className="panel report-overview">
        <div>
          <span>面试题目</span>
          <strong>{report.question.title}</strong>
          <p>{report.question.description}</p>
        </div>
      </section>
      {isInvalidatedReport && (
        <section className="panel report-invalidated-panel">
          <div className="panel-title"><Radar size={18} /> 继续沉淀</div>
          <p>本场面试没有生成详细数据评分，数据分析图不展示。请先围绕题目补齐定位路径、关键命令、修复方案和回滚考虑，再重新开始面试。</p>
        </section>
      )}
      <div className="two-column">
        {!isInvalidatedReport && (
          <section className="panel">
            <div className="panel-title"><Radar size={18} /> 五维评分</div>
            <div className="chart-box">
              <ResponsiveContainer width="100%" height="100%">
                <RadarChart data={radarData}>
                  <PolarGrid />
                  <PolarAngleAxis dataKey="dimension" />
                  <PolarRadiusAxis domain={[0, 100]} tick={false} axisLine={false} />
                  <RadarShape dataKey="score" fill="#0f766e" fillOpacity={0.2} stroke="#0f766e" strokeWidth={2} />
                </RadarChart>
              </ResponsiveContainer>
            </div>
          </section>
        )}
        <section className="panel">
          <div className="panel-title"><ClipboardList size={18} /> 轮次对比</div>
          <div className="review-thread">
            {evaluations.map((item) => (
              <div className="review-turn" key={item.displayRound}>
                <strong>{invalidSubmissionRounds.has(item.displayRound) ? `第 ${item.displayRound} 轮：无效作答` : `第 ${item.displayRound} 轮：${item.total_score} 分`}</strong>
                <span>{invalidSubmissionRounds.has(item.displayRound) ? '未进行详细评分，请围绕面试题重新组织有效回答。' : (item.deficiencies ?? []).join(' ')}</span>
              </div>
            ))}
          </div>
          {!isInvalidatedReport && <AgentSummary trace={reportAgentTrace} />}
        </section>
      </div>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><FileText size={18} /> 综合评语</div>
          <p className="report-text">{report.final_report || '已生成综合评价。'}</p>
        </section>
        <section className="panel">
          <div className="panel-title"><MessageSquareText size={18} /> 作答记录</div>
          <div className="submission-list">
            {submissions.map((submission) => (
              <div className="review-turn" key={submission.displayRound}>
                <strong>第 {submission.displayRound} 轮{submissionSourceLabel(submission.source, submission.type)}回答</strong>
                <span>{submission.content}</span>
                {(submission.asset_id || submission.voice_quality) && (
                  <div className="submission-evidence">
                    <small>
                      <Mic size={13} />
                      {submission.asset?.filename || submission.asset_id ? `语音资源：${submission.asset?.filename || submission.asset_id}` : '语音资源已留档'}
                      {submission.duration_seconds ? ` · ${submission.duration_seconds}s` : ''}
                    </small>
                    {submission.asset && (
                      <small>资产链路：{submission.asset.id} · {formatBytes(submission.asset.size)} · SHA256 {submission.asset.checksum || '待补'}</small>
                    )}
                    {submission.asset_id && audioUrls[submission.asset_id] ? (
                      <audio controls preload="metadata" src={audioUrls[submission.asset_id]} aria-label={`第 ${submission.displayRound} 轮语音证据回放`} />
                    ) : submission.asset_id ? <small>音频文件已留档，但当前无法直接回放，请检查资源是否仍可读取。</small> : null}
                    {submission.voice_quality && (
                      <small>
                        质检：相关性 {submission.voice_quality.topic_relevance_score} · 语言 {submission.voice_quality.detected_language || 'unknown'} · 置信度 {Math.round((submission.voice_quality.stt_confidence || 0) * 100)}%
                      </small>
                    )}
                    {submission.voice_quality?.keyword_hits?.length ? <small>命中：{submission.voice_quality.keyword_hits.join('、')}</small> : null}
                    {submission.source === 'voice_edited' && submission.transcript ? <small>原始转写：{submission.transcript}</small> : null}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      </div>
    </section>
  )
}

function submissionSourceLabel(source: string | undefined, type: string) {
  if (source === 'voice_edited') return '语音编辑后文本'
  if (source === 'voice_transcript' || type === 'voice') return '语音'
  return '文本'
}

function normalizeEvaluationRounds(items: Awaited<ReturnType<typeof api.interviewReport>>['session']['evaluations']) {
  return items.map((item, index) => ({
    ...item,
    displayRound: index + 1,
  }))
}

function normalizeSubmissionRounds(items: Awaited<ReturnType<typeof api.interviewReport>>['session']['submissions']) {
  return items.map((item, index) => ({
    ...item,
    displayRound: index + 1,
  }))
}

function formatBytes(value: number | undefined) {
  if (!value || value <= 0) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / (1024 * 1024)).toFixed(1)} MB`
}

function getReportAgentTrace(items: Awaited<ReturnType<typeof api.interviewReport>>['session']['evaluations']) {
  return [...items].reverse().find((item) => item.agent_trace)?.agent_trace ?? null
}

function AgentSummary({ trace }: { trace: AgentTrace | null }) {
  if (!trace?.steps?.length) return null
  return (
    <div className="agent-stage-list completed report-agent-summary" data-testid="report-agent-summary">
      <strong className="agent-trace-summary">Agent 已执行 {trace.steps.length} 个安全步骤</strong>
      {trace.steps.map((step, index) => (
        <span key={`${step.name}-${index}`}>
          安全步骤 {index + 1}
          {step.summary ? ` · ${sanitizeAgentText(step.summary)}` : ''}
        </span>
      ))}
    </div>
  )
}

function sanitizeAgentText(value: string) {
  return value
    .replace(/\b(?:reference_answer|standard_procedure|root_cause|metadata|tool|tool_args|arguments|agent_trace)\b/gi, '安全信息')
    .replace(/\s+/g, ' ')
    .trim()
}

function dimensionLabel(value: string) {
  const labels: Record<string, string> = {
    technical_accuracy: '技术准确性',
    accuracy: '技术准确性',
    logical_completeness: '逻辑完整性',
    completeness: '逻辑完整性',
    solution_feasibility: '方案可落地性',
    feasibility: '方案可落地性',
    depth_breadth: '深度与广度',
    depth_breadthexpression_structure: '深度与广度',
    expression_structure: '表达结构',
    expression: '表达结构',
  }
  return labels[value] ?? value
}

function sessionStatusLabel(value: string) {
  const labels: Record<string, string> = {
    active: '进行中',
    follow_up_1_presented: '追问中',
    follow_up_2_presented: '追问中',
    final_evaluated: '已完成',
    completed: '已完成',
    abandoned: '已放弃',
    expired: '已失效',
  }
  return labels[value] ?? value
}
