import { useEffect } from 'react'
import { Bot, Database, FileText, Save, Settings, ShieldAlert, ShieldCheck, SlidersHorizontal } from 'lucide-react'
import type {
  ProviderPoolProvider,
  ProviderPoolStatus,
  RouterDecision,
  RouterTelemetry,
  SystemStatus,
} from '../../types'
import { Loading, Metric } from '../../components/common'
import { useToken } from '../../lib/auth'
import { aiCapabilitySummary, aiHealthLabel, aiModeLabel, aiTaskLabel } from '../../lib/ai'
import { systemStatusLabel } from '../../lib/labels'
import { useSystemStore } from '../../stores/systemStore'
import './SystemPage.css'

export function SystemPage() {
  const token = useToken()
  const {
    status,
    isLoading,
    message,
    error,
    promptError,
    isBulkSavingPromptEngines,
    promptDrafts,
    aiConfigDraft,
    initialize,
    dispose,
    loadPromptDraft,
    updatePromptDraft,
    updatePromptRenderEngine,
    updateAIConfigDraft,
    savePrompt,
    saveAllPromptRenderEngines,
    saveAIConfig,
  } = useSystemStore()

  useEffect(() => {
    void initialize(token)
    return () => dispose()
  }, [dispose, initialize, token])

  if (error && !status) return <span className="inline-error">{error}</span>
  if (!status || isLoading) return <Loading title="读取系统状态" />

  const routerTelemetry = status.ai.telemetry ?? emptyRouterTelemetry()
  const services = status.services ?? []
  const providerPool: ProviderPoolStatus = status.ai.provider_pool ?? {
    active_provider: status.ai.provider,
    fallback_order: [status.ai.provider].filter(Boolean),
    degraded_count: 0,
    providers: [],
    recent_attempts: [],
    updated_at: status.generated_at,
  }
  const recentDecisions = routerTelemetry.recent_decisions ?? []
  const supportedTasks = status.ai.capability?.supported_tasks ?? []
  const serviceSummary = summarizeServices(services)
  const heroHighlights = [
    { label: '服务总数', value: serviceSummary.total, tone: 'default' },
    { label: '正常运行', value: serviceSummary.ok, tone: 'accent' },
    { label: '需关注', value: serviceSummary.attention, tone: 'warning' },
    { label: '演示脚本', value: status.runbook?.length ?? 0, tone: 'neutral' },
  ] as const
  const heroTags = [
    status.ai.provider,
    status.ai.model,
    status.store?.mode ?? 'unknown',
    `更新于 ${formatOptionalDateTime(status.generated_at)}`,
  ]

  return (
    <section className="page-stack system-page">
      <section className="panel system-hero">
        <div className="system-hero-copy">
          <span className="system-hero-kicker">OPERATIONS BOARD</span>
          <div className="system-hero-title-row">
            <span className="system-hero-icon"><Settings size={24} /></span>
            <div className="system-hero-title-block">
              <h1>系统状态</h1>
              <div className="system-hero-tags">
                {heroTags.map((tag) => <span key={tag}>{tag}</span>)}
              </div>
            </div>
          </div>
        </div>
        <div className="system-hero-highlight-grid">
          {heroHighlights.map((item) => (
            <div className={`system-hero-highlight system-hero-highlight-${item.tone}`} key={item.label}>
              <span>{item.label}</span>
              <strong>{item.value}</strong>
            </div>
          ))}
        </div>
      </section>
      {error && <span className="inline-error">{error}</span>}
      <div className="system-service-grid">
        {services.map((service) => (
          <section className="panel system-service" key={service.name}>
            <div className="system-service-heading">
              <span className={`status-dot status-${service.status}`} />
              <strong>{service.name}</strong>
              <small>{systemStatusLabel(service.status)}</small>
            </div>
            <p>{service.detail}</p>
          </section>
        ))}
      </div>
      <div className="metric-row">
        <Metric label="用户数" value={status.counts.users} />
        <Metric label="题目数" value={status.counts.scenarios} />
        <Metric label="AI生成题" value={status.counts.generated_scenarios ?? 0} />
        <Metric label="AI任务" value={status.counts.ai_jobs ?? 0} />
        <Metric label="活跃题目" value={status.counts.active_scenarios} />
        <Metric label="待审 UGC" value={status.counts.pending_ugc} />
      </div>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><Bot size={18} /> AI Router 概览</div>
          <div className="system-kv">
            <span>Provider</span><strong>{status.ai.provider}</strong>
            <span>Model</span><strong>{status.ai.model}</strong>
            <span>健康</span><strong>{systemStatusLabel(aiHealthLabel(status.ai))}</strong>
            <span>模式</span><strong>{aiModeLabel(status.ai)}</strong>
            <span>Transport</span><strong>{status.ai.transport ?? 'openai-compatible'}</strong>
            <span>Router</span><strong>{status.ai.router_version ?? 'router-v1'}</strong>
            <span>Stream</span><strong>{status.ai.stream_enabled ? '启用' : '未启用'}</strong>
            <span>Fallback</span><strong>{status.ai.fallback ? '启用' : '未启用'}</strong>
          </div>
          <div className="checked-action-list">
            {(supportedTasks.length > 0 ? supportedTasks : ['scenario_generate', 'community_structure', 'scenario_reply', 'interview_feedback', 'sensitive_check'])
              .map((task) => <span key={task}>{aiTaskLabel(task)}</span>)}
          </div>
        </section>
        <section className="panel">
          <div className="panel-title"><ShieldCheck size={18} /> Router 统计</div>
          <div className="metric-row compact-metrics">
            <Metric label="总调用" value={routerTelemetry.total_calls} />
            <Metric label="成功" value={routerTelemetry.successful_calls} />
            <Metric label="失败" value={routerTelemetry.failed_calls} />
          </div>
          <div className="metric-row compact-metrics">
            <Metric label="Fallback" value={routerTelemetry.fallback_calls} />
            <Metric label="流式" value={routerTelemetry.stream_calls} />
            <Metric label="JSON" value={routerTelemetry.json_calls} />
          </div>
          <div className="metric-row compact-metrics">
            <Metric label="安全重写" value={routerTelemetry.safety_rewrites} />
            <Metric label="校验失败" value={routerTelemetry.validation_errors} />
            <Metric label="最近更新" value={formatOptionalDateTime(routerTelemetry.updated_at)} variant="compact" />
          </div>
        </section>
      </div>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><ShieldCheck size={18} /> 演示账号</div>
          <div className="list-stack">
            {(status.demo_accounts ?? []).map((account) => (
              <div className="review-turn" key={account.username}>
                <strong>{account.username} · {account.role}</strong>
                <span>{account.purpose}</span>
              </div>
            ))}
          </div>
        </section>
        <section className="panel">
          <div className="panel-title"><Database size={18} /> 存储运行模式</div>
          <div className="system-kv">
            <span>Store</span><strong>{status.store?.mode ?? 'unknown'}</strong>
            <span>持久化</span><strong>{status.store?.persistent ? '已启用' : '临时内存'}</strong>
            <span>AI生成题</span><strong>{status.counts.generated_scenarios ?? 0}</strong>
            <span>AI Job</span><strong>{status.counts.ai_jobs ?? 0}</strong>
          </div>
          {status.store?.warning && <span className="inline-error">{status.store.warning}</span>}
        </section>
      </div>
      <div className="system-signal-grid">
        <div className="system-signal-column">
          <section className="panel">
            <div className="panel-title"><Bot size={18} /> Router 运行标签</div>
            <div className="checked-action-list">
              <span>{status.ai.health ?? 'ok'}</span>
              <span>{status.ai.transport ?? 'openai-compatible'}</span>
              <span>{status.ai.router_version ?? 'router-v1'}</span>
              <span>{status.ai.capability?.cost_tier ?? 'standard'}</span>
            </div>
          </section>
          <section className="panel">
            <div className="panel-title"><Bot size={18} /> 最近错误与追踪</div>
            <div className="system-kv">
              <span>Trace</span><strong>{status.ai.last_trace_id ?? '暂无'}</strong>
              <span>任务</span><strong>{aiTaskLabel(status.ai.last_task)}</strong>
              <span>延迟</span><strong>{status.ai.last_latency_ms ?? 0} ms</strong>
              <span>错误类型</span><strong>{status.ai.last_error_type ?? '无'}</strong>
              <span>回退原因</span><strong>{status.ai.last_fallback_reason ?? '无'}</strong>
              <span>错误时间</span><strong>{formatOptionalDateTime(status.ai.last_error_at)}</strong>
            </div>
            <div className="system-kv" style={{ marginTop: 16 }}>
              <span>能力摘要</span><strong>{aiCapabilitySummary(status.ai)}</strong>
              <span>模型状态</span><strong>{status.ai.healthy ? '稳定' : '需关注'}</strong>
            </div>
          </section>
        </div>
        <section className="panel">
          <div className="panel-title"><Bot size={18} /> 最近决策</div>
          <div className="review-thread">
            {recentDecisions.length > 0 ? recentDecisions.slice(0, 6).map((decision) => (
              <RouterDecisionRow key={decision.trace_id} decision={decision} />
            )) : (
              <div className="review-turn">
                <strong>暂无路由决策</strong>
                <span>尚未产生 AI 调用或 telemetry 未上报。</span>
              </div>
            )}
          </div>
        </section>
      </div>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><Bot size={18} /> Agent 运行摘要</div>
          <div className="system-kv">
            <span>最近运行</span><strong data-testid="agent-summary-total">{status.agent_summary?.total_recent ?? 0}</strong>
            <span>最新 Agent</span><strong data-testid="agent-summary-latest-agent">{status.agent_summary?.latest_agent ?? 'diagnostic_agent'}</strong>
            <span>失败次数</span><strong data-testid="agent-summary-failed">{status.agent_summary?.failed_recent ?? 0}</strong>
            <span>安全重写</span><strong data-testid="agent-summary-safety-rewritten">{status.agent_summary?.safety_rewritten_recent ?? 0}</strong>
            <span>风险标记</span><strong data-testid="agent-summary-flagged">{status.agent_summary?.flagged_recent ?? 0}</strong>
            <span>最近时间</span><strong data-testid="agent-summary-latest-run">{formatOptionalDateTime(status.agent_summary?.latest_run_at)}</strong>
          </div>
          <AgentBreakdownPanel summary={status.agent_summary} />
        </section>
        <section className="panel">
          <div className="panel-title"><ShieldAlert size={18} /> Sensitive Detection</div>
          <div className="system-kv">
            <span>状态</span><strong>{systemStatusLabel(status.sensitive_detection?.status ?? 'missing')}</strong>
            <span>Provider</span><strong>{status.sensitive_detection?.provider ?? 'unknown'}</strong>
            <span>Model</span><strong>{status.sensitive_detection?.model ?? 'unknown'}</strong>
            <span>Schema</span><strong>{status.sensitive_detection?.schema ?? 'sensitive_check'}</strong>
            <span>规则检测</span><strong>{status.sensitive_detection?.rule_enabled ? '启用' : '未启用'}</strong>
            <span>模型检测</span><strong>{status.sensitive_detection?.model_enabled ? '启用' : '未启用'}</strong>
            <span>回退状态</span><strong>{status.sensitive_detection?.fallback_used ? `已回退 ${status.sensitive_detection?.fallback_count ?? 0} 次` : '未回退'}</strong>
            <span>说明</span><strong>{status.sensitive_detection?.detail ?? '敏感信息检测状态未上报'}</strong>
          </div>
          {(status.sensitive_detection?.checked_actions ?? []).length > 0 && (
            <div className="checked-action-list">
              {status.sensitive_detection?.checked_actions?.map((action) => <span key={action}>{action}</span>)}
            </div>
          )}
        </section>
      </div>
      <div className="two-column">
        <section className="panel">
          <div className="panel-title"><SlidersHorizontal size={18} /> 模型参数</div>
          {aiConfigDraft && (
            <div className="admin-config-grid">
              <label>Provider<input value={aiConfigDraft.provider} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, provider: event.target.value })} /></label>
              <label>Model<input value={aiConfigDraft.model} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, model: event.target.value })} /></label>
              <label>Base URL<input value={aiConfigDraft.base_url ?? ''} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, base_url: event.target.value })} /></label>
              <label>Fallback<input value={aiConfigDraft.fallback_model} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, fallback_model: event.target.value })} /></label>
              <label>Temperature<input type="number" step="0.1" value={aiConfigDraft.temperature} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, temperature: Number(event.target.value) })} /></label>
              <label>Top P<input type="number" step="0.1" value={aiConfigDraft.top_p} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, top_p: Number(event.target.value) })} /></label>
              <label>Top K<input type="number" step="1" value={aiConfigDraft.top_k} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, top_k: Number(event.target.value) })} /></label>
              <label>Max Tokens<input type="number" step="1" value={aiConfigDraft.max_tokens} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, max_tokens: Number(event.target.value) })} /></label>
              <label>Stream Enabled<input type="checkbox" checked={aiConfigDraft.stream_enabled} onChange={(event) => updateAIConfigDraft({ ...aiConfigDraft, stream_enabled: event.target.checked })} /></label>
              <button className="primary-button compact" type="button" onClick={() => void saveAIConfig()}><Save size={16} />保存配置</button>
            </div>
          )}
        </section>
        <section className="panel">
          <div className="panel-title"><ShieldCheck size={18} /> Schema 校验</div>
          <div className="list-stack">
            {(status.schema_validators ?? []).map((validator) => {
              const schemaName = validator.schema_name ?? validator.name
              return (
                <div className="review-turn schema-validator-row" key={schemaName}>
                  <div>
                    <strong>{schemaName}</strong>
                    <span className={`schema-status schema-status-${validator.status ?? 'ok'}`}>
                      {schemaStatusLabel(validator.status)}
                    </span>
                  </div>
                  <span>{validator.target}</span>
                  <small>{validator.task ?? 'AI'} · v{validator.version ?? '1.0.0'} · {validator.description ?? '结构化输出校验'}</small>
                </div>
              )
            })}
          </div>
        </section>
      </div>
      <section className="panel">
        <div className="panel-title system-panel-title-row">
          <span className="system-panel-title-main"><Bot size={18} /> Prompt 模板</span>
          <div className="system-panel-actions">
            <button
              className="ghost-button compact"
              type="button"
              onClick={() => void saveAllPromptRenderEngines('go_template')}
              disabled={isBulkSavingPromptEngines}
            >
              全部保存为 go_template
            </button>
            <button
              className="ghost-button compact"
              type="button"
              onClick={() => void saveAllPromptRenderEngines('jinja2')}
              disabled={isBulkSavingPromptEngines}
            >
              {isBulkSavingPromptEngines ? '批量保存中' : '全部保存为 jinja2'}
            </button>
          </div>
        </div>
        <div className="prompt-admin-list">
          {(status.prompt_templates ?? []).map((prompt) => {
            const renderEngine = prompt.render_engine ?? 'go_template'
            const editableLoaded = promptDrafts[prompt.name] !== undefined
            return (
            <div className="prompt-admin-item" key={prompt.name}>
              <div className="prompt-admin-header">
                <strong>{prompt.name} · {prompt.task}</strong>
                <span>{prompt.validator} · {renderEngine}{prompt.is_modified ? ' · 已修改' : ' · 默认模板'}</span>
              </div>
              <div className="system-kv">
                <span>摘要</span><strong>{prompt.summary}</strong>
                <span>引擎</span><strong>{renderEngine}</strong>
                <span>当前长度</span><strong>{prompt.content_length}</strong>
                <span>默认长度</span><strong>{prompt.default_length}</strong>
                <span>更新时间</span><strong>{formatOptionalDateTime(prompt.updated_at)}</strong>
              </div>
              <label>Render Engine
                <select
                  value={renderEngine}
                  onChange={(event) => {
                    updatePromptRenderEngine(prompt.name, event.target.value)
                  }}
                >
                  <option value="go_template">go_template</option>
                  <option value="jinja2">jinja2</option>
                </select>
              </label>
              {editableLoaded ? (
                <textarea value={promptDrafts[prompt.name]} onChange={(event) => updatePromptDraft(prompt.name, event.target.value)} />
              ) : (
                <div className="review-turn">
                  <strong>原文已从系统状态脱敏</strong>
                  <span>如需编辑，请通过管理员 Prompt 专用接口加载原文。</span>
                </div>
              )}
              <div className="card-actions">
                <button className="ghost-button compact" type="button" onClick={() => void loadPromptDraft(prompt.name)}>加载编辑原文</button>
                <button className="ghost-button compact" type="button" onClick={() => void savePrompt({ ...prompt, render_engine: renderEngine }, true)}>回滚默认</button>
                <button className="primary-button compact" type="button" onClick={() => void savePrompt({ ...prompt, render_engine: renderEngine })}><Save size={16} />保存 Prompt</button>
              </div>
            </div>
          )})}
        </div>
        {promptError && <span className="inline-error">{promptError}</span>}
        {message && <span className="success-line">{message}</span>}
      </section>
      <section className="panel">
        <div className="panel-title"><ShieldCheck size={18} /> 审计与限流</div>
        <div className="metric-row compact-metrics">
          <Metric label="最近事件" value={status.audit_summary?.total_recent ?? 0} />
          <Metric label="限流状态" value={status.rate_limit?.enabled ? '启用' : '未启用'} />
          <Metric label="AI/限流异常" value={(status.recent_ai_errors ?? []).length} />
        </div>
        <div className="review-thread">
          {(status.audit_summary?.latest ?? []).slice(0, 6).map((event) => (
            <div className="review-turn" key={event.id}>
              <strong>{event.action} · {event.resource_type}</strong>
              <span>{event.resource_id || 'system'} · {formatDateTime(event.created_at)}</span>
            </div>
          ))}
        </div>
      </section>
      <ProviderPoolPanel pool={providerPool} />
      <section className="panel">
        <div className="panel-title"><FileText size={18} /> 演示脚本入口</div>
        <div className="runbook-list">
          {(status.runbook ?? []).map((item) => (
            <div key={item.command}>
              <span>{item.title}</span>
              <code>{item.command}</code>
            </div>
          ))}
        </div>
      </section>
    </section>
  )
}
function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function formatOptionalDateTime(value?: string) {
  if (!value) return '暂无'
  return formatDateTime(value)
}

