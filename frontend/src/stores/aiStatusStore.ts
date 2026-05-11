import { create } from 'zustand'
import { api } from '../api/client'
import type { AIStatus } from '../types'

interface AIStatusState {
  status: AIStatus | null
  isLoading: boolean
  error: string
  lastLoadedAt: number
  load: (force?: boolean) => Promise<AIStatus | null>
  clear: () => void
}

const AI_STATUS_TTL_MS = 30_000

export const useAIStatusStore = create<AIStatusState>((set, get) => ({
  status: null,
  isLoading: false,
  error: '',
  lastLoadedAt: 0,

  load: async (force = false) => {
    const state = get()
    if (!force && state.status && Date.now() - state.lastLoadedAt < AI_STATUS_TTL_MS) {
      return state.status
    }
    if (state.isLoading) {
      return state.status
    }

    set({ isLoading: true, error: '' })
    try {
      const status = await api.aiStatus()
      set({ status, isLoading: false, error: '', lastLoadedAt: Date.now() })
      return status
    } catch (err) {
      set({
        status: null,
        isLoading: false,
        error: err instanceof Error ? err.message : '读取 AI 状态失败',
        lastLoadedAt: 0,
      })
      return null
    }
  },

  clear: () => set({ status: null, isLoading: false, error: '', lastLoadedAt: 0 }),
}))
