import { create } from 'zustand'
import { api } from '../api/client'
import type { ScenarioMessage, ScenarioQuestion, ScenarioSession } from '../types'

interface ScenarioStreamingTurn {
  userContent: string
  assistantContent: string
}

interface ScenarioAgentStage {
  step: string
  message: string
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
  streamingTurn: ScenarioStreamingTurn | null
  agentStages: ScenarioAgentStage[]
  completedAgentStages: Record<string, ScenarioAgentStage[]>
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
    streamingTurn: null as ScenarioStreamingTurn | null,
    agentStages: [] as ScenarioAgentStage[],
    completedAgentStages: {} as Record<string, ScenarioAgentStage[]>,
  }
}

export const useScenarioSessionStore = create<ScenarioSessionState>((set) => ({
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
      streamingTurn: { userContent, assistantContent: '' },
      agentStages: [],
    }))

    const stagesForTurn: ScenarioAgentStage[] = []
    try {
      const result = await api.sendScenarioMessageStream(
        token,
        sessionId,
        userContent,
        (chunk) => set((state) => ({
          ...state,
          streamingTurn: state.streamingTurn
            ? { ...state.streamingTurn, assistantContent: state.streamingTurn.assistantContent + chunk }
            : state.streamingTurn,
        })),
        (stage) => {
          if (!stage.message && !stage.step) return
          const nextStage = { step: stage.step, message: stage.message }
          const existingIndex = stagesForTurn.findIndex((item) => item.step === nextStage.step)
          if (existingIndex >= 0) {
            stagesForTurn[existingIndex] = nextStage
          } else {
            stagesForTurn.push(nextStage)
          }
          set((state) => ({
            ...state,
            agentStages: [...state.agentStages.filter((item) => item.step !== stage.step), nextStage],
          }))
        },
      )

      set((state) => ({
        ...state,
        session: result.session,
        messages: [...state.messages, result.message],
        streamingTurn: null,
        agentStages: [],
        isSending: false,
        completedAgentStages: stagesForTurn.length > 0
          ? { ...state.completedAgentStages, [result.message.id]: stagesForTurn }
          : state.completedAgentStages,
      }))
    } catch (err) {
      set((state) => ({
        ...state,
        isSending: false,
        streamingTurn: null,
        agentStages: [],
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