function AgentBreakdownPanel({ summary }: { summary?: SystemStatus['agent_summary'] }) {
  const items = summary?.per_agent ?? []
  return (
    <div className="review-history system-agent-breakdown">
      <strong>分 Agent 摘要</strong>
      {items.length > 0 ? items.map((item) => (
        <div key={item.agent} className="review-turn">
          <strong>{item.agent}</strong>
          <span>
            最近运行 {item.total_recent ?? 0} 次 · 失败 {item.failed_recent ?? 0} 次 · 安全重写 {item.safety_rewritten_recent ?? 0} 次 · 风险标记 {item.flagged_recent ?? 0} 次
          </span>
          <small>
            最近状态 {item.latest_status || '未上报'} · 最近时间 {formatOptionalDateTime(item.latest_run_at)}
          </small>
        </div>
      )) : (
        <div className="review-turn system-agent-breakdown-empty">
          <strong>分 Agent 数据待接入</strong>
          <span>当前先预留展示位，等待后端返回 `agent_summary.per_agent`。</span>
        </div>
      )}
    </div>
  )
}

function schemaStatusLabel(status?: string) {
  if (!status || status === 'ok') return '通过'
  if (status === 'degraded') return '异常'
  return '待确认'
}

function emptyRouterTelemetry(): RouterTelemetry {
  return {
    total_calls: 0,
    successful_calls: 0,
    failed_calls: 0,
    fallback_calls: 0,
    stream_calls: 0,
    json_calls: 0,
    safety_rewrites: 0,
    validation_errors: 0,
    provider_calls: {},
    task_calls: {},
    recent_decisions: [],
    updated_at: new Date().toISOString(),
  }
}

