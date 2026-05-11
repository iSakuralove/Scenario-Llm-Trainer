import { useEffect, useState } from 'react'
import { BookOpenCheck, CheckCircle2, Radar, Settings, Sparkles, UserRound } from 'lucide-react'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/authStore'
import type { CommunityPost, UserRole } from '../../types'
import { HeaderBlock, Select } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import './ProfilePage.css'

export function ProfilePage() {
  const token = useToken()
  const user = useAuthStore((state) => state.user)
  const setSession = useAuthStore((state) => state.setSession)
  const refreshToken = useAuthStore((state) => state.refreshToken)
  const savedTargetLevel = user?.profile.target_level ?? 'intermediate'
  const savedPreferredDomains = user?.profile.preferred_domains ?? ['database', 'network', 'os']
  const [targetLevelDraft, setTargetLevelDraft] = useState(savedTargetLevel)
  const [domainTextDraft, setDomainTextDraft] = useState(savedPreferredDomains.join(','))
  const [communityPosts, setCommunityPosts] = useState<CommunityPost[]>([])
  const [message, setMessage] = useState('')
  const [historyError, setHistoryError] = useState('')

  useEffect(() => {
    let ignore = false
    void api.history(token)
      .then((res) => {
        if (ignore) return
        setCommunityPosts(res.community_posts ?? [])
        setHistoryError('')
      })
      .catch((err) => {
        if (ignore) return
        setHistoryError(err instanceof Error ? err.message : '读取案例投稿失败')
      })
    return () => {
      ignore = true
    }
  }, [token])

  async function save() {
    const updated = await api.updateProfile(token, targetLevelDraft, domainTextDraft.split(',').map((item) => item.trim()).filter(Boolean))
    setSession(updated, token, refreshToken)
    setMessage('已保存个人档案')
  }

  const preferredDomains = savedPreferredDomains
  const stats = user?.profile.total_stats ?? {
    scenarios_solved: 0,
    interviews_taken: 0,
    average_score: 0,
    streak_days: 0,
  }
  const profileHighlights = [
    { label: '目标职级', value: targetLevelLabel(savedTargetLevel), detail: '训练难度与画像基线' },
    { label: '偏好专业域', value: preferredDomains.length, detail: preferredDomains.length > 0 ? preferredDomains.map(domainLabel).join(' / ') : '待补充' },
    { label: '案例投稿', value: communityPosts.length, detail: communityPosts.length > 0 ? '已沉淀真实故障样本' : '尚未形成投稿记录' },
    { label: '平均得分', value: stats.average_score || '--', detail: '来自排查与面试训练' },
  ]

  return (
    <section className="page-stack profile-page">
      <HeaderBlock icon={<Settings size={22} />} title="个人档案" description="维护目标职级与偏好专业域，驱动题目推荐和能力画像。" />
      <section className="profile-hero panel">
        <div className="profile-hero-copy">
          <div className="profile-hero-kicker">Profile Workspace</div>
          <h2>让训练方向、目标职级和案例沉淀保持在同一张工作台上。</h2>
          <p>把目标职级、专业偏好和案例沉淀放在同一张档案页里，让训练路径保持清晰、稳定、可延续。</p>
        </div>
        <div className="profile-domain-ribbon">
          <div className="profile-domain-ribbon-head">
            <Radar size={18} />
            <strong>当前关注域</strong>
          </div>
          <div className="profile-domain-chip-list">
            {preferredDomains.length > 0
              ? preferredDomains.map((domain) => <span key={domain}>{domainLabel(domain)}</span>)
              : <span>待补充偏好专业域</span>}
          </div>
        </div>
      </section>
      <div className="metric-row profile-highlight-grid">
        {profileHighlights.map((item) => (
          <div className="metric profile-highlight-card" key={item.label}>
            <span>{item.label}</span>
            <strong>{item.value}</strong>
            <small>{item.detail}</small>
          </div>
        ))}
      </div>
      <div className="two-column profile-main-grid">
        <section className="panel profile-settings-panel">
          <div className="panel-title"><Sparkles size={18} /> 训练偏好设置</div>
          <div className="profile-settings-form">
            <label>目标职级<Select value={targetLevelDraft} onChange={setTargetLevelDraft} options={[
              { value: 'junior', label: '初级' },
              { value: 'intermediate', label: '中级' },
              { value: 'senior', label: '高级' },
              { value: 'architect', label: '架构师' },
            ]} /></label>
            <label>偏好专业域<input value={domainTextDraft} onChange={(event) => setDomainTextDraft(event.target.value)} placeholder="database,network,os" /></label>
            <div className="profile-settings-actions">
              <button className="primary-button compact" onClick={() => void save()}><CheckCircle2 size={16} />保存设置</button>
              {message && <span className="success-line">{message}</span>}
            </div>
          </div>
        </section>
        <section className="panel profile-summary-panel">
          <div className="panel-title"><Radar size={18} /> 当前训练画像</div>
          <div className="profile-summary-list">
            <div className="profile-summary-row">
              <strong>排查训练</strong>
              <span>{stats.scenarios_solved} 次</span>
            </div>
            <div className="profile-summary-row">
              <strong>面试训练</strong>
              <span>{stats.interviews_taken} 次</span>
            </div>
            <div className="profile-summary-row">
              <strong>连续打卡</strong>
              <span>{stats.streak_days} 天</span>
            </div>
            <div className="profile-summary-row">
              <strong>推荐节奏</strong>
              <span>{preferredDomains.length > 0 ? `优先围绕 ${preferredDomains.map(domainLabel).join('、')}` : '请先补充专业域偏好'}</span>
            </div>
          </div>
        </section>
      </div>
      <CommunityPostHistoryPanel posts={communityPosts} error={historyError} />
      {user?.role === 'admin' && <AdminUserPanel token={token} />}
    </section>
  )
}

