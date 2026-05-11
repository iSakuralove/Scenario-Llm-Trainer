import { Component } from 'react'
import type { ErrorInfo, ReactNode } from 'react'

export class AppErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('page render failed', error, info.componentStack)
  }

  render() {
    if (!this.state.error) {
      return this.props.children
    }

    return (
      <div className="boot-screen error-fallback">
        <div>
          <strong>页面渲染失败</strong>
          <span>{this.state.error.message || '请返回首页或重新加载。'}</span>
          <div className="card-actions">
            <button className="primary-button compact" type="button" onClick={() => this.setState({ error: null })}>返回当前系统</button>
            <button className="ghost-button compact" type="button" onClick={() => window.location.assign('/')}>回到首页</button>
          </div>
        </div>
      </div>
    )
  }
}
