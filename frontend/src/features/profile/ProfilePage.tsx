import { useEffect, useRef, useState } from 'react'
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
  const savedTargetLevel = user?.profile.target_level ?? 'intermediate'
  const savedTargetRole = user?.profile.target_role ?? ''
  const savedPreferredDomains = user?.profile.preferred_domains ?? ['database', 'network', 'os']
  const savedResumeSummary = user?.profile.resume_summary ?? ''
  const savedProjectSummary = user?.profile.project_summary ?? ''
  const [targetLevelDraft, setTargetLevelDraft] = useState(savedTargetLevel)
  const [targetRoleDraft, setTargetRoleDraft] = useState(savedTargetRole)
  const [domainTextDraft, setDomainTextDraft] = useState(savedPreferredDomains.join(','))
  const [resumeSummaryDraft, setResumeSummaryDraft] = useState(savedResumeSummary)
  const [projectSummaryDraft, setProjectSummaryDraft] = useState(savedProjectSummary)
  const [newPassword, setNewPassword] = useState('')
  const [isImportingResume, setImportingResume] = useState(false)
  const [communityPosts, setCommunityPosts] = useState<CommunityPost[]>([])
  const [message, setMessage] = useState('')
  const [historyError, setHistoryError] = useState('')
  const resumeFileInputRef = useRef<HTMLInputElement | null>(null)

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
    const updated = await api.updateProfile(token, {
      target_level: targetLevelDraft,
      target_role: targetRoleDraft,
      preferred_domains: domainTextDraft.split(',').map((item) => item.trim()).filter(Boolean),
      resume_summary: resumeSummaryDraft,
      project_summary: projectSummaryDraft,
    })
    const latestAuth = useAuthStore.getState()
    setSession(updated, latestAuth.token, latestAuth.refreshToken)
    setMessage('已保存个人档案')
  }

  async function importResume(file: File) {
    setImportingResume(true)
    setMessage('')
    try {
      const updated = await api.importProfileResume(token, file)
      const latestAuth = useAuthStore.getState()
      setSession(updated, latestAuth.token, latestAuth.refreshToken)
      setResumeSummaryDraft(updated.profile.resume_summary ?? '')
      setMessage('已导入简历文本')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : '导入简历失败')
    } finally {
      setImportingResume(false)
      if (resumeFileInputRef.current) {
        resumeFileInputRef.current.value = ''
      }
    }
  }

  async function changePassword() {
    try {
      const result = await api.updatePassword(token, newPassword)
      setSession(result.user, result.access_token, result.refresh_token)
      setNewPassword('')
      setMessage('密码已更新，请妥善保管新密码')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : '密码更新失败')
    }
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
    { label: '目标岗位', value: savedTargetRole || '待补充', detail: '用于对齐面试准备方向' },
    { label: '偏好专业域', value: preferredDomains.length, detail: preferredDomains.length > 0 ? preferredDomains.map(domainLabel).join(' / ') : '待补充' },
    { label: '案例投稿', value: communityPosts.length, detail: communityPosts.length > 0 ? '已沉淀真实故障样本' : '尚未形成投稿记录' },
  ]

  return (
    <section className="page-stack profile-page">
      <HeaderBlock icon={<Settings size={22} />} title="个人档案" description="维护目标职级与偏好专业域，驱动题目推荐和能力画像。" />
      <section className="profile-hero panel">
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
      <div className="profile-main-grid">
        <section className="panel profile-settings-panel">
          <div className="panel-title"><Sparkles size={18} /> 训练偏好设置</div>
          <div className="profile-settings-form">
            <label>目标职级<Select value={targetLevelDraft} onChange={setTargetLevelDraft} options={[
              { value: 'junior', label: '初级' },
              { value: 'intermediate', label: '中级' },
              { value: 'senior', label: '高级' },
              { value: 'architect', label: '架构师' },
            ]} /></label>
            <label>目标岗位<input value={targetRoleDraft} onChange={(event) => setTargetRoleDraft(event.target.value)} placeholder="例如：后端开发工程师 / 数据库工程师" /></label>
            <label>偏好专业域<input value={domainTextDraft} onChange={(event) => setDomainTextDraft(event.target.value)} placeholder="database,network,os" /></label>
            <div className="profile-import-row" data-testid="profile-resume-import">
              <span>简历导入</span>
              <small>支持 TXT / MD / DOCX / PDF，导入后会覆盖“简历摘要”。</small>
              <input
                ref={resumeFileInputRef}
                type="file"
                accept=".txt,.md,.docx,.pdf"
                onChange={(event) => {
                  const file = event.target.files?.[0]
                  if (file) {
                    void importResume(file)
                  }
                }}
              />
              <button className="ghost-button compact" type="button" disabled={isImportingResume} onClick={() => resumeFileInputRef.current?.click()}>
                {isImportingResume ? '导入中' : '导入简历文本'}
              </button>
            </div>
            <label>
              简历摘要
              <textarea
                value={resumeSummaryDraft}
                onChange={(event) => setResumeSummaryDraft(event.target.value)}
                placeholder="例如：做过 MySQL 慢查询治理、缓存一致性治理和线上故障排查。"
              />
            </label>
            <label>
              项目经历摘要
              <textarea
                value={projectSummaryDraft}
                onChange={(event) => setProjectSummaryDraft(event.target.value)}
                placeholder="例如：负责核心订单链路、性能压测和发布回滚预案。"
              />
            </label>
            <div className="profile-settings-actions">
              <button className="primary-button compact" onClick={() => void save()}><CheckCircle2 size={16} />保存设置</button>
              {message && <span className="success-line">{message}</span>}
            </div>
          </div>
        </section>
        <div className="profile-side-column">
          <section className="panel profile-security-panel">
            <div className="panel-title"><Settings size={18} /> 密码安全</div>
            <p className="profile-security-copy">定期更新密码可以降低账号被盗风险。修改后，其他已登录设备会自动失效。</p>
            <div className="profile-security-form">
              <label>
                <span>新密码</span>
                <input type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} placeholder="至少 6 位" />
              </label>
              <button className="primary-button compact" type="button" disabled={!newPassword.trim()} onClick={() => void changePassword()}>更新密码</button>
            </div>
            <div className="profile-security-note">忘记密码？退出登录后，可在登录页通过注册邮箱发送一次性重置链接。</div>
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
          <CommunityPostHistoryPanel posts={communityPosts} error={historyError} />
          {user?.role === 'admin' && <AdminUserPanel token={token} />}
        </div>
      </div>
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
