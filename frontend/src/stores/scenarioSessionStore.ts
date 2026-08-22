import { create } from 'zustand'
import { api, ScenarioStreamReconnectable } from '../api/client'
import type { ScenarioMessageResponse } from '../api/client'
import type { ScenarioMessage, ScenarioQuestion, ScenarioSession } from '../types'
import { SCENARIO_RUN_EVENT_SCHEMA_V2 } from '../types/agentRun'
import type { ScenarioAllowedAction, ScenarioDebugTraceEvent, ScenarioRunEventAny } from '../types/agentRun'

interface ScenarioActiveRun {
  requestId: string
  userContent: string
  events: ScenarioRunEventAny[]
  /** 测试调试流：不进入正式事件或 sessionStorage。 */
  reasoningChunks: string[]
  reasoningStartedAt: number
  /** QuickAction 轮：结构化动作本身是用户输入，正文为空。 */
  structuredAction?: ScenarioAllowedAction
}

interface PersistedScenarioRun extends Omit<ScenarioActiveRun, 'reasoningChunks' | 'reasoningStartedAt'> {
  stateRevision: number
  updatedAt: number
}

const PENDING_RUN_TTL_MS = 30 * 60 * 1000

interface ScenarioSessionState {
  sessionId: string
  question: ScenarioQuestion | null
  session: ScenarioSession | null
  messages: ScenarioMessage[]
  isLoading: boolean
  isSending: boolean
  isQuitting: boolean
  sendError: string
  activeRun: ScenarioActiveRun | null
  completedRuns: Record<string, ScenarioRunEventAny[]>
  completedDebugReasoning: Record<string, string[]>
  completedDebugReasoningDuration: Record<string, number>
  _connectRun: (
    token: string,
    sessionId: string,
    run: ScenarioActiveRun,
    stateRevision: number,
  ) => Promise<ScenarioMessageResponse>
  _applyRunResult: (sessionId: string, requestId: string, result: ScenarioMessageResponse) => void
  hydrate: (token: string, sessionId: string, optimistic?: { question?: ScenarioQuestion | null; session?: ScenarioSession | null }) => Promise<void>
  sendMessage: (token: string, sessionId: string, content: string) => Promise<void>
  sendStructuredAction: (token: string, sessionId: string, action: ScenarioAllowedAction) => Promise<void>
  quit: (token: string, sessionId: string) => Promise<{ status: string; session: ScenarioSession }>
  clear: () => void
}

function emptyState() {
  return {
    sessionId: '',
    question: null,
    session: null,
    messages: [] as ScenarioMessage[],
    isLoading: false,
    isSending: false,
    isQuitting: false,
    sendError: '',
    activeRun: null as ScenarioActiveRun | null,
    completedRuns: {} as Record<string, ScenarioRunEventAny[]>,
    completedDebugReasoning: {} as Record<string, string[]>,
    completedDebugReasoningDuration: {} as Record<string, number>,
  }
}

