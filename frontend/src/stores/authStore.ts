import { create } from 'zustand'
import { api } from '../api/client'
import type { User } from '../types'

interface AuthState {
  user: User | null
  token: string
  refreshToken: string
  isReady: boolean
  setSession: (user: User, token: string, refreshToken: string) => void
  bootstrap: () => Promise<void>
  logout: () => void
}

const ACCESS_KEY = 'teaching_mvp_access'
const REFRESH_KEY = 'teaching_mvp_refresh'

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  token: localStorage.getItem(ACCESS_KEY) ?? '',
  refreshToken: localStorage.getItem(REFRESH_KEY) ?? '',
  isReady: false,

  setSession: (user, token, refreshToken) => {
    localStorage.setItem(ACCESS_KEY, token)
    localStorage.setItem(REFRESH_KEY, refreshToken)
    set({ user, token, refreshToken, isReady: true })
  },

  bootstrap: async () => {
    const { token, refreshToken, setSession, logout } = get()
    if (!token) {
      set({ isReady: true })
      return
    }
    try {
      const user = await api.me(token)
      set({ user, token, refreshToken, isReady: true })
    } catch {
      if (!refreshToken) {
        logout()
        return
      }
      try {
        const session = await api.refresh(refreshToken)
        setSession(session.user, session.access_token, session.refresh_token)
      } catch {
        logout()
      }
    }
  },

  logout: () => {
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
    set({ user: null, token: '', refreshToken: '', isReady: true })
  },
}))
