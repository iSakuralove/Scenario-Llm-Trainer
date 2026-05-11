import { create } from 'zustand'
import { api } from '../api/client'
import type {
  AIConfig,
  AIStatus,
  FallbackAttempt,
  PromptTemplate,
  PromptTemplateStatus,
  ProviderPoolProvider,
  ProviderPoolStatus,
  RouterDecision,
  RouterTelemetry,
  SystemStatus,
} from '../types'

type RefreshMode = 'initial' | 'refresh'

interface SystemStoreState {
  token: string
  status: SystemStatus | null
  isLoading: boolean
  isBulkSavingPromptEngines: boolean
  message: string
  error: string
  promptError: string
  promptDrafts: Record<string, string>
  loadedPromptSources: Record<string, string>
  promptEngineDrafts: Record<string, string>
  aiConfigDraft: AIConfig | null
  draftsInitialized: boolean
  initialize: (token: string) => Promise<void>
  dispose: () => void
  refreshStatus: (mode?: RefreshMode) => Promise<void>
  loadPromptDraft: (name: string) => Promise<void>
  updatePromptDraft: (name: string, content: string) => void
  updatePromptRenderEngine: (name: string, renderEngine: string) => void
  savePrompt: (prompt: PromptTemplateStatus, resetDefault?: boolean) => Promise<void>
  saveAllPromptRenderEngines: (renderEngine: string) => Promise<void>
  updateAIConfigDraft: (draft: AIConfig | null) => void
  saveAIConfig: () => Promise<void>
}

let pollTimer: number | null = null
let teardownListeners: (() => void) | null = null

function bindRefreshListeners() {
  if (teardownListeners) return

  const refresh = () => {
    void useSystemStore.getState().refreshStatus('refresh')
  }

  pollTimer = window.setInterval(refresh, 5000)
  const onFocus = () => refresh()
  const onVisibilityChange = () => refresh()
  const onCustomRefresh = () => refresh()
  const onStorage = (event: StorageEvent) => {
    if (event.key === 'ai-router-telemetry-updated-at') {
      refresh()
    }
  }

  window.addEventListener('focus', onFocus)
  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('ai-router:refresh', onCustomRefresh)
  window.addEventListener('storage', onStorage)

  let channel: BroadcastChannel | null = null
  if (typeof BroadcastChannel !== 'undefined') {
    channel = new BroadcastChannel('ai-router-telemetry')
    channel.onmessage = () => refresh()
  }

  teardownListeners = () => {
    if (pollTimer !== null) {
      window.clearInterval(pollTimer)
      pollTimer = null
    }
    window.removeEventListener('focus', onFocus)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('ai-router:refresh', onCustomRefresh)
    window.removeEventListener('storage', onStorage)
    channel?.close()
    teardownListeners = null
  }
}

function unbindRefreshListeners() {
  teardownListeners?.()
}