export const useScenarioSessionStore = create<ScenarioSessionState>((set, get) => {
  let hydrateGeneration = 0

  const isCurrentRun = (sessionId: string, requestId: string) => {
    const state = get()
    return state.sessionId === sessionId && state.activeRun?.requestId === requestId
  }

  const failActiveRun = (
    sessionId: string,
    requestId: string,
    err: unknown,
    fallbackMessage: string,
  ) => {
    // 无论错误来自 turn_failed、网络断流、超时还是 finish 缺失，
    // 都先按 request_id 回收临时重连凭据，避免失败轮刷新后再次自动执行。
    clearPendingRun(sessionId, requestId)
    set((state) => {
      if (state.sessionId !== sessionId || state.activeRun?.requestId !== requestId) return state
      return {
        ...state,
        activeRun: null,
        isSending: false,
        sendError: err instanceof Error ? err.message : fallbackMessage,
      }
    })
  }

  return {
    ...emptyState(),

  // Keep the request identity across a browser refresh. The server owns idempotency;
  // the client only stores public run events needed to resume the visible stream.
  _connectRun: async (token, sessionId, run, stateRevision) => {
    const onRunEvent = (event: ScenarioRunEventAny) => {
      // SSE 过程事件来自可演进的 Agent 旁路。只把满足公开事件最小边界
      // 的帧写入当前回合；坏帧被丢弃，不让它污染重连游标或页面状态。
      if (!isPublicScenarioRunEvent(event)) return
      if (event.request_id !== run.requestId) return
      let nextRun: ScenarioActiveRun | null = null
      set((state) => {
        if (state.sessionId !== sessionId || state.activeRun?.requestId !== run.requestId) return state
        nextRun = {
          ...state.activeRun,
          events: appendRunEvent(state.activeRun.events, event),
        }
        return { ...state, activeRun: nextRun }
      })
      if (nextRun) persistPendingRun(sessionId, nextRun, stateRevision)
    }
    const onDebugTrace = (trace: ScenarioDebugTraceEvent) => {
      if (trace.kind !== 'reasoning_raw_delta' || trace.text === '') return
      set((state) => {
        if (state.sessionId !== sessionId || state.activeRun?.requestId !== run.requestId) return state
        return {
          ...state,
          activeRun: {
            ...state.activeRun,
            reasoningChunks: [...state.activeRun.reasoningChunks, trace.text],
          },
        }
      })
    }

    const basePayload = {
      content: run.userContent,
      requestId: run.requestId,
      stateRevision,
      structuredUserAction: run.structuredAction,
    }
    try {
      return await api.sendScenarioMessageStream(
        token,
        sessionId,
        { ...basePayload, afterSequence: latestSequence(run.events) },
        onRunEvent,
        onDebugTrace,
      )
    } catch (err) {
      if (!(err instanceof ScenarioStreamReconnectable)) throw err
      if (!isCurrentRun(sessionId, run.requestId)) throw err
      const afterSequence = latestSequence(get().activeRun?.events ?? run.events)
      return api.sendScenarioMessageStream(
        token,
        sessionId,
        { ...basePayload, afterSequence },
        onRunEvent,
        onDebugTrace,
      )
    }
  },

  _applyRunResult: (sessionId, requestId, result) => {
    if (!isCurrentRun(sessionId, requestId)) {
      clearPendingRun(sessionId, requestId)
      return
    }
    if (!scenarioResponseBelongsToRun(sessionId, requestId, result)) {
      failActiveRun(
        sessionId,
        requestId,
        new Error('流式响应归属无效'),
        '本轮响应归属无效，请重试',
      )
      return
    }
    clearPendingRun(sessionId, requestId)
    set((state) => {
      if (state.sessionId !== sessionId || state.activeRun?.requestId !== requestId) return state
      const debugReasoning = state.activeRun?.reasoningChunks ?? []
      return {
        ...state,
        session: result.session,
        messages: state.messages.some((message) => message.id === result.message.id)
          ? state.messages
          : [...state.messages, result.message],
        activeRun: null,
        isSending: false,
        completedRuns: {
          ...state.completedRuns,
          [result.message.id]: normalizeRunEvents(
            result.run_events ?? result.message.response_meta.run_events ?? state.activeRun?.events ?? [],
          ),
        },
        completedDebugReasoning: {
          ...state.completedDebugReasoning,
          ...(debugReasoning.length > 0 ? { [result.message.id]: debugReasoning } : {}),
        },
        completedDebugReasoningDuration: {
          ...state.completedDebugReasoningDuration,
          ...(debugReasoning.length > 0
            ? {
                [result.message.id]: Math.max(
                  1,
                  Math.round((Date.now() - (state.activeRun?.reasoningStartedAt ?? Date.now())) / 1000),
                ),
              }
            : {}),
        },
      }
    })
  },

  hydrate: async (token, sessionId, optimistic) => {
    const generation = ++hydrateGeneration
    const hasOptimistic = Boolean(optimistic?.question && optimistic?.session)
    const pendingRun = readPendingRun(sessionId)
    set(() => ({
      ...emptyState(),
      sessionId,
      question: optimistic?.question ?? null,
      session: optimistic?.session ?? null,
      activeRun: pendingRun
        ? {
            requestId: pendingRun.requestId,
            userContent: pendingRun.userContent,
            events: pendingRun.events,
            reasoningChunks: [],
            reasoningStartedAt: Date.now(),
            structuredAction: pendingRun.structuredAction,
          }
        : null,
      isSending: Boolean(pendingRun),
      isLoading: true,
    }))
    try {
      const detail = await api.scenarioSessionDetail(token, sessionId)
      if (generation !== hydrateGeneration || get().sessionId !== sessionId) return
      const committedPendingRun = pendingRun && (detail.messages ?? []).some(
        (message) => message.response_meta.request_id === pendingRun.requestId,
      )
      const stalePendingRun = pendingRun && !committedPendingRun && detail.session.state_revision > pendingRun.stateRevision
      set((state) => {
        if (generation !== hydrateGeneration || state.sessionId !== sessionId) return state
        return {
          ...state,
          sessionId,
          question: detail.session.question_snapshot,
          session: detail.session,
          messages: detail.messages ?? [],
          activeRun: committedPendingRun || stalePendingRun || !pendingRun
            ? null
            : {
                requestId: pendingRun.requestId,
                userContent: pendingRun.userContent,
                events: pendingRun.events,
                reasoningChunks: [],
                reasoningStartedAt: Date.now(),
                structuredAction: pendingRun.structuredAction,
              },
          isSending: Boolean(pendingRun && !committedPendingRun && !stalePendingRun),
          completedRuns: Object.fromEntries(
            (detail.messages ?? [])
              .filter((message) => (message.response_meta.run_events?.length ?? 0) > 0)
              .map((message) => [message.id, normalizeRunEvents(message.response_meta.run_events ?? [])]),
          ),
          isLoading: false,
        }
      })
      if (generation !== hydrateGeneration || get().sessionId !== sessionId) return
      if (committedPendingRun) {
        clearPendingRun(sessionId, pendingRun.requestId)
      } else if (stalePendingRun) {
        clearPendingRun(sessionId, pendingRun.requestId)
      } else if (pendingRun) {
        try {
          const pendingActiveRun: ScenarioActiveRun = {
            ...pendingRun,
            reasoningChunks: [],
            reasoningStartedAt: Date.now(),
          }
          const result = await get()._connectRun(token, sessionId, pendingActiveRun, pendingRun.stateRevision)
          get()._applyRunResult(sessionId, pendingRun.requestId, result)
        } catch (err) {
          failActiveRun(sessionId, pendingRun.requestId, err, '恢复排查消息失败')
        }
      }
    } catch (err) {
      if (pendingRun) failActiveRun(sessionId, pendingRun.requestId, err, '读取排查会话失败')
      set((state) => ({
        ...state,
        ...(state.sessionId === sessionId && generation === hydrateGeneration
          ? {
              isLoading: false,
              sendError: hasOptimistic && !pendingRun
                ? ''
                : (err instanceof Error ? err.message : '读取排查会话失败'),
            }
          : {}),
      }))
      throw err
    }
  },

  sendMessage: async (token, sessionId, content) => {
    const userContent = content.trim()
    if (!userContent) return
    if (get().isSending) return

    set((state) => ({
      ...state,
      isSending: true,
      sendError: '',
      activeRun: {
        requestId: createRequestId(),
        userContent,
        events: [],
        reasoningChunks: [],
        reasoningStartedAt: Date.now(),
      },
    }))

    const activeRun = get().activeRun ?? {
      requestId: createRequestId(),
      userContent,
      events: [],
      reasoningChunks: [],
      reasoningStartedAt: Date.now(),
    }
    const requestId = activeRun.requestId
    const stateRevision = get().session?.state_revision ?? 0
    persistPendingRun(sessionId, activeRun, stateRevision)
    try {
      const result = await get()._connectRun(token, sessionId, activeRun, stateRevision)
      get()._applyRunResult(sessionId, requestId, result)
    } catch (err) {
      failActiveRun(sessionId, requestId, err, '消息发送失败')
      throw err
    }
  },

  // QuickAction 点击：产生 StructuredUserAction 轮，与自然语言共用
  // request_id / state_revision / 幂等与预算通道；正文为空字符串。
  sendStructuredAction: async (token, sessionId, action) => {
    if (get().isSending) return
    set((state) => ({
      ...state,
      isSending: true,
      sendError: '',
      activeRun: {
        requestId: createRequestId(),
        userContent: '',
        events: [],
        reasoningChunks: [],
        reasoningStartedAt: Date.now(),
        structuredAction: action,
      },
    }))
    const activeRun = get().activeRun
    if (!activeRun) return
    const requestId = activeRun.requestId
    const stateRevision = get().session?.state_revision ?? 0
    persistPendingRun(sessionId, activeRun, stateRevision)
    try {
      const result = await get()._connectRun(token, sessionId, activeRun, stateRevision)
      get()._applyRunResult(sessionId, requestId, result)
    } catch (err) {
      failActiveRun(sessionId, requestId, err, '快捷操作失败')
      throw err
    }
  },

  quit: async (token, sessionId) => {
    set((state) => ({ ...state, isQuitting: true, sendError: '' }))
    try {
      const result = await api.quitScenarioSession(token, sessionId)
      clearPendingRun(sessionId)
      set((state) => ({
        ...state,
        isQuitting: false,
        session: result.session,
      }))
      return result
    } catch (err) {
      set((state) => ({
        ...state,
        isQuitting: false,
        sendError: err instanceof Error ? err.message : '放弃会话失败',
      }))
      throw err
    }
  },

  clear: () => {
    hydrateGeneration += 1
    set(emptyState())
  },
  }
})

