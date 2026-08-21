import { create } from 'zustand'
import { api, ScenarioRunFailure } from '../api/client'
import type { ScenarioMessageResponse } from '../api/client'
import type { ScenarioMessage, ScenarioQuestion, ScenarioSession } from '../types'
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

export const useScenarioSessionStore = create<ScenarioSessionState>((set, get) => ({
  ...emptyState(),

  // Keep the request identity across a browser refresh. The server owns idempotency;
  // the client only stores public run events needed to resume the visible stream.
  _connectRun: async (token, sessionId, run, stateRevision) => {
    const onRunEvent = (event: ScenarioRunEventAny) => {
      let nextRun: ScenarioActiveRun | null = null
      set((state) => {
        if (state.activeRun?.requestId !== run.requestId) return state
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
        if (state.activeRun?.requestId !== run.requestId) return state
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
      if (err instanceof ScenarioRunFailure) throw err
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
    clearPendingRun(sessionId, requestId)
    set((state) => {
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
      const committedPendingRun = pendingRun && (detail.messages ?? []).some(
        (message) => message.response_meta.request_id === pendingRun.requestId,
      )
      const stalePendingRun = pendingRun && !committedPendingRun && detail.session.state_revision > pendingRun.stateRevision
      set((state) => ({
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
      }))
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
          if (err instanceof ScenarioRunFailure) clearPendingRun(sessionId, pendingRun.requestId)
          set((state) => ({
            ...state,
            isSending: false,
            sendError: err instanceof Error ? err.message : '恢复排查消息失败',
          }))
        }
      }
    } catch (err) {
      set((state) => ({
        ...state,
        isLoading: false,
        sendError: hasOptimistic ? '' : (err instanceof Error ? err.message : '读取排查会话失败'),
      }))
      throw err
    }
  },

  sendMessage: async (token, sessionId, content) => {
    const userContent = content.trim()
    if (!userContent) return

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
      if (err instanceof ScenarioRunFailure) clearPendingRun(sessionId, requestId)
      set((state) => ({
        ...state,
        isSending: false,
        sendError: err instanceof Error ? err.message : '消息发送失败',
      }))
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
      if (err instanceof ScenarioRunFailure) clearPendingRun(sessionId, requestId)
      set((state) => ({
        ...state,
        isSending: false,
        sendError: err instanceof Error ? err.message : '快捷操作失败',
      }))
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

  clear: () => set(emptyState()),
}))

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
  for (const event of events) byKey.set(`${event.request_id}:${event.sequence}`, event)
  return [...byKey.values()].sort((left, right) => left.sequence - right.sequence)
}

function latestSequence(events: ScenarioRunEventAny[]) {
  return events.reduce((latest, event) => Math.max(latest, event.sequence), 0)
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
      || typeof parsed.updatedAt !== 'number'
      || !Array.isArray(parsed.events)
    ) {
      clearPendingRun(sessionId)
      return null
    }
    if (Date.now() - parsed.updatedAt > PENDING_RUN_TTL_MS) {
      clearPendingRun(sessionId)
      return null
    }
    return {
      requestId: parsed.requestId,
      userContent: parsed.userContent,
      events: normalizeRunEvents(parsed.events as ScenarioRunEventAny[]),
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