export const useSystemStore = create<SystemStoreState>((set, get) => ({
  token: '',
  status: null,
  isLoading: false,
  isBulkSavingPromptEngines: false,
  message: '',
  error: '',
  promptError: '',
  promptDrafts: {},
  loadedPromptSources: {},
  promptEngineDrafts: {},
  aiConfigDraft: null,
  draftsInitialized: false,

  initialize: async (token: string) => {
    const state = get()
    if (!token) {
      get().dispose()
      return
    }

    const tokenChanged = state.token !== token
    set({ token, isLoading: true })
    bindRefreshListeners()

    if (tokenChanged) {
      set({
        status: null,
        message: '',
        error: '',
        isBulkSavingPromptEngines: false,
        promptError: '',
        promptDrafts: {},
        loadedPromptSources: {},
        promptEngineDrafts: {},
        aiConfigDraft: null,
        draftsInitialized: false,
      })
    }

    await get().refreshStatus('initial')
  },

  dispose: () => {
    unbindRefreshListeners()
    set({
      token: '',
      status: null,
      isLoading: false,
      isBulkSavingPromptEngines: false,
      message: '',
      error: '',
      promptError: '',
      promptDrafts: {},
      loadedPromptSources: {},
      promptEngineDrafts: {},
      aiConfigDraft: null,
      draftsInitialized: false,
    })
  },

  refreshStatus: async (mode = 'refresh') => {
    const state = get()
    if (!state.token) return

    if (mode === 'initial') {
      set({ isLoading: true })
    }

    try {
      let normalized = normalizeSystemStatus(await api.systemStatus(state.token))
      if (mode !== 'initial') {
        normalized = {
          ...normalized,
          prompt_templates: (normalized.prompt_templates ?? []).map((prompt) => ({
            ...prompt,
            render_engine: state.promptEngineDrafts[prompt.name] ?? prompt.render_engine,
          })),
        }
      }

      set((current) => ({
        status: normalized,
        isLoading: false,
        error: '',
        aiConfigDraft: mode === 'initial' || !current.draftsInitialized
          ? normalized.ai_config ?? null
          : current.aiConfigDraft,
        promptDrafts: mode === 'initial' || !current.draftsInitialized ? {} : current.promptDrafts,
        loadedPromptSources: mode === 'initial' || !current.draftsInitialized ? {} : current.loadedPromptSources,
        promptEngineDrafts: mode === 'initial' || !current.draftsInitialized ? {} : current.promptEngineDrafts,
        draftsInitialized: true,
      }))
    } catch (err) {
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : '读取系统状态失败',
      })
    }
  },

  loadPromptDraft: async (name: string) => {
    const state = get()
    if (!state.token || state.promptDrafts[name] !== undefined) return

    set({ message: '', promptError: '' })
    try {
      const result = await api.adminPrompts(state.token)
      const editable = result.list.find((item) => item.name === name)
      if (!editable) {
        set({ promptError: `未找到 Prompt：${name}` })
        return
      }

      set((current) => ({
        promptDrafts: { ...current.promptDrafts, [name]: editable.content },
        loadedPromptSources: { ...current.loadedPromptSources, [name]: editable.content },
      }))
    } catch (err) {
      set({
        promptError: err instanceof Error ? err.message : '读取 Prompt 原文失败',
      })
    }
  },

  updatePromptDraft: (name: string, content: string) => {
    set((state) => ({
      promptDrafts: { ...state.promptDrafts, [name]: content },
    }))
  },

  updatePromptRenderEngine: (name: string, renderEngine: string) => {
    set((state) => {
      const nextStatus = state.status ? {
        ...state.status,
        prompt_templates: (state.status.prompt_templates ?? []).map((prompt) => (
          prompt.name === name ? { ...prompt, render_engine: renderEngine } : prompt
        )),
      } : state.status
      return {
        status: nextStatus,
        promptEngineDrafts: { ...state.promptEngineDrafts, [name]: renderEngine },
      }
    })
  },

  savePrompt: async (prompt: PromptTemplateStatus, resetDefault = false) => {
    const state = get()
    if (!state.token) return

    set({ message: '', promptError: '' })

    const hasContentDraft = state.promptDrafts[prompt.name] !== undefined
    const hasEngineDraft = state.promptEngineDrafts[prompt.name] !== undefined
    const renderEngine = state.promptEngineDrafts[prompt.name] ?? prompt.render_engine ?? 'go_template'

    if (!resetDefault && !hasContentDraft && !hasEngineDraft) {
      set({ promptError: '请先加载编辑原文并修改后再保存' })
      return
    }

    if (!resetDefault) {
      const draft = state.promptDrafts[prompt.name]
      const source = state.loadedPromptSources[prompt.name]
      const contentChanged = draft !== undefined && source !== undefined && draft !== source
      if (!contentChanged && !hasEngineDraft) {
        set({ promptError: '请先修改 Prompt 内容或切换引擎后再保存' })
        return
      }
      if (draft !== undefined && draft.trim() === '') {
        set({ promptError: 'Prompt 原文不能为空' })
        return
      }
    }

    try {
      const updated = await api.updateAdminPrompt(state.token, prompt.name, {
        content: resetDefault ? undefined : state.promptDrafts[prompt.name],
        render_engine: renderEngine,
        reset_default: resetDefault,
      })

      set((current) => {
        const nextPromptEngineDrafts = { ...current.promptEngineDrafts }
        delete nextPromptEngineDrafts[updated.name]
        return {
          status: current.status ? {
            ...current.status,
            prompt_templates: (current.status.prompt_templates ?? []).map((item) => (
              item.name === updated.name ? promptStatusFromEditable(updated) : item
            )),
          } : current.status,
          promptDrafts: { ...current.promptDrafts, [updated.name]: updated.content },
          loadedPromptSources: { ...current.loadedPromptSources, [updated.name]: updated.content },
          promptEngineDrafts: nextPromptEngineDrafts,
          message: `已保存 Prompt：${updated.name}`,
        }
      })
    } catch (err) {
      set({
        promptError: err instanceof Error ? err.message : '保存 Prompt 失败',
      })
    }
  },

  saveAllPromptRenderEngines: async (renderEngine: string) => {
    const state = get()
    if (!state.token || !state.status?.prompt_templates?.length) return

    set({ message: '', promptError: '', isBulkSavingPromptEngines: true })

    try {
      const editablePrompts = await api.adminPrompts(state.token)
      const updatedPrompts: PromptTemplate[] = []
      for (const prompt of state.status.prompt_templates) {
        const editable = editablePrompts.list.find((item) => item.name === prompt.name)
        if (!editable) {
          throw new Error(`未找到 Prompt 原文：${prompt.name}`)
        }
        const updated = await api.updateAdminPrompt(state.token, prompt.name, {
          content: editable.content,
          render_engine: renderEngine,
        })
        updatedPrompts.push(updated)
      }

      set((current) => {
        const nextDrafts = { ...current.promptEngineDrafts }
        for (const prompt of updatedPrompts) {
          delete nextDrafts[prompt.name]
        }

        return {
          status: current.status ? {
            ...current.status,
            prompt_templates: (current.status.prompt_templates ?? []).map((item) => {
              const updated = updatedPrompts.find((prompt) => prompt.name === item.name)
              return updated ? promptStatusFromEditable(updated) : item
            }),
          } : current.status,
          promptEngineDrafts: nextDrafts,
          isBulkSavingPromptEngines: false,
          message: `已批量保存 ${updatedPrompts.length} 个 Prompt 为 ${renderEngine}`,
        }
      })
    } catch (err) {
      set({
        isBulkSavingPromptEngines: false,
        promptError: err instanceof Error ? err.message : '批量保存 Prompt 引擎失败',
      })
    }
  },

  updateAIConfigDraft: (draft) => {
    set({ aiConfigDraft: draft })
  },

  saveAIConfig: async () => {
    const state = get()
    if (!state.token || !state.aiConfigDraft) return

    set({ message: '', error: '' })
    try {
      const updated = await api.updateAdminAIConfig(state.token, state.aiConfigDraft)
      set((current) => ({
        status: current.status ? { ...current.status, ai_config: updated } : current.status,
        aiConfigDraft: updated,
        message: '已保存 AI 配置',
      }))
      await get().refreshStatus('refresh')
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : '保存 AI 配置失败',
      })
    }
  },
}))