function createRequestId() {
  return globalThis.crypto?.randomUUID?.() ?? `scenario-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function appendRunEvent(events: ScenarioRunEventAny[], event: ScenarioRunEventAny) {
  return normalizeRunEvents([
    ...events.filter((item) => !(item.request_id === event.request_id && item.sequence === event.sequence)),
    event,
  ])
}

function normalizeRunEvents(events: ScenarioRunEventAny[]) {
  const byKey = new Map<string, ScenarioRunEventAny>()
  for (const event of events) {
    if (!isPublicScenarioRunEvent(event)) continue
    byKey.set(`${event.request_id}:${event.sequence}`, event)
  }
  return [...byKey.values()].sort((left, right) => left.sequence - right.sequence)
}

const PUBLIC_SCENARIO_RUN_EVENT_KINDS = new Set([
  'user_message',
  'reasoning_summary_delta',
  'reasoning_summary_completed',
  'observation_result',
  'tool_started',
  'tool_result',
  'tool_completed',
  'response_summary',
  'mentor_buffered',
  'guard_passed',
  'proposal_approved',
  'reply_delta',
  'turn_started',
  'task_upserted',
  'assistant_delta',
  'clue_published',
  'hint_published',
  'turn_completed',
  'turn_failed',
])

function isPublicScenarioRunEvent(event: unknown): event is ScenarioRunEventAny {
  if (!event || typeof event !== 'object') return false
  const candidate = event as { request_id?: unknown; sequence?: unknown; kind?: unknown; schema_version?: unknown; payload?: unknown }
  const baseValid = typeof candidate.request_id === 'string'
    && candidate.request_id.trim() !== ''
    && typeof candidate.sequence === 'number'
    && Number.isInteger(candidate.sequence)
    && candidate.sequence > 0
    && typeof candidate.kind === 'string'
    && PUBLIC_SCENARIO_RUN_EVENT_KINDS.has(candidate.kind)
  if (!baseValid) return false
  if (candidate.schema_version === undefined) return true
  if (candidate.schema_version !== SCENARIO_RUN_EVENT_SCHEMA_V2) return false
  return isWellFormedV2PublicEvent(candidate.kind, candidate.payload)
}

function isWellFormedV2PublicEvent(kind: unknown, payload: unknown): boolean {
  if (!payload || typeof payload !== 'object') return false
  const value = payload as Record<string, unknown>
  const hasObject = (key: string) => Boolean(value[key] && typeof value[key] === 'object')
  const hasString = (object: unknown, key: string) => (
    Boolean(object && typeof object === 'object' && typeof (object as Record<string, unknown>)[key] === 'string')
  )
  switch (kind) {
    case 'turn_started':
    case 'turn_completed':
    case 'turn_failed':
      return true
    case 'task_upserted':
      return hasObject('task') && hasString(value.task, 'task_id') && hasString(value.task, 'title')
    case 'tool_result': {
      const result = value.tool_result
      return hasString(result, 'call_id') && hasString(result, 'tool_id') && hasString(result, 'tool_kind')
    }
    case 'clue_published':
      return hasObject('clue') && hasString(value.clue, 'clue_id') && hasObjectField(value.clue, 'content')
    case 'hint_published':
      return hasObject('hint') && hasString(value.hint, 'hint_id') && hasObjectField(value.hint, 'content')
    case 'assistant_delta':
      return value.phase === 'understanding' || value.phase === 'replying'
        ? typeof value.markdown_ready_delta === 'string'
        : false
    default:
      return false
  }
}

function hasObjectField(object: unknown, key: string): boolean {
  return Boolean(object && typeof object === 'object' && (object as Record<string, unknown>)[key]
    && typeof (object as Record<string, unknown>)[key] === 'object')
}

function latestSequence(events: ScenarioRunEventAny[]) {
  return events.reduce((latest, event) => Math.max(latest, event.sequence), 0)
}

function scenarioResponseBelongsToRun(
  sessionId: string,
  requestId: string,
  result: ScenarioMessageResponse,
): boolean {
  const candidate = result as ScenarioMessageResponse | null | undefined
  const message = candidate?.message
  const session = candidate?.session
  if (!message || !session || session.id !== sessionId || message.session_id !== sessionId) return false
  const responseRequestId = message.response_meta?.request_id ?? candidate?.response_meta?.request_id
  if (responseRequestId !== requestId) return false
  const events = candidate?.run_events ?? message.response_meta?.run_events ?? []
  return Array.isArray(events) && events.every((event) => (
    isPublicScenarioRunEvent(event) && event.request_id === requestId
  ))
}

function pendingRunStorageKey(sessionId: string) {
  return `hiddenworld:scenario:pending-run:${sessionId}`
}

function readPendingRun(sessionId: string): PersistedScenarioRun | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.sessionStorage.getItem(pendingRunStorageKey(sessionId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<PersistedScenarioRun>
    if (
      typeof parsed.requestId !== 'string'
      || typeof parsed.userContent !== 'string'
      || typeof parsed.stateRevision !== 'number'
      || !Number.isInteger(parsed.stateRevision)
      || parsed.stateRevision < 0
      || typeof parsed.updatedAt !== 'number'
      || !Number.isFinite(parsed.updatedAt)
      || !Array.isArray(parsed.events)
    ) {
      clearPendingRun(sessionId)
      return null
    }
    if (Date.now() - parsed.updatedAt > PENDING_RUN_TTL_MS) {
      clearPendingRun(sessionId)
      return null
    }
    const events = parsed.events.filter((event): event is ScenarioRunEventAny => (
      isPublicScenarioRunEvent(event)
      && event.request_id === parsed.requestId
    ))
    return {
      requestId: parsed.requestId,
      userContent: parsed.userContent,
      events: normalizeRunEvents(events),
      structuredAction: parsed.structuredAction,
      stateRevision: parsed.stateRevision,
      updatedAt: parsed.updatedAt,
    }
  } catch {
    clearPendingRun(sessionId)
    return null
  }
}

function persistPendingRun(sessionId: string, run: ScenarioActiveRun, stateRevision: number) {
  if (typeof window === 'undefined') return
  try {
    const payload: PersistedScenarioRun = {
      requestId: run.requestId,
      userContent: run.userContent,
      events: normalizeRunEvents(run.events),
      structuredAction: run.structuredAction,
      stateRevision,
      updatedAt: Date.now(),
    }
    window.sessionStorage.setItem(pendingRunStorageKey(sessionId), JSON.stringify(payload))
  } catch {
    // Storage can be unavailable in private browsing; in-memory recovery still works.
  }
}

function clearPendingRun(sessionId: string, requestId?: string) {
  if (typeof window === 'undefined') return
  try {
    if (requestId) {
      const raw = window.sessionStorage.getItem(pendingRunStorageKey(sessionId))
      if (raw) {
        const parsed = JSON.parse(raw) as Partial<PersistedScenarioRun>
        if (parsed.requestId && parsed.requestId !== requestId) return
      }
    }
    window.sessionStorage.removeItem(pendingRunStorageKey(sessionId))
  } catch {
    // Ignore storage cleanup failures.
  }
}
