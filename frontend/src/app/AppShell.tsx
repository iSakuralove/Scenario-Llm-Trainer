import { Suspense, lazy, useEffect } from 'react'
import { Link, NavLink, Route, Routes, useNavigate } from 'react-router-dom'
import {
  BookOpenCheck,
  Bot,
  BrainCircuit,
  Database,
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
import { Loading } from '../components/common'
import { DashboardPage } from '../features/learning/DashboardPage'
import { RoleGuard } from './RoleGuard'
import { roleLabel } from '../lib/labels'
import { aiModeLabel } from '../lib/ai'

const ScenariosPage = lazy(() => import('../features/scenarios/ScenariosPage').then((module) => ({ default: module.ScenariosPage })))
const ScenarioSessionPage = lazy(() => import('../features/scenarios/ScenarioSessionPage').then((module) => ({ default: module.ScenarioSessionPage })))
const ScenarioReviewPage = lazy(() => import('../features/scenarios/ScenarioReviewPage').then((module) => ({ default: module.ScenarioReviewPage })))
const InterviewsPage = lazy(() => import('../features/interviews/InterviewsPage').then((module) => ({ default: module.InterviewsPage })))
const InterviewSessionRoute = lazy(() => import('../features/interviews/InterviewSessionRoute').then((module) => ({ default: module.InterviewSessionRoute })))
const InterviewReportPage = lazy(() => import('../features/interviews/InterviewReportPage').then((module) => ({ default: module.InterviewReportPage })))
const CommunityPage = lazy(() => import('../features/community/CommunityPage').then((module) => ({ default: module.CommunityPage })))
const MentorPage = lazy(() => import('../features/mentor/MentorPage').then((module) => ({ default: module.MentorPage })))
const ProfilePage = lazy(() => import('../features/profile/ProfilePage').then((module) => ({ default: module.ProfilePage })))
const SystemPage = lazy(() => import('../features/system/SystemPage').then((module) => ({ default: module.SystemPage })))
const InterviewBankAdminPage = lazy(() => import('../features/interviewBank/InterviewBankAdminPage').then((module) => ({ default: module.InterviewBankAdminPage })))

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
          <NavLink to="/mentor"><Bot size={18} />AI Mentor</NavLink>
          <NavLink to="/community"><BookOpenCheck size={18} />案例工坊</NavLink>
          <NavLink to="/profile"><UserRound size={18} />个人档案</NavLink>
          {user?.role === 'admin' && <NavLink to="/interview-bank"><Database size={18} />面试题库</NavLink>}
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
        <Suspense fallback={<Loading title="加载页面" />}>
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/scenarios" element={<ScenariosPage />} />
            <Route path="/scenarios/session/:id" element={<ScenarioSessionPage />} />
            <Route path="/scenarios/session/:id/review" element={<ScenarioReviewPage />} />
            <Route path="/interviews" element={<InterviewsPage />} />
            <Route path="/mentor" element={<MentorPage />} />
            <Route path="/interviews/session/:id" element={<InterviewSessionRoute />} />
            <Route path="/interviews/session/:id/report" element={<InterviewReportPage />} />
            <Route path="/community" element={<CommunityPage />} />
            <Route path="/profile" element={<ProfilePage />} />
            <Route path="/interview-bank" element={<RoleGuard allow={['admin']}><InterviewBankAdminPage /></RoleGuard>} />
            <Route path="/system" element={<RoleGuard allow={['admin']}><SystemPage /></RoleGuard>} />
          </Routes>
        </Suspense>
      </main>
    </div>
  )
}
