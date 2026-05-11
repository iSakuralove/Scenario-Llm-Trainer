import { useEffect } from 'react'
import { Link, NavLink, Route, Routes, useNavigate } from 'react-router-dom'
import {
  BookOpenCheck,
  BrainCircuit,
  Gauge,
  LogOut,
  MessageSquareText,
  PanelLeftClose,
  PanelLeftOpen,
  Settings,
  UserRound,
} from 'lucide-react'
import { useAuthStore } from '../stores/authStore'
import { useAIStatusStore } from '../stores/aiStatusStore'
import { useLayoutStore } from '../stores/layoutStore'
import { RoleGuard } from './RoleGuard'
import { DashboardPage } from '../features/learning/DashboardPage'
import { ScenariosPage } from '../features/scenarios/ScenariosPage'
import { ScenarioSessionPage } from '../features/scenarios/ScenarioSessionPage'
import { ScenarioReviewPage } from '../features/scenarios/ScenarioReviewPage'
import { InterviewsPage } from '../features/interviews/InterviewsPage'
import { InterviewSessionRoute } from '../features/interviews/InterviewSessionRoute'
import { InterviewReportPage } from '../features/interviews/InterviewReportPage'
import { CommunityPage } from '../features/community/CommunityPage'
import { ProfilePage } from '../features/profile/ProfilePage'
import { SystemPage } from '../features/system/SystemPage'
import { roleLabel } from '../lib/labels'
import { aiModeLabel } from '../lib/ai'

export function AppShell() {
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.user)
  const logout = useAuthStore((state) => state.logout)
  const aiStatus = useAIStatusStore((state) => state.status)
  const loadAIStatus = useAIStatusStore((state) => state.load)
  const isSidebarCollapsed = useLayoutStore((state) => state.isSidebarCollapsed)
  const setSidebarCollapsed = useLayoutStore((state) => state.setSidebarCollapsed)

  useEffect(() => {
    void loadAIStatus()
  }, [loadAIStatus])

  function handleLogout() {
    logout()
    navigate('/', { replace: true })
  }

  return (
    <div className={`app-shell ${isSidebarCollapsed ? 'sidebar-collapsed' : ''}`}>
      {isSidebarCollapsed && (
        <button
          className="sidebar-restore-button"
          type="button"
          onClick={() => setSidebarCollapsed(false)}
          aria-label="显示全局导航"
          title="显示全局导航"
        >
          <PanelLeftOpen size={18} />
        </button>
      )}
      <aside className="sidebar" data-testid="global-sidebar" hidden={isSidebarCollapsed}>
        <div className="sidebar-brand-row">
          <Link className="brand" to="/dashboard">
            <span className="brand-mark">AI</span>
            <span>
              <strong>情景式教学系统</strong>
              <small>比赛 MVP</small>
            </span>
          </Link>
          <button
            className="icon-button compact-icon sidebar-collapse-button"
            type="button"
            onClick={() => setSidebarCollapsed(true)}
            aria-label="隐藏全局导航"
            title="隐藏全局导航"
          >
            <PanelLeftClose size={18} />
          </button>
        </div>
        <nav className="nav-list">
          <NavLink to="/dashboard"><Gauge size={18} />仪表盘</NavLink>
          <NavLink to="/scenarios"><BrainCircuit size={18} />排查工坊</NavLink>
          <NavLink to="/interviews"><MessageSquareText size={18} />面试舱</NavLink>
          <NavLink to="/community"><BookOpenCheck size={18} />案例工坊</NavLink>
          <NavLink to="/profile"><UserRound size={18} />个人档案</NavLink>
          {user?.role === 'admin' && <NavLink to="/system"><Settings size={18} />系统状态</NavLink>}
        </nav>
        <div className="sidebar-status-card">
          <span className="sidebar-status-label">当前模式</span>
          <strong>{aiModeLabel(aiStatus)}</strong>
          <small>围绕排障训练、技术面试与案例沉淀组织演示路径。</small>
        </div>
        <div className="user-strip">
          <div>
            <strong>{user?.username}</strong>
            <small>{user?.role === 'student' ? '用户' : roleLabel(user?.role)}</small>
          </div>
          <button className="icon-button" type="button" onClick={handleLogout} title="退出登录">
            <LogOut size={18} />
          </button>
        </div>
      </aside>
      <main className="workspace">
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/scenarios/session/:id" element={<ScenarioSessionPage />} />
          <Route path="/scenarios/session/:id/review" element={<ScenarioReviewPage />} />
          <Route path="/interviews" element={<InterviewsPage />} />
          <Route path="/interviews/session/:id" element={<InterviewSessionRoute />} />
          <Route path="/interviews/session/:id/report" element={<InterviewReportPage />} />
          <Route path="/community" element={<CommunityPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="/system" element={<RoleGuard allow={['admin']}><SystemPage /></RoleGuard>} />
        </Routes>
      </main>
    </div>
  )
}