function normalizeSystemStatus(status: SystemStatus): SystemStatus {
  const raw = status as Partial<SystemStatus> & {
    ai?: Partial<SystemStatus['ai']>
    counts?: Partial<SystemStatus['counts']>
    store?: Partial<NonNullable<SystemStatus['store']>>
    sensitive_detection?: Partial<NonNullable<SystemStatus['sensitive_detection']>>
  }

  return {
    generated_at: raw.generated_at ?? new Date().toISOString(),
    services: raw.services ?? [],
    ai: normalizeAIStatus(raw.ai),
    store: {
      mode: raw.store?.mode ?? 'memory',
      persistent: raw.store?.persistent ?? false,
      warning: raw.store?.warning,
    },
    ai_config: normalizeAIConfig(raw.ai_config ?? {
      provider: raw.ai?.configured_provider ?? raw.ai?.provider ?? 'mock',
      model: raw.ai?.configured_model ?? raw.ai?.model ?? 'mock',
      temperature: 0.2,
      top_p: 0,
      top_k: 0,
      max_tokens: 0,
      stream_enabled: raw.ai?.stream_enabled ?? true,
      fallback_model: 'mock',
      updated_at: new Date().toISOString(),
    }),
    sensitive_detection: normalizeSensitiveDetection(raw.sensitive_detection, raw.ai),
    prompt_templates: normalizePromptTemplateStatuses(raw.prompt_templates ?? []),
    schema_validators: normalizeSchemaValidators(raw.schema_validators ?? []),
    rate_limit: raw.rate_limit ?? { enabled: false, detail: 'Rate limiting is using noop fallback' },
    audit_summary: raw.audit_summary ?? { total_recent: 0, by_action: {}, latest: [] },
    agent_summary: normalizeAgentSummary(raw.agent_summary),
    recent_ai_errors: raw.recent_ai_errors ?? [],
    counts: {
      users: raw.counts?.users ?? 0,
      scenarios: raw.counts?.scenarios ?? 0,
      active_scenarios: raw.counts?.active_scenarios ?? 0,
      community_posts: raw.counts?.community_posts ?? 0,
      pending_ugc: raw.counts?.pending_ugc ?? 0,
      generated_scenarios: raw.counts?.generated_scenarios ?? 0,
      ai_jobs: raw.counts?.ai_jobs ?? 0,
    },
    demo_accounts: raw.demo_accounts ?? [],
    runbook: raw.runbook ?? [],
  }
}

