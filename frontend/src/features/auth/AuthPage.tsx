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
  const logout = useAuthStore((state) => state.logout)
  const resetToken = new URLSearchParams(location.search).get('token') || ''
  const isResetRoute = location.pathname === '/reset-password'
  const [mode, setMode] = useState<'login' | 'register' | 'forgot' | 'reset'>(isResetRoute || resetToken ? 'reset' : 'login')
  const [username, setUsername] = useState('demo')
  const [email, setEmail] = useState('demo@example.com')
  const [password, setPassword] = useState(isResetRoute || resetToken ? '' : 'demo123')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [feedback, setFeedback] = useState<{ tone: 'error' | 'success'; message: string } | null>(null)
  const [isSubmitting, setSubmitting] = useState(false)
  // 进入重置页时先向后端校验链接是否仍然有效（10 分钟过期 / 一次性失效），
  // 避免用户填完新密码才发现链接已失效。初始态用惰性初始化：有 token 时进入
  // 校验中，缺 token 时直接判定无效，避免在 effect 里同步 setState。
  const [tokenStatus, setTokenStatus] = useState<'idle' | 'checking' | 'valid' | 'invalid'>(() => {
    if (!(isResetRoute || resetToken)) return 'idle'
    return resetToken ? 'checking' : 'invalid'
  })
  const [tokenError, setTokenError] = useState(() =>
    (isResetRoute || resetToken) && !resetToken ? '重置链接缺少安全令牌，请返回登录页重新发送邮件。' : '',
  )
  const aiStatus = useAIStatusStore((state) => state.status)
  const loadAIStatus = useAIStatusStore((state) => state.load)

  useEffect(() => {
    void loadAIStatus()
  }, [loadAIStatus])

  // 仅在重置模式且带 token 时向后端异步核验；核验结果只在回调里落地。
  useEffect(() => {
    if (mode !== 'reset' || !resetToken) return
    let cancelled = false
    api
      .verifyPasswordReset(resetToken)
      .then(() => {
        if (!cancelled) setTokenStatus('valid')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setTokenStatus('invalid')
        setTokenError(err instanceof Error ? err.message : '重置链接无效或已过期')
      })
    return () => {
      cancelled = true
    }
  }, [mode, resetToken])

  async function submit(event: FormEvent) {
    event.preventDefault()
    setFeedback(null)
    if (mode === 'reset') {
      if (!resetToken) {
        setFeedback({ tone: 'error', message: '重置链接缺少安全令牌，请返回登录页重新发送邮件。' })
        return
      }
      if (password.length < 6) {
        setFeedback({ tone: 'error', message: '密码至少需要 6 位。' })
        return
      }
      if (password !== confirmPassword) {
        setFeedback({ tone: 'error', message: '两次输入的密码不一致。' })
        return
      }
    }
    setSubmitting(true)
    try {
      if (mode === 'forgot') {
        await api.requestPasswordReset(email || username)
        setFeedback({ tone: 'success', message: '如果该邮箱已注册，重置邮件会发送到你的邮箱。' })
        return
      }
      if (mode === 'reset') {
        await api.confirmPasswordReset(resetToken, password)
        logout()
        setMode('login')
        setUsername('')
        setPassword('')
        setConfirmPassword('')
        setFeedback({ tone: 'success', message: '密码已重置，请使用新密码登录。' })
        navigate('/', { replace: true })
        return
      }
      const session = mode === 'login' ? await api.login(username, password) : await api.register(username, email, password)
      setSession(session.user, session.access_token, session.refresh_token)
      const defaultRoute = getDefaultRouteForRole(session.user.role)
      const targetPath = isRouteAllowedForRole(location.pathname, session.user.role)
        ? location.pathname
        : defaultRoute
      navigate(targetPath === '/' ? defaultRoute : targetPath, { replace: true })
    } catch (err) {
      setFeedback({ tone: 'error', message: err instanceof Error ? err.message : '登录失败' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={`auth-layout ${mode === 'reset' ? 'auth-layout-reset' : ''}`}>
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

      <form className="auth-panel" onSubmit={submit} aria-labelledby="auth-panel-title">
        {mode === 'reset' && (
          <div className="auth-reset-brand" aria-label="情景式教学系统密码安全中心">
            <span className="auth-reset-brand-mark" aria-hidden="true">AI</span>
            <span>
              <strong>情景式教学系统</strong>
              <small>密码安全中心</small>
            </span>
          </div>
        )}
        <div className="auth-panel-year" aria-hidden="true">
          2026
        </div>
        <div className="auth-panel-head">
          <div className="auth-panel-kicker">LIVE DEMO ACCESS</div>
          <h2 id="auth-panel-title">{mode === 'login' ? '登录演示账号' : mode === 'register' ? '创建学员账号' : mode === 'forgot' ? '找回密码' : '设置新密码'}</h2>
          <p>
            {mode === 'login'
              ? '选择演示账号后进入系统，体验排障训练、技术面试与案例沉淀的完整流程。'
              : mode === 'register' ? '创建新账号后进入系统，继续体验排障训练、技术面试与案例沉淀的完整流程。' : mode === 'forgot' ? '输入注册邮箱，我们会发送一次性密码重置链接。' : '请输入至少 6 位的新密码。'}
          </p>
        </div>

        {mode === 'reset' && tokenStatus === 'checking' && (
          <div className="form-feedback" role="status" aria-live="polite">
            正在校验重置链接…
          </div>
        )}

        {mode === 'reset' && tokenStatus === 'invalid' && (
          <div className="auth-reset-invalid">
            <div className="form-feedback error" role="alert" aria-live="polite">
              {tokenError || '重置链接无效或已过期，请返回登录页重新发送邮件。'}
            </div>
            <button className="primary-button" type="button" onClick={() => { setMode('forgot'); setFeedback(null) }}>
              <Play size={18} />
              重新发送重置邮件
            </button>
            <button className="ghost-button" type="button" onClick={() => { setMode('login'); navigate('/', { replace: true }) }}>
              返回登录
            </button>
          </div>
        )}

        {!(mode === 'reset' && tokenStatus !== 'valid') && (
        <div className="auth-form-fields">
          {mode !== 'reset' && (
            <label>
              {mode === 'forgot' ? '注册邮箱' : '用户名或邮箱'}
              <input
                autoComplete={mode === 'forgot' ? 'email' : 'username'}
                type={mode === 'forgot' ? 'email' : 'text'}
                value={mode === 'forgot' ? email : username}
                required
                onChange={(event) => mode === 'forgot' ? setEmail(event.target.value) : setUsername(event.target.value)}
              />
            </label>
          )}
          {mode === 'register' && (
            <label>
              邮箱
              <input autoComplete="email" type="email" value={email} required onChange={(event) => setEmail(event.target.value)} />
            </label>
          )}
          {mode !== 'forgot' && <label>
            {mode === 'reset' ? '新密码' : '密码'}
            <input
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              minLength={mode === 'login' ? undefined : 6}
              type="password"
              value={password}
              required
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>}
          {mode === 'reset' && (
            <label>
              确认新密码
              <input
                autoComplete="new-password"
                minLength={6}
                type="password"
                value={confirmPassword}
                required
                onChange={(event) => setConfirmPassword(event.target.value)}
              />
            </label>
          )}
        </div>
        )}

        {feedback && (
          <div className={`form-feedback ${feedback.tone}`} role={feedback.tone === 'error' ? 'alert' : 'status'} aria-live="polite">
            {feedback.message}
          </div>
        )}

        {!(mode === 'reset' && tokenStatus !== 'valid') && (
        <button className="primary-button" type="submit" disabled={isSubmitting}>
          <Play size={18} />
          {isSubmitting ? '处理中' : mode === 'login' ? '进入系统' : mode === 'register' ? '注册并进入' : mode === 'forgot' ? '发送重置邮件' : '确认新密码'}
        </button>
        )}
        {mode === 'login' && <button className="ghost-button" type="button" onClick={() => setMode('forgot')}>忘记密码</button>}
        {mode !== 'reset' && <button className="ghost-button" type="button" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>{mode === 'login' ? '需要新账号' : '已有账号登录'}</button>}
        {mode === 'reset' && <button className="ghost-button" type="button" onClick={() => {
          logout()
          setMode('login')
          setPassword('')
          setConfirmPassword('')
          setFeedback(null)
          navigate('/', { replace: true })
        }}>返回登录页</button>}
        {mode === 'login' && <div className="auth-panel-demo">
          <strong>演示账号</strong>
          <span>demo / demo123</span>
          <span>instructor / instructor123</span>
          <span>admin / admin123</span>
        </div>}
      </form>

    </div>
  )
}
