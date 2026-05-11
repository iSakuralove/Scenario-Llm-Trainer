import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Play } from 'lucide-react'
import { api } from '../../api/client'
import { getDefaultRouteForRole, isRouteAllowedForRole } from '../../app/routes'
import { aiModeLabel } from '../../lib/ai'
import { useAIStatusStore } from '../../stores/aiStatusStore'
import { useAuthStore } from '../../stores/authStore'

export function AuthPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const setSession = useAuthStore((state) => state.setSession)
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('demo')
  const [email, setEmail] = useState('demo@example.com')
  const [password, setPassword] = useState('demo123')
  const [error, setError] = useState('')
  const [isSubmitting, setSubmitting] = useState(false)
  const aiStatus = useAIStatusStore((state) => state.status)
  const loadAIStatus = useAIStatusStore((state) => state.load)

  useEffect(() => {
    void loadAIStatus()
  }, [loadAIStatus])

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      const session =
        mode === 'login'
          ? await api.login(username, password)
          : await api.register(username, email, password)
      setSession(session.user, session.access_token, session.refresh_token)
      const defaultRoute = getDefaultRouteForRole(session.user.role)
      const targetPath = isRouteAllowedForRole(location.pathname, session.user.role)
        ? location.pathname
        : defaultRoute
      navigate(targetPath === '/' ? defaultRoute : targetPath, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="auth-layout">
      <section className="auth-intro" aria-label="登录页海报封面">
        <div className="auth-stage-meta">
          <div className="auth-kicker">
            <span>AGENT-DRIVEN</span>
            <span>TRAINING SYSTEM</span>
          </div>
          <div className="auth-edition">
            <span>2026 DESIGN COMPETITION</span>
            <span>SCENARIO / TROUBLESHOOTING / INTERVIEW</span>
          </div>
        </div>
        <div className="auth-accent-line" aria-hidden="true" />

        <div className="auth-hero-copy">
          <h1>
            <span>基于Agent的</span>
            <span>IT技能排障与面</span>
            <span>试情景式训练</span>
            <span>系统</span>
          </h1>
          <p>让排障训练、技术面试与案例沉淀汇聚成一个统一入口，把生成、排查、复盘串成可见、可操作、可演示的训练闭环。</p>
        </div>

        <div className="auth-poster-grid">
          <div className="auth-flow-list" aria-label="教学闭环流程">
            <div className="auth-flow-card">
              <span>STEP 01</span>
              <strong>情景题生成</strong>
              <p>从真实约束生成训练入口，而不是静态题库堆砌。</p>
            </div>
            <div className="auth-flow-card">
              <span>STEP 02</span>
              <strong>渐进式排查</strong>
              <p>通过线索释放与追问，让推理路径可见、可教、可复盘。</p>
            </div>
            <div className="auth-flow-card">
              <span>STEP 03</span>
              <strong>评分复盘</strong>
              <p>将面试表现、案例审核与训练数据沉淀为学习闭环。</p>
            </div>
          </div>

          <div className="auth-workbench-preview auth-collage-preview" aria-label="登录前工作台预览">
            <div className="auth-ribbon auth-ribbon-core">
              <span>CORE</span>
              <strong>渐进式线索释放</strong>
            </div>
            <div className="auth-ribbon auth-ribbon-interaction">
              <span>INTERACTION</span>
              <strong>技术面试追问</strong>
            </div>
            <div className="auth-ribbon auth-ribbon-pipeline">
              <span>PIPELINE</span>
              <strong>UGC 转题库 / AI Router</strong>
            </div>

            <div className="auth-collage-surface" aria-hidden="true">
              <div className="auth-collage-sidebar">
                <div className="auth-collage-logo" />
                <span />
                <span className="active" />
                <span />
                <span />
                <span />
              </div>
              <div className="auth-collage-main">
                <div className="auth-collage-header" />
                <div className="auth-collage-metrics">
                  <span />
                  <span />
                  <span />
                </div>
                <div className="auth-collage-panels">
                  <div className="auth-collage-panel-main" />
                  <div className="auth-collage-panel-stack">
                    <span />
                    <span />
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="auth-runtime">本地演示环境 · {aiModeLabel(aiStatus)}</div>
      </section>

      <form className="auth-panel" onSubmit={submit}>
        <div className="auth-panel-year" aria-hidden="true">
          2026
        </div>
        <div className="auth-panel-head">
          <div className="auth-panel-kicker">LIVE DEMO ACCESS</div>
          <h2>{mode === 'login' ? '登录演示账号' : '创建学员账号'}</h2>
          <p>
            {mode === 'login'
              ? '选择演示账号后进入系统，体验排障训练、技术面试与案例沉淀的完整流程。'
              : '创建新账号后进入系统，继续体验排障训练、技术面试与案例沉淀的完整流程。'}
          </p>
        </div>

        <div className="auth-form-fields">
          <label>
            用户名或邮箱
            <input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} />
          </label>
          {mode === 'register' && (
            <label>
              邮箱
              <input autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} />
            </label>
          )}
          <label>
            密码
            <input
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
        </div>

        {error && <div className="form-error">{error}</div>}

        <button className="primary-button" type="submit" disabled={isSubmitting}>
          <Play size={18} />
          {isSubmitting ? '处理中' : mode === 'login' ? '进入系统' : '注册并进入'}
        </button>
        <button className="ghost-button" type="button" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
          {mode === 'login' ? '需要新账号' : '已有账号登录'}
        </button>
        <div className="auth-panel-demo">
          <strong>演示账号</strong>
          <span>demo / demo123</span>
          <span>instructor / instructor123</span>
          <span>admin / admin123</span>
        </div>
      </form>

    </div>
  )
}