function ProviderPoolPanel({ pool }: { pool: ProviderPoolStatus }) {
  return (
    <section className="panel" data-testid="provider-pool-panel">
      <div className="panel-title"><Bot size={18} /> Provider 能力矩阵</div>
          <div className="metric-row compact-metrics">
            <Metric label="Active Provider" value={pool.active_provider} />
            <Metric label="Active Model" value={resolveActiveModel(pool)} />
            <Metric label="Degraded" value={pool.degraded_count} />
            <Metric label="最近更新" value={formatOptionalDateTime(pool.updated_at)} variant="compact" />
          </div>
      <div className="review-turn" data-testid="fallback-order" style={{ overflowWrap: 'anywhere' }}>
        <strong>Fallback Order</strong>
        <span>{pool.fallback_order.length > 0 ? pool.fallback_order.join(' -> ') : 'unknown'}</span>
      </div>
      <div className="review-thread">
        {pool.providers.map((provider) => (
          <div
            className="review-turn"
            data-testid={`provider-row-${provider.provider}`}
            key={`${provider.provider}-${provider.model}`}
            style={{ overflowWrap: 'anywhere', wordBreak: 'break-word' }}
          >
            <strong>{provider.provider} / {provider.model}</strong>
            <span>
              health {provider.health} · status {provider.status} · priority {provider.priority} · calls {provider.call_count}
            </span>
            <small>
              {provider.enabled ? 'enabled' : 'disabled'} · {provider.transport} · {provider.supports_streaming ? 'stream' : 'no-stream'} · {provider.supports_json ? 'json' : 'no-json'} · {provider.top_k ? 'top-k' : 'no-top-k'} · {formatRateLimit(provider.rate_limit)}
            </small>
            <small>
              最近检查 {formatOptionalDateTime(provider.last_checked_at)} · 最大 tokens {provider.max_tokens} · 成本 {provider.cost_tier}
            </small>
            <small>
              最近错误 {provider.last_error_type ?? 'none'} · 降级原因 {provider.fallback_reason ?? 'none'}
            </small>
          </div>
        ))}
        {pool.providers.length === 0 && (
          <div className="review-turn">
            <strong>Provider 池暂无数据</strong>
            <span>系统状态未返回 providers，前端已使用空状态兜底。</span>
          </div>
        )}
      </div>
      <div className="review-history">
        <strong>recent_attempts</strong>
        {pool.recent_attempts.length > 0 ? pool.recent_attempts.map((attempt, index) => (
          <div className="review-turn" key={`${attempt.provider}-${attempt.model}-${attempt.started_at}-${index}`} style={{ overflowWrap: 'anywhere' }}>
            <strong>{attempt.provider} / {attempt.model}</strong>
            <span>{attempt.success ? 'success' : 'failed'} · {attempt.latency_ms ?? 0} ms · 开始于 {formatOptionalDateTime(attempt.started_at)}</span>
            <small>{attempt.error_type ?? 'none'} · {attempt.fallback_reason ?? 'none'} · 完成于 {formatOptionalDateTime(attempt.completed_at ?? attempt.started_at)}</small>
          </div>
        )) : (
          <div className="review-turn">
            <strong>暂无 recent_attempts</strong>
            <span>Provider 池尚未返回 fallback 尝试记录。</span>
          </div>
        )}
      </div>
    </section>
  )
}

