import { create } from 'zustand'
import { api, ScenarioRunFailure } from '../api/client'
import type { ScenarioMessage, ScenarioQuestion, ScenarioSession } from '../types'
import type { ScenarioRunEvent } from '../types/agentRun'

interface ScenarioActiveRun {
  requestId: string
  userContent: string
  events: ScenarioRunEvent[]
}

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
  completedRuns: Record<string, ScenarioRunEvent[]>
  hydrate: (token: string, sessionId: string, optimistic?: { question?: ScenarioQuestion | null; session?: ScenarioSession | null }) => Promise<void>
  sendMessage: (token: string, sessionId: string, content: string) => Promise<void>
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
    completedRuns: {} as Record<string, ScenarioRunEvent[]>,
  }
}

export const useScenarioSessionStore = create<ScenarioSessionState>((set, get) => ({
  ...emptyState(),

  hydrate: async (token, sessionId, optimistic) => {
    const hasOptimistic = Boolean(optimistic?.question && optimistic?.session)
    set(() => ({
      ...emptyState(),
      sessionId,
      question: optimistic?.question ?? null,
      session: optimistic?.session ?? null,
      isLoading: true,
    }))
    try {
      const detail = await api.scenarioSessionDetail(token, sessionId)
      set((state) => ({
        ...state,
        sessionId,
        question: detail.session.question_snapshot,
        session: detail.session,
        messages: detail.messages ?? [],
        completedRuns: Object.fromEntries(
          (detail.messages ?? [])
            .filter((message) => (message.response_meta.run_events?.length ?? 0) > 0)
            .map((message) => [message.id, normalizeRunEvents(message.response_meta.run_events ?? [])]),
        ),
        isLoading: false,
      }))
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
      activeRun: { requestId: createRequestId(), userContent, events: [] },
    }))

    const requestId = get().activeRun?.requestId ?? createRequestId()
    const stateRevision = get().session?.state_revision ?? 0
    const onRunEvent = (event: ScenarioRunEvent) => {
      set((state) => ({
        ...state,
        activeRun: state.activeRun?.requestId === requestId
          ? { ...state.activeRun, events: appendRunEvent(state.activeRun.events, event) }
          : state.activeRun,
      }))
    }
    try {
      let result
      try {
        result = await api.sendScenarioMessageStream(
          token,
          sessionId,
          { content: userContent, requestId, stateRevision },
          onRunEvent,
        )
      } catch (err) {
        if (err instanceof ScenarioRunFailure) throw err
        const afterSequence = latestSequence(get().activeRun?.events ?? [])
        result = await api.sendScenarioMessageStream(
          token,
          sessionId,
          { content: userContent, requestId, stateRevision, afterSequence },
          onRunEvent,
        )
      }

      set((state) => ({
        ...state,
        session: result.session,
        messages: [...state.messages, result.message],
        activeRun: null,
        isSending: false,
        completedRuns: {
          ...state.completedRuns,
          [result.message.id]: normalizeRunEvents(
            result.run_events ?? result.message.response_meta.run_events ?? state.activeRun?.events ?? [],
          ),
        },
      }))
    } catch (err) {
      set((state) => ({
        ...state,
        isSending: false,
        sendError: err instanceof Error ? err.message : '消息发送失败',
      }))
      throw err
    }
  },

  quit: async (token, sessionId) => {
    set((state) => ({ ...state, isQuitting: true, sendError: '' }))
    try {
      const result = await api.quitScenarioSession(token, sessionId)
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

function appendRunEvent(events: ScenarioRunEvent[], event: ScenarioRunEvent) {
  return normalizeRunEvents([
    ...events.filter((item) => !(item.request_id === event.request_id && item.sequence === event.sequence)),
    event,
  ])
}

function normalizeRunEvents(events: ScenarioRunEvent[]) {
  const byKey = new Map<string, ScenarioRunEvent>()
  for (const event of events) byKey.set(`${event.request_id}:${event.sequence}`, event)
  return [...byKey.values()].sort((left, right) => left.sequence - right.sequence)
}

function latestSequence(events: ScenarioRunEvent[]) {
  return events.reduce((latest, event) => Math.max(latest, event.sequence), 0)
}