function normalizeAIConfig(config: AIConfig): AIConfig {
  return {
    ...config,
    provider: config.provider ?? '',
    model: config.model ?? '',
    base_url: config.base_url ?? '',
    temperature: config.temperature ?? 0,
    top_p: config.top_p ?? 1,
    top_k: config.top_k ?? 0,
    max_tokens: config.max_tokens ?? 0,
    stream_enabled: config.stream_enabled ?? true,
    fallback_model: config.fallback_model ?? '',
  }
}

function normalizePromptTemplateStatuses(templates: Partial<PromptTemplateStatus>[]): PromptTemplateStatus[] {
  return templates.map((template) => ({
    name: template.name ?? 'unknown',
    task: template.task ?? 'unknown',
    render_engine: template.render_engine ?? 'go_template',
    updated_by: template.updated_by,
    updated_at: template.updated_at ?? new Date().toISOString(),
    is_modified: template.is_modified ?? false,
    validator: template.validator ?? 'unknown',
    summary: template.summary ?? '原文已从系统状态脱敏',
    content_length: template.content_length ?? 0,
    default_length: template.default_length ?? template.content_length ?? 0,
  }))
}

function promptStatusFromEditable(prompt: PromptTemplate): PromptTemplateStatus {
  const contentLength = prompt.content?.length ?? 0
  const defaultLength = prompt.default?.length ?? contentLength
  return {
    name: prompt.name,
    task: prompt.task,
    render_engine: prompt.render_engine ?? 'go_template',
    updated_by: prompt.updated_by,
    updated_at: prompt.updated_at,
    is_modified: prompt.is_modified,
    validator: prompt.validator,
    summary: `原文已从系统状态脱敏，当前 ${contentLength} 字符`,
    content_length: contentLength,
    default_length: defaultLength,
  }
}

function normalizeAIStatus(ai: Partial<AIStatus> | undefined): AIStatus {
  const fallback = ai?.fallback ?? ai?.health === 'fallback'
  const normalized: AIStatus = {
    provider: ai?.provider ?? 'unknown',
    model: ai?.model ?? 'unknown',
    base_url: ai?.base_url,
    fallback,
    configured_provider: ai?.configured_provider,
    configured_model: ai?.configured_model,
    init_error: ai?.init_error,
    stream_enabled: ai?.stream_enabled ?? true,
    router_version: ai?.router_version ?? 'router-v1',
    healthy: ai?.healthy ?? !fallback,
    health: ai?.health ?? (fallback ? 'fallback' : 'ok'),
    transport: ai?.transport ?? 'openai-compatible',
    last_trace_id: ai?.last_trace_id,
    last_task: ai?.last_task,
    last_latency_ms: ai?.last_latency_ms,
    last_error_type: ai?.last_error_type,
    last_error: ai?.last_error,
    last_error_at: ai?.last_error_at,
    last_fallback_reason: ai?.last_fallback_reason ?? ai?.telemetry?.last_fallback_reason,
    last_fallback_error: ai?.last_fallback_error ?? ai?.telemetry?.last_fallback_error,
    capability: normalizeCapability(ai?.capability, ai),
    telemetry: normalizeRouterTelemetry(ai?.telemetry),
  }
  normalized.telemetry = ensureRouterTelemetry(normalized)
  normalized.provider_pool = normalizeProviderPool(ai?.provider_pool, normalized)
  const lastDecision = normalized.telemetry.last_decision ?? normalized.telemetry.recent_decisions[0]
  normalized.last_trace_id = normalized.last_trace_id ?? lastDecision?.trace_id
  normalized.last_task = normalized.last_task ?? lastDecision?.task
  normalized.last_latency_ms = normalized.last_latency_ms ?? lastDecision?.latency_ms
  return normalized
}