function formatRateLimit(rateLimit: ProviderPoolProvider['rate_limit']) {
  if (!rateLimit) return 'rate-limit none'
  if (typeof rateLimit === 'string') return `rate-limit ${rateLimit}`
  if (rateLimit.status || rateLimit.limit !== undefined || rateLimit.in_flight !== undefined) {
    return `rate-limit ${rateLimit.status ?? 'ok'} ${rateLimit.in_flight ?? 0}/${rateLimit.limit ?? 'unlimited'}`
  }
  const remaining = rateLimit.remaining ?? 'unknown'
  const resetAt = rateLimit.reset_at ? ` reset ${formatOptionalDateTime(rateLimit.reset_at)}` : ''
  return `rate-limit ${rateLimit.enabled ? 'enabled' : 'disabled'} remaining ${remaining}${resetAt}${rateLimit.detail ? ` ${rateLimit.detail}` : ''}`
}

function resolveActiveModel(pool: ProviderPoolStatus) {
  const activeProvider = pool.providers.find((provider) => provider.provider === pool.active_provider)
  return activeProvider?.model ?? 'unknown'
}

function summarizeServices(services: NonNullable<SystemStatus['services']>) {
  return services.reduce((summary, service) => {
    summary.total += 1
    if (service.status === 'ok') {
      summary.ok += 1
      return summary
    }
    if (service.status === 'fallback' || service.status === 'degraded') {
      summary.attention += 1
      return summary
    }
    summary.offline += 1
    return summary
  }, {
    total: 0,
    ok: 0,
    attention: 0,
    offline: 0,
  })
}

