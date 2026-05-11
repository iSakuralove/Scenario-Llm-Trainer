import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { BrainCircuit, GitFork, Play, Plus, Square, WandSparkles, X } from 'lucide-react'
import { api } from '../../api/client'
import type { ScenarioQuestion, ScenarioSession } from '../../types'
import { HeaderBlock, Segmented, Select } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { aiJobModelLabel, aiJobStageLabel, aiJobStageText, aiProviderLabel } from '../../lib/ai'
import { redactSensitiveText } from '../../lib/redaction'
import { useScenarioGenerationStore } from '../../stores/scenarioGenerationStore'
import './ScenariosPage.css'

export function ScenariosPage() {
  const token = useToken()
  const navigate = useNavigate()
  const hasHydratedFiltersRef = useRef(false)
  const [completedScenarioSummaries, setCompletedScenarioSummaries] = useState<Map<string, CompletedScenarioSummary>>(
    () => new Map(),
  )
  const items = useScenarioGenerationStore((state) => state.items)
  const total = useScenarioGenerationStore((state) => state.total)
  const filters = useScenarioGenerationStore((state) => state.filters)
  const lastForked = useScenarioGenerationStore((state) => state.lastForked)
  const forkingId = useScenarioGenerationStore((state) => state.forkingId)
  const isGenerationDialogOpen = useScenarioGenerationStore((state) => state.isGenerationDialogOpen)
  const generationDraft = useScenarioGenerationStore((state) => state.draft)
  const isGenerating = useScenarioGenerationStore((state) => state.isGenerating)
  const generationStartedAt = useScenarioGenerationStore((state) => state.generationStartedAt)
  const generationElapsed = useScenarioGenerationStore((state) => state.generationElapsed)
  const generationJob = useScenarioGenerationStore((state) => state.generationJob)
  const lastGenerated = useScenarioGenerationStore((state) => state.lastGenerated)
  const lastGenerationFailure = useScenarioGenerationStore((state) => state.lastGenerationFailure)
  const lastGenerationCanceled = useScenarioGenerationStore((state) => state.lastGenerationCanceled)
  const error = useScenarioGenerationStore((state) => state.error)
  const generationFormError = useScenarioGenerationStore((state) => state.formError)
  const attachPage = useScenarioGenerationStore((state) => state.attachPage)
  const detachPage = useScenarioGenerationStore((state) => state.detachPage)
  const hydrateList = useScenarioGenerationStore((state) => state.hydrateList)
  const refreshList = useScenarioGenerationStore((state) => state.refreshList)
  const setFilters = useScenarioGenerationStore((state) => state.setFilters)
  const resetFilters = useScenarioGenerationStore((state) => state.resetFilters)
  const openGenerationDialog = useScenarioGenerationStore((state) => state.openGenerationDialog)
  const closeGenerationDialog = useScenarioGenerationStore((state) => state.closeGenerationDialog)
  const forkScenario = useScenarioGenerationStore((state) => state.forkScenario)
  const setStoreError = useScenarioGenerationStore((state) => state.setError)
  const updateDraft = useScenarioGenerationStore((state) => state.updateDraft)
  const startGeneration = useScenarioGenerationStore((state) => state.startGeneration)
  const resumeActiveJob = useScenarioGenerationStore((state) => state.resumeActiveJob)
  const cancelGeneration = useScenarioGenerationStore((state) => state.cancelGeneration)

  const pageSize = 9
  const selectedDomain = filters.selectedDomain
  const selectedDifficulty = filters.selectedDifficulty
  const tagFilter = filters.tagFilter
  const page = filters.page
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const visibleTags = uniqueVisibleTags(items)
  const workshopMetrics = scenarioWorkshopMetrics(total, items, isGenerating, generationJob?.progress ?? 0)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void hydrateList(token).catch(() => {
        setStoreError('读取题库失败')
      })
    }, 0)
    return () => window.clearTimeout(timer)
  }, [hydrateList, setStoreError, token])

  useEffect(() => {
    let active = true
    const timer = window.setTimeout(() => {
      void api.history(token)
        .then((history) => {
          if (!active) return
          setCompletedScenarioSummaries(buildCompletedScenarioSummaries(history.scenarios ?? []))
        })
        .catch(() => {
          if (active) setCompletedScenarioSummaries(new Map())
        })
    }, 0)
    return () => {
      active = false
      window.clearTimeout(timer)
    }
  }, [token])

  useEffect(() => {
    if (!hasHydratedFiltersRef.current) {
      hasHydratedFiltersRef.current = true
      return
    }
    void refreshList(token).catch(() => {
      setStoreError('读取题库失败')
    })
  }, [page, refreshList, selectedDomain, selectedDifficulty, setStoreError, tagFilter, token])

  useEffect(() => {
    const onCompleted = ({ question }: { question: ScenarioQuestion }) => {
      setFilters({
        selectedDomain: question.domain,
        selectedDifficulty: question.difficulty,
        tagFilter: '',
        page: 1,
      })
      notifyRouterTelemetryUpdated()
    }

    attachPage({ onCompleted })
    const timer = window.setTimeout(() => {
      void resumeActiveJob(token).catch(() => {})
    }, 0)

    return () => {
      window.clearTimeout(timer)
      detachPage()
    }
  }, [attachPage, detachPage, resumeActiveJob, setFilters, token])

  async function startSession(question: ScenarioQuestion) {
    const res = await api.createScenarioSession(token, question.id)
    navigate(`/scenarios/session/${res.session_id}`, {
      state: { question: res.question_snapshot, sessionId: res.session_id },
    })
  }

  async function generateFromDraft() {
    try {
      const started = await startGeneration(token, selectedDomain, selectedDifficulty)
      if (!started) return
    } catch {
      useScenarioGenerationStore.setState({ isGenerationDialogOpen: true })
    }
  }

  return (
    <section className="page-stack scenarios-workshop-page">
      <div className="scenario-workshop-rail" aria-hidden="true">
        <span>SCENARIO</span>
        <span>WORKSHOP</span>
        <span>2026</span>
      </div>
      <HeaderBlock
        icon={<BrainCircuit size={22} />}
        title="排查工坊"
        description="选择真实故障情景，进入渐进式排查会话，沉淀可复盘的证据链与标准答案。"
        action={(
          <div className="generation-action-cluster">
            <span className="workshop-kicker">Agent Guided Lab</span>
            <button
              className="primary-button compact"
              onClick={() => openGenerationDialog({
                domain: selectedDomain || 'database',
                difficulty: selectedDifficulty || 'L2',
              })}
              disabled={isGenerating}
              aria-busy={isGenerating}
            >
              <Plus size={16} />{isGenerating ? '生成中' : '生成题目'}
            </button>
          </div>
        )}
      />
      {(isGenerating || lastGenerationFailure || lastGenerated || lastGenerationCanceled) && (
        <section
          className={`generation-status ${isGenerating ? 'is-active' : lastGenerationFailure ? 'is-canceled' : lastGenerationCanceled ? 'is-canceled' : 'is-complete'}`}
          aria-live="polite"
        >
          <div className="generation-signal" aria-hidden="true">
            <span />
          </div>
          <div className="generation-copy">
            <strong>
              {isGenerating
                ? 'AI 正在生成情景题'
                : lastGenerationFailure
                  ? '题目生成失败'
                  : lastGenerationCanceled
                    ? '已停止生成'
                    : '题目已生成'}
            </strong>
            <span>
              {isGenerating
                ? `任务 ${generationJob?.id.slice(0, 8) || '创建中'}，${aiJobStageLabel(generationJob)}${aiJobModelLabel(generationJob)}，已等待 ${generationElapsed} 秒。完成后会自动插入列表第一位。`
                : lastGenerationFailure
                  ? `任务 ${lastGenerationFailure.jobId ? lastGenerationFailure.jobId.slice(0, 8) : '未知'} 失败：${lastGenerationFailure.message}。${formatGenerationFailureMeta(lastGenerationFailure)}`
                  : lastGenerationCanceled
                  ? `任务 ${generationJob?.id.slice(0, 8) || '未知'} 已停止，本次不会写入题库，也不会计入 Router 调用统计。`
                  : `已插入：${lastGenerated?.title}。来源：${aiProviderLabel(lastGenerated?.provider, lastGenerated?.fallbackUsed)}。`}
            </span>
          </div>
          {isGenerating && generationStartedAt && (
            <button className="ghost-button compact" type="button" onClick={() => void cancelGeneration(token)}>
              <Square size={14} />停止生成
            </button>
          )}
          <div className="generation-progress">
            <span className="active">构造约束</span>
            <span className={(generationJob?.progress ?? 0) >= 30 || !isGenerating ? 'active' : ''}>调用模型</span>
            <span className={(generationJob?.progress ?? 0) >= 75 || !isGenerating ? 'active' : ''}>校验结构</span>
            <span className={(generationJob?.progress ?? 0) >= 100 || !isGenerating ? 'active' : ''}>写入题库</span>
          </div>
        </section>
      )}
      {error && <div className="inline-error">{error}</div>}
      {lastForked && (
        <div className="inline-success">
          <span>已创建“{lastForked.title}”草稿。前往案例工坊“我的草稿”编辑并提交初审；提交后讲师才能在初审队列看到。</span>
          <button className="ghost-button compact" type="button" onClick={() => navigate('/community?status=draft')}>
            去编辑草稿
          </button>
        </div>
      )}
      <section className="scenario-filters">
        <div className="scenario-filter-primary">
          <span className="scenario-filter-eyebrow">大方向</span>
          <Segmented value={selectedDomain} onChange={(value) => setFilters({ selectedDomain: value, page: 1 })} />
        </div>
        <div className="scenario-filter-secondary">
          <div className="filter-controls">
            <label>
              难度
              <Select
                value={selectedDifficulty}
                onChange={(value) => setFilters({ selectedDifficulty: value, page: 1 })}
                options={[
                  { value: '', label: '全部难度' },
                  { value: 'L1', label: 'L1' },
                  { value: 'L2', label: 'L2' },
                  { value: 'L3', label: 'L3' },
                  { value: 'L4', label: 'L4' },
                  { value: 'L5', label: 'L5' },
                ]}
              />
            </label>
            <label>
              标签
              <div className="tag-filter-composer">
                <input value={tagFilter} onChange={(event) => setFilters({ tagFilter: event.target.value, page: 1 })} placeholder="输入或点选标签" />
              </div>
            </label>
            <button className="ghost-button compact filter-reset-button" type="button" onClick={resetFilters}>重置筛选</button>
          </div>
          <div className="list-summary">
            共 {total} 道题，当前第 {page}/{pageCount} 页
          </div>
        </div>
        {visibleTags.length > 0 && (
          <div className="scenario-tag-filter-row">
            <span className="scenario-filter-eyebrow">常用标签</span>
            <div className="tag-filter-chips" aria-label="常用筛选项">
              {visibleTags.map((tag) => (
                <button
                  key={tag.value}
                  className={tagFilter.trim() === tag.value ? 'active' : ''}
                  type="button"
                  onClick={() => setFilters({ tagFilter: tagFilter.trim() === tag.value ? '' : tag.value, page: 1 })}
                >
                  {tag.label}
                </button>
              ))}
            </div>
          </div>
        )}
      </section>
      <div className="metric-row scenario-workshop-metrics">
        {workshopMetrics.map((metric) => (
          <div className={`metric ${metric.tone}`} key={metric.label}>
            <span>{metric.label}</span>
            <strong>{metric.value}</strong>
            <small>{metric.detail}</small>
          </div>
        ))}
      </div>
      <div className="scenario-workshop-layout">
        <section className="scenario-workshop-board">
          <div className="scenario-workshop-board-head">
            <div>
              <span>Scenario Queue</span>
              <h2>故障案例流</h2>
            </div>
          </div>
          <div className="scenario-grid">
            {items.map((item, index) => {
              const completedSummary = completedScenarioSummaries.get(item.id)
              const completedScoreText = formatCompletedScenarioScore(completedSummary?.score)
              const completeBadgeLabel = completedScoreText
                ? `已排查并提交答案，得分 ${completedScoreText}`
                : '已排查并提交答案'
              const isCompleted = Boolean(completedSummary)
              return (
                <article
                  className={`scenario-card ${isCompleted ? 'is-completed' : ''}`}
                  data-testid={`scenario-card-${item.id}`}
                  key={item.id}
                >
                  {isCompleted && (
                    <span className="scenario-complete-badge" aria-label={completeBadgeLabel} title={completeBadgeLabel}>
                      <span className="scenario-complete-symbol" aria-hidden="true">√</span>
                      已完成
                      {completedScoreText && <span className="scenario-complete-score">{completedScoreText}</span>}
                    </span>
                  )}
                  <div className="scenario-card-topline">
                    <span>{String(index + 1).padStart(2, '0')}</span>
                    <div className="scenario-meta">
                      <span>{domainLabel(item.domain)}</span>
                      <span>{item.difficulty}</span>
                      <span>{item.scenario_type}</span>
                    </div>
                  </div>
                  <h3>{redactSensitiveText(item.title)}</h3>
                  <p>{redactSensitiveText(item.description)}</p>
                  <div className="tag-row">
                    <span>{scenarioSourceLabel(item)}</span>
                    {(item.tags ?? []).slice(0, 3).map((tag) => <span key={tag}>{redactSensitiveText(tag)}</span>)}
                    {item.creator_role && <span>{item.creator_role}</span>}
                  </div>
                  <div className="card-actions">
                    <span className="safe-badge">脱敏选题卡片</span>
                    <button
                      className="ghost-button compact"
                      type="button"
                      onClick={() => void forkScenario(token, item)}
                      disabled={forkingId === item.id}
                    >
                      <GitFork size={16} />{forkingId === item.id ? '派生中' : '派生题目'}
                    </button>
                    {item.status === 'active' ? (
                      <button className="primary-button compact" onClick={() => void startSession(item)}><Play size={16} />{isCompleted ? '再次排查' : '开始排查'}</button>
                    ) : (
                      <button className="ghost-button compact" type="button" disabled>待审核</button>
                    )}
                  </div>
                </article>
              )
            })}
          </div>
          <div className="pagination-bar">
            <button className="ghost-button compact" type="button" onClick={() => setFilters({ page: Math.max(1, page - 1) })} disabled={page <= 1}>
              上一页
            </button>
            <span>第 {page} / {pageCount} 页</span>
            <button className="ghost-button compact" type="button" onClick={() => setFilters({ page: Math.min(pageCount, page + 1) })} disabled={page >= pageCount}>
              下一页
            </button>
          </div>
        </section>
      </div>

      {isGenerationDialogOpen && (
        <div className="scenario-generation-overlay" role="dialog" aria-modal="true" aria-label="约束生成情景题">
          <div className="scenario-generation-modal">
            <div className="scenario-generation-header">
              <div>
                <strong>约束生成情景题</strong>
                <span>基础字段必填，高级约束会作为 AI 参考补充。</span>
              </div>
              <button className="icon-button compact-icon" type="button" onClick={closeGenerationDialog} aria-label="关闭">
                <X size={16} />
              </button>
            </div>
            <div className="scenario-generation-form">
              <div className="scenario-generation-grid">
                <label>
                  领域
                  <Select
                    value={generationDraft.domain}
                    onChange={(value) => updateDraft({ domain: value })}
                    options={[
                      { value: 'database', label: '数据库' },
                      { value: 'security', label: '安全' },
                      { value: 'dns', label: 'DNS' },
                      { value: 'network', label: '网络' },
                    ]}
                  />
                </label>
                <label>
                  生成难度
                  <Select
                    value={generationDraft.difficulty}
                    onChange={(value) => updateDraft({ difficulty: value })}
                    options={[
                      { value: 'L1', label: 'L1' },
                      { value: 'L2', label: 'L2' },
                      { value: 'L3', label: 'L3' },
                      { value: 'L4', label: 'L4' },
                      { value: 'L5', label: 'L5' },
                    ]}
                  />
                </label>
                <label>
                  题型
                  <Select
                    value={generationDraft.scenarioType}
                    onChange={(value) => updateDraft({ scenarioType: value })}
                    options={[
                      { value: 'troubleshooting', label: 'troubleshooting' },
                      { value: 'design', label: 'design' },
                      { value: 'performance', label: 'performance' },
                    ]}
                  />
                </label>
              </div>
              <button
                className="ghost-button compact"
                type="button"
                onClick={() => updateDraft((current) => ({ ...current, advancedOpen: !current.advancedOpen }))}
              >
                <WandSparkles size={16} />
                {generationDraft.advancedOpen ? '收起高级约束' : '显示高级约束'}
              </button>
              {generationDraft.advancedOpen && (
                <div className="scenario-generation-advanced">
                  <label>
                    标题约束
                    <input
                      aria-label="标题约束"
                      value={generationDraft.title}
                      onChange={(event) => updateDraft({ title: event.target.value })}
                      placeholder="硬约束，若填写则标题必须遵守"
                    />
                  </label>
                  <label>
                    描述约束
                    <textarea
                      aria-label="描述约束"
                      value={generationDraft.description}
                      onChange={(event) => updateDraft({ description: event.target.value })}
                      placeholder="可选约束，AI 会参考补全"
                    />
                  </label>
                  <label>
                    细分主题
                    <textarea
                      aria-label="细分主题"
                      value={generationDraft.topicScope}
                      onChange={(event) => updateDraft({ topicScope: event.target.value })}
                      placeholder="每行一个，例如：主从复制"
                    />
                  </label>
                  <label>
                    根因提示
                    <textarea
                      aria-label="根因提示"
                      value={generationDraft.rootCauseHint}
                      onChange={(event) => updateDraft({ rootCauseHint: event.target.value })}
                      placeholder="可选根因方向"
                    />
                  </label>
                  <label>
                    证据提示
                    <textarea
                      aria-label="证据提示"
                      value={generationDraft.evidenceHints}
                      onChange={(event) => updateDraft({ evidenceHints: event.target.value })}
                      placeholder="每行一个证据提示"
                    />
                  </label>
                  <label>
                    线索提示
                    <textarea
                      aria-label="线索提示"
                      value={generationDraft.clueHints}
                      onChange={(event) => updateDraft({ clueHints: event.target.value })}
                      placeholder="每行一个线索提示"
                    />
                  </label>
                </div>
              )}
              {generationFormError && <div className="inline-error">{generationFormError}</div>}
            </div>
            <div className="scenario-generation-footer">
              <button className="ghost-button compact" type="button" onClick={closeGenerationDialog}>关闭</button>
              <button className="primary-button compact" type="button" onClick={() => void generateFromDraft()} disabled={isGenerating}>
                <Plus size={16} />开始生成
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}

function scenarioWorkshopMetrics(total: number, items: ScenarioQuestion[], isGenerating: boolean, progress: number) {
  const activeCount = items.filter((item) => item.status === 'active').length
  const generatedCount = items.filter((item) => item.source === 'llm_generated').length
  const reviewCount = items.filter((item) => item.status !== 'active').length
  const evidenceRate = total > 0 ? Math.min(98, Math.max(42, Math.round((activeCount / Math.max(items.length, 1)) * 100))) : 0

  return [
    { label: '可训练情景', value: total, detail: `${activeCount} 道可直接开始`, tone: 'tone-green' },
    { label: '正在生成', value: isGenerating ? '01' : '00', detail: isGenerating ? `进度 ${progress || 1}%` : `${generatedCount} 道 AI 题`, tone: 'tone-yellow' },
    { label: '证据链覆盖', value: `${evidenceRate}%`, detail: '日志、指标、配置、Trace', tone: 'tone-blue' },
    { label: '待复盘', value: reviewCount, detail: reviewCount > 0 ? '建议优先处理' : '当前队列稳定', tone: 'tone-orange' },
  ]
}

function uniqueVisibleTags(items: ScenarioQuestion[]) {
  const tags: Array<{ value: string; label: string }> = []
  const seen = new Set<string>()
  for (const item of items) {
    for (const value of item.tags ?? []) {
      if (!value || seen.has(value)) continue
      seen.add(value)
      tags.push({ value, label: redactSensitiveText(value) })
      if (tags.length >= 10) return tags
    }
  }
  return tags
}

type CompletedScenarioSummary = {
  score?: number
}

function buildCompletedScenarioSummaries(sessions: ScenarioSession[]) {
  const completedSummaries = new Map<string, CompletedScenarioSummary>()
  for (const session of sessions) {
    if (!isAnsweredScenarioSession(session)) continue
    if (completedSummaries.has(session.question_id)) continue
    const score = normalizeScenarioScore(session.score?.total)
    completedSummaries.set(session.question_id, {
      ...(typeof score === 'number' ? { score } : {}),
    })
  }
  return completedSummaries
}

function isAnsweredScenarioSession(session: ScenarioSession) {
  if (session.status !== 'evaluated') return false
  const completedSession = session as ScenarioSession & { ended_at?: string }
  return Boolean(
    session.user_answer?.trim()
    && session.evaluation_result
    && session.score
    && completedSession.ended_at,
  )
}

function normalizeScenarioScore(score: unknown) {
  return typeof score === 'number' && Number.isFinite(score) ? Math.round(score) : undefined
}

function formatCompletedScenarioScore(score: number | undefined) {
  return typeof score === 'number' ? `${score}分` : undefined
}

function scenarioSourceLabel(item: ScenarioQuestion) {
  if (item.source === 'llm_generated') return 'AI生成'
  if (item.source === 'ugc_structured') return '用户生成'
  return '系统生成'
}

function notifyRouterTelemetryUpdated() {
  const updatedAt = new Date().toISOString()
  window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  window.localStorage.setItem('ai-router-telemetry-updated-at', updatedAt)
  if (typeof BroadcastChannel !== 'undefined') {
    const channel = new BroadcastChannel('ai-router-telemetry')
    channel.postMessage({ task: 'scenario_generate', updatedAt })
    channel.close()
  }
}

function formatGenerationFailureMeta(failure: {
  provider?: string
  model?: string
  jobId?: string
  stage?: string
  fallbackUsed?: boolean
}) {
  const parts = [
    failure.provider ? `Provider：${aiProviderLabel(failure.provider, failure.fallbackUsed)}` : '',
    failure.model ? `Model：${failure.model}` : '',
    failure.stage ? `阶段：${aiJobStageText(failure.stage)}` : '',
    failure.fallbackUsed ? '已触发兜底链路' : '',
  ].filter(Boolean)
  return parts.join('；') || '未返回 provider/model/stage 信息。'
}