function normalizeCapability(
  capability: Partial<NonNullable<AIStatus['capability']>> | undefined,
  ai: Partial<AIStatus> | undefined,
) {
  return {
    provider: capability?.provider ?? ai?.provider ?? 'unknown',
    model: capability?.model ?? ai?.model ?? 'unknown',
    transport: capability?.transport ?? ai?.transport ?? 'openai-compatible',
    supports_streaming: capability?.supports_streaming ?? ai?.stream_enabled ?? true,
    supports_json: capability?.supports_json ?? true,
    supports_tools: capability?.supports_tools ?? false,
    temperature: capability?.temperature ?? true,
    top_p: capability?.top_p ?? false,
    top_k: capability?.top_k ?? false,
    max_tokens: capability?.max_tokens ?? 8192,
    cost_tier: capability?.cost_tier ?? 'standard',
    health: capability?.health ?? (ai?.fallback || ai?.health === 'fallback' ? 'fallback' : 'ok'),
    supported_tasks: capability?.supported_tasks ?? [
      'scenario_generate',
      'community_structure',
      'scenario_reply',
      'interview_feedback',
      'sensitive_check',
    ],
  }
}

function normalizeProviderPool(
  pool: Partial<ProviderPoolStatus> | undefined,
  ai: Partial<AIStatus> | undefined,
): ProviderPoolStatus {
  const fallbackOrder = pool?.fallback_order?.length ? pool.fallback_order : buildFallbackOrder(ai)
  const providers = pool?.providers?.length
    ? pool.providers.map((provider, index) => normalizeProviderPoolProvider(provider, ai, index))
    : buildFallbackProviders(fallbackOrder, ai)

  return {
    active_provider: pool?.active_provider ?? ai?.provider ?? fallbackOrder[0] ?? 'unknown',
    fallback_order: fallbackOrder,
    degraded_count: pool?.degraded_count ?? providers.filter(isProviderDegraded).length,
    providers,
    recent_attempts: ((pool?.recent_attempts ?? ai?.telemetry?.recent_attempts) ?? []).map(normalizeFallbackAttempt),
    updated_at: pool?.updated_at ?? ai?.telemetry?.updated_at ?? new Date().toISOString(),
  }
}

function buildFallbackOrder(ai: Partial<AIStatus> | undefined) {
  const ordered = ['deepseek', 'qwen', 'ernie', 'openai_compatible', 'mock']
  const active = new Set<string>()
  if (ai?.provider) active.add(ai.provider)
  Object.keys(ai?.telemetry?.provider_calls ?? {}).forEach((provider) => active.add(provider))
  if (ai?.fallback) active.add('mock')
  if (active.size === 0) return ordered
  return ordered.filter((provider) => active.has(provider) || provider === 'mock')
}

function buildFallbackProviders(fallbackOrder: string[], ai: Partial<AIStatus> | undefined): ProviderPoolProvider[] {
  return fallbackOrder.map((provider, index) => normalizeProviderPoolProvider({
    ...ai?.capability,
    provider,
    model: provider === ai?.provider ? ai?.model : provider,
    priority: index + 1,
    enabled: provider === ai?.provider || provider === 'mock' || Boolean(ai?.telemetry?.provider_calls?.[provider]),
    call_count: ai?.telemetry?.provider_calls?.[provider] ?? 0,
    status: provider === ai?.provider ? ai?.health : (provider === 'mock' ? 'standby' : 'missing'),
    health: provider === ai?.provider ? ai?.health : (provider === 'mock' ? 'standby' : 'missing'),
    fallback_reason: provider === ai?.provider ? ai?.last_fallback_reason : undefined,
    last_error_type: provider === ai?.provider ? ai?.last_error_type : undefined,
    last_error: provider === ai?.provider ? ai?.last_error : undefined,
    last_checked_at: ai?.telemetry?.updated_at,
  }, ai, index))
}