function RouterDecisionRow({ decision }: { decision: RouterDecision }) {
  const promptName = decision.prompt_template?.name ?? decision.prompt ?? 'unmanaged'
  const promptVersion = decision.prompt_template?.version ?? 'unknown'
  const schemaName = decision.schema ?? decision.validation.schema ?? 'none'
  const parseStatus = decision.output?.parse_status ?? 'skipped'
  const repairLabel = decision.output?.repair_used ? '已修复' : '未修复'
  const safetyFlags = [
    decision.safety.blocked ? '已拦截' : '',
    decision.safety.rewrite_used ? '已重写' : '',
  ].filter(Boolean).join(' · ')
  return (
    <div className="review-turn">
      <strong>{aiTaskLabel(decision.task)} · {decision.provider}</strong>
      <span>
        {decision.trace_id} · {decision.status} · {decision.latency_ms ?? 0} ms · {decision.validation.status}
        {decision.validation.detail ? ` · ${decision.validation.detail}` : ''}
      </span>
      <small>
        模式 {decision.output_mode} · 解析 {parseStatus} · {repairLabel} · {decision.stream ? '流式' : '非流式'} · 安全 {decision.safety.status}
        {safetyFlags ? ` · ${safetyFlags}` : ''}
        {decision.fallback_chain.length > 0 ? ` · 链路 ${decision.fallback_chain.join(' → ')}` : ''}
      </small>
      {decision.safety.detail && <small>{decision.safety.detail}</small>}
      <small>
        Prompt {promptName}@{promptVersion} · Schema {schemaName} · Context {decision.context.strategy}
        {decision.context.retained_messages > 0 ? ` ${decision.context.retained_messages}/${decision.context.original_messages}` : ''}
      </small>
    </div>
  )
}