function CommunityPostHistoryPanel({ posts, error }: { posts: CommunityPost[]; error: string }) {
  return (
    <section className="panel profile-community-panel">
      <div className="panel-title"><BookOpenCheck size={18} /> 我的案例投稿</div>
      {error && <span className="inline-error">{error}</span>}
      {posts.length > 0 ? (
        <div className="profile-community-list">
          {posts.map((post) => (
            <div className="profile-community-row" key={post.id}>
              <div>
                <strong>{post.title}</strong>
                <span>{domainLabel(post.domain)} · {communityStatusLabel(post.status)}{post.converted_question_id ? ` · 已转题 ${post.converted_question_id}` : ''}</span>
              </div>
              <small>{formatDateTime(post.updated_at || post.created_at)}</small>
            </div>
          ))}
        </div>
      ) : (
        <div className="empty-inline">暂无案例投稿记录。</div>
      )}
    </section>
  )
}

function communityStatusLabel(status: string) {
  const labels: Record<string, string> = {
    draft: '草稿',
    pending_review: '待讲师初审',
    instructor_approved: '待管理员终审',
    instructor_rejected: '讲师已驳回',
    final_rejected: '终审已驳回',
    published: '已发布题库',
  }
  return labels[status] ?? status
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function targetLevelLabel(level: string) {
  const labels: Record<string, string> = {
    junior: '初级',
    intermediate: '中级',
    senior: '高级',
    architect: '架构师',
  }
  return labels[level] ?? level
}

function AdminUserPanel({ token }: { token: string }) {
  const [users, setUsers] = useState<Awaited<ReturnType<typeof api.adminUsers>>['list']>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    void api.adminUsers(token).then((res) => setUsers(res.list)).catch((err) => setError(err instanceof Error ? err.message : '读取用户失败'))
  }, [token])

  async function updateRole(userID: string, role: UserRole) {
    setMessage('')
    setError('')
    try {
      const updated = await api.updateUserRole(token, userID, role)
      setUsers((prev) => prev.map((item) => (item.id === updated.id ? updated : item)))
      setMessage(`已更新 ${updated.username} 的角色`)
    } catch (err) {
      setError(err instanceof Error ? err.message : '角色更新失败')
    }
  }

  return (
    <section className="panel admin-user-panel profile-admin-panel">
      <div className="panel-title"><UserRound size={18} /> 用户权限</div>
      <div className="admin-user-list">
        {(users ?? []).map((item) => (
          <div className="admin-user-row" key={item.id}>
            <div>
              <strong>{item.username}</strong>
              <span>{item.email}</span>
            </div>
            <Select
              value={item.role}
              onChange={(role) => void updateRole(item.id, role as UserRole)}
              options={[
                { value: 'student', label: '学员' },
                { value: 'instructor', label: '讲师' },
                { value: 'admin', label: '管理员' },
              ]}
            />
          </div>
        ))}
      </div>
      {message && <span className="success-line">{message}</span>}
      {error && <span className="inline-error">{error}</span>}
    </section>
  )
}