function normalizeProviderPoolProvider(
  provider: Partial<ProviderPoolProvider>,
  ai: Partial<AIStatus> | undefined,
  index: number,
): ProviderPoolProvider {
  const capability = normalizeCapability(provider, {
    provider: provider.provider ?? ai?.provider,
    model: provider.model ?? ai?.model,
    stream_enabled: provider.supports_streaming ?? ai?.stream_enabled,
    transport: provider.transport ?? ai?.transport,
    fallback: ai?.fallback,
    health: provider.health ?? ai?.health,
  })
  return {
    ...capability,
    ...provider,
    provider: provider.provider ?? capability.provider,
    model: provider.model ?? capability.model,
    status: provider.status ?? provider.health ?? capability.health ?? 'unknown',
    health: provider.health ?? provider.status ?? capability.health ?? 'unknown',
    priority: provider.priority ?? index + 1,
    enabled: provider.enabled ?? true,
    call_count: provider.call_count ?? 0,
    supported_tasks: provider.supported_tasks ?? capability.supported_tasks,
  }
}

function normalizeFallbackAttempt(attempt: Partial<FallbackAttempt>): FallbackAttempt {
  return {
    provider: attempt.provider ?? 'unknown',
    model: attempt.model ?? 'unknown',
    success: attempt.success ?? false,
    error_type: attempt.error_type,
    fallback_reason: attempt.fallback_reason,
    latency_ms: attempt.latency_ms ?? 0,
    started_at: attempt.started_at ?? new Date().toISOString(),
    completed_at: attempt.completed_at,
  }
}

function isProviderDegraded(provider: ProviderPoolProvider) {
  const degradedStates = ['degraded', 'fallback', 'missing', 'disabled', 'error', 'failed']
  return degradedStates.includes(provider.health) || degradedStates.includes(provider.status)
}

function normalizeRouterTelemetry(telemetry: Partial<RouterTelemetry> | undefined): RouterTelemetry {
  const recentDecisions = (telemetry?.recent_decisions ?? []).map((decision) => normalizeRouterDecision(decision))
  return {
    total_calls: telemetry?.total_calls ?? 0,
    successful_calls: telemetry?.successful_calls ?? 0,
    failed_calls: telemetry?.failed_calls ?? 0,
    fallback_calls: telemetry?.fallback_calls ?? 0,
    stream_calls: telemetry?.stream_calls ?? 0,
    json_calls: telemetry?.json_calls ?? 0,
    safety_rewrites: telemetry?.safety_rewrites ?? 0,
    validation_errors: telemetry?.validation_errors ?? 0,
    provider_calls: telemetry?.provider_calls ?? {},
    task_calls: telemetry?.task_calls ?? {},
    recent_attempts: (telemetry?.recent_attempts ?? []).map(normalizeFallbackAttempt),
    recent_decisions: recentDecisions,
    last_decision: telemetry?.last_decision ? normalizeRouterDecision(telemetry.last_decision) : recentDecisions[0],
    last_error: telemetry?.last_error ?? '',
    last_error_type: telemetry?.last_error_type ?? '',
    last_fallback_reason: telemetry?.last_fallback_reason ?? '',
    last_fallback_error: telemetry?.last_fallback_error ?? '',
    last_error_at: telemetry?.last_error_at ?? '',
    updated_at: telemetry?.updated_at ?? new Date().toISOString(),
  }
}

