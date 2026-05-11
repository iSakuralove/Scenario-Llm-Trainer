import { create } from 'zustand'

const SIDEBAR_COLLAPSED_KEY = 'teaching_mvp_sidebar_collapsed'

interface LayoutState {
  isSidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  toggleSidebarCollapsed: () => void
  reset: () => void
}

function readSidebarCollapsed() {
  return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
}

function persistSidebarCollapsed(collapsed: boolean) {
  window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? '1' : '0')
}

export const useLayoutStore = create<LayoutState>((set) => ({
  isSidebarCollapsed: readSidebarCollapsed(),

  setSidebarCollapsed: (collapsed) => {
    persistSidebarCollapsed(collapsed)
    set({ isSidebarCollapsed: collapsed })
  },

  toggleSidebarCollapsed: () => set((state) => {
    const next = !state.isSidebarCollapsed
    persistSidebarCollapsed(next)
    return { isSidebarCollapsed: next }
  }),

  reset: () => {
    persistSidebarCollapsed(false)
    set({ isSidebarCollapsed: false })
  },
}))