function ensureRouterTelemetry(ai: AIStatus): RouterTelemetry {
  const telemetry = ai.telemetry ?? emptyRouterTelemetry()
  if ((telemetry.recent_decisions ?? []).length > 0) {
    return telemetry
  }
  const generatedAt = telemetry.updated_at || new Date().toISOString()
  const decision = normalizeRouterDecision({
    trace_id: ai.last_trace_id ?? `llm-router-status-${Date.parse(generatedAt) || Date.now()}`,
    task: ai.last_task ?? 'router_status',
    provider: ai.provider,
    model: ai.model,
    output_mode: 'status',
    stream: false,
    safety_policy: 'default',
    fallback_chain: ai.fallback ? [ai.provider, 'mock'] : [ai.provider],
    context: {
      version: ai.router_version ?? 'router-v1',
      strategy: 'status-snapshot',
      original_messages: 0,
      retained_messages: 0,
      summary_retained: false,
      estimated_input_tokens: 0,
      max_input_tokens: ai.capability?.max_tokens ?? 8192,
      compressed: false,
    },
    capability: ai.capability,
    validation: { required: false, status: 'skipped' },
    safety: { policy: 'default', status: 'passed', blocked: false },
    started_at: generatedAt,
    completed_at: generatedAt,
    latency_ms: ai.last_latency_ms ?? 0,
    status: ai.healthy === false ? 'degraded' : 'ok',
    error_type: ai.last_error_type,
    error_message: ai.last_error,
  })
  return {
    ...telemetry,
    total_calls: telemetry.total_calls ?? 0,
    successful_calls: telemetry.successful_calls ?? 0,
    failed_calls: telemetry.failed_calls ?? 0,
    fallback_calls: telemetry.fallback_calls ?? 0,
    provider_calls: telemetry.provider_calls ?? {},
    task_calls: telemetry.task_calls ?? {},
    recent_decisions: [decision],
    last_decision: decision,
    updated_at: generatedAt,
  }
}

function normalizeRouterDecision(decision: Partial<RouterDecision>): RouterDecision {
  return {
    trace_id: decision.trace_id ?? `trace-${Math.random().toString(36).slice(2, 8)}`,
    task: decision.task ?? 'unknown',
    provider: decision.provider ?? 'unknown',
    model: decision.model ?? 'unknown',
    schema: decision.schema,
    prompt: decision.prompt,
    prompt_template: decision.prompt_template,
    output_mode: decision.output_mode ?? 'json',
    stream: decision.stream ?? false,
    safety_policy: decision.safety_policy ?? 'default',
    fallback_chain: decision.fallback_chain ?? [],
    context: normalizeContextWindow(decision.context),
    capability: normalizeCapability(decision.capability, { provider: decision.provider, model: decision.model, stream_enabled: decision.stream }),
    output: normalizeRouterOutput(decision.output),
    validation: normalizeValidationResult(decision.validation, decision.schema),
    safety: normalizeSafetyVerdict(decision.safety, decision.safety_policy),
    started_at: decision.started_at ?? new Date().toISOString(),
    completed_at: decision.completed_at,
    latency_ms: decision.latency_ms ?? 0,
    status: decision.status ?? 'ok',
    error_type: decision.error_type,
    error_message: decision.error_message,
  }
}

function normalizeContextWindow(context: Partial<RouterDecision['context']> | undefined): RouterDecision['context'] {
  return {
    version: context?.version ?? 'router-v1',
    strategy: context?.strategy ?? 'direct',
    original_messages: context?.original_messages ?? 0,
    retained_messages: context?.retained_messages ?? 0,
    summary_retained: context?.summary_retained ?? false,
    key_facts_retained: context?.key_facts_retained ?? [],
    estimated_input_tokens: context?.estimated_input_tokens ?? 0,
    max_input_tokens: context?.max_input_tokens ?? 8192,
    compressed: context?.compressed ?? false,
  }
}

function normalizeRouterOutput(output: Partial<RouterDecision['output']> | undefined): RouterDecision['output'] {
  return {
    parse_status: output?.parse_status ?? 'skipped',
    repair_used: output?.repair_used ?? false,
  }
}

function normalizeValidationResult(validation: Partial<RouterDecision['validation']> | undefined, schema?: string): RouterDecision['validation'] {
  return {
    required: validation?.required ?? Boolean(schema),
    schema: validation?.schema ?? schema,
    status: validation?.status ?? (schema ? 'passed' : 'skipped'),
    detail: validation?.detail,
  }
}

function normalizeSafetyVerdict(safety: Partial<RouterDecision['safety']> | undefined, policy?: string): RouterDecision['safety'] {
  const status = safety?.status ?? 'passed'
  return {
    policy: safety?.policy ?? policy ?? 'default',
    status,
    detail: safety?.detail,
    blocked: safety?.blocked ?? false,
    rewrite_used: safety?.rewrite_used ?? status === 'rewritten',
  }
}

function normalizeSensitiveDetection(
  detection: Partial<NonNullable<SystemStatus['sensitive_detection']>> | undefined,
  ai: Partial<SystemStatus['ai']> | undefined,
): NonNullable<SystemStatus['sensitive_detection']> {
  return {
    status: detection?.status ?? (ai?.fallback ? 'fallback' : 'missing'),
    provider: detection?.provider ?? ai?.provider ?? 'unknown',
    model: detection?.model ?? ai?.model ?? 'unknown',
    fallback_count: detection?.fallback_count ?? 0,
    fallback_used: detection?.fallback_used ?? ai?.fallback ?? false,
    rule_enabled: detection?.rule_enabled ?? true,
    model_enabled: detection?.model_enabled ?? true,
    schema: detection?.schema ?? 'sensitive_check',
    detail: detection?.detail ?? '敏感信息检测状态未上报，前端使用默认展示元数据。',
    checked_actions: detection?.checked_actions ?? [],
  }
}

function normalizeSchemaValidator(validator: NonNullable<SystemStatus['schema_validators']>[number]): NonNullable<SystemStatus['schema_validators']>[number] {
  const schemaName = validator.schema_name ?? validator.name
  const defaults = schemaValidatorDefaults[schemaName] ?? {
    version: 'unknown',
    task: 'AI',
    description: '结构化输出校验',
    target: validator.target,
  }
  return {
    ...validator,
    name: validator.name || schemaName,
    schema_name: schemaName,
    version: validator.version ?? defaults.version,
    task: validator.task ?? defaults.task,
    description: validator.description ?? defaults.description,
    target: validator.target ?? defaults.target,
    status: validator.status ?? 'ok',
  }
}

function normalizeSchemaValidators(validators: NonNullable<SystemStatus['schema_validators']>) {
  const normalized = validators.map(normalizeSchemaValidator)
  const names = new Set(normalized.map((validator) => validator.schema_name ?? validator.name))
  if (!names.has('sensitive_check')) {
    normalized.push(normalizeSchemaValidator({
      name: 'sensitive_check',
      schema_name: 'sensitive_check',
      target: schemaValidatorDefaults.sensitive_check.target,
    }))
  }
  return normalized
}

function normalizeAgentSummary(summary: SystemStatus['agent_summary']): NonNullable<SystemStatus['agent_summary']> {
  return {
    total_recent: summary?.total_recent ?? 0,
    latest_agent: summary?.latest_agent ?? 'diagnostic_agent',
    latest_run_at: summary?.latest_run_at ?? '',
    failed_recent: summary?.failed_recent ?? 0,
    safety_rewritten_recent: summary?.safety_rewritten_recent ?? 0,
    flagged_recent: summary?.flagged_recent ?? 0,
    per_agent: (summary?.per_agent ?? []).map((item) => ({
      agent: item.agent ?? 'unknown_agent',
      total_recent: item.total_recent ?? 0,
      failed_recent: item.failed_recent ?? 0,
      safety_rewritten_recent: item.safety_rewritten_recent ?? 0,
      flagged_recent: item.flagged_recent ?? 0,
      latest_run_at: item.latest_run_at ?? '',
      latest_status: item.latest_status ?? '',
    })),
  }
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

const schemaValidatorDefaults: Record<string, { version: string; task: string; description: string; target: string }> = {
  scenario_question: {
    version: '1.0.0',
    task: 'SC-03',
    description: '情景题生成 JSON Schema',
    target: 'SC-03 情景题完整内容',
  },
  scenario_content_preview: {
    version: '1.0.0',
    task: 'CM-02',
    description: '案例工坊结构化预览 JSON Schema',
    target: 'CM-02 UGC 结构化预览',
  },
  interview_feedback: {
    version: '1.0.0',
    task: 'IV-05',
    description: '面试评估反馈 JSON Schema',
    target: 'IV-05 面试评估反馈',
  },
  scenario_reply: {
    version: '1.0.0',
    task: 'DG-02',
    description: '排查会话回复 JSON Schema',
    target: 'DG-02 排查对话回复',
  },
  sensitive_check: {
    version: '1.0.0',
    task: 'SR-02 AI-03',
    description: '敏感信息检测 JSON Schema',
    target: '案例工坊敏感信息检测结果',
  },
}
