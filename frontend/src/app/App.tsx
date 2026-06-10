import { Suspense, lazy, useEffect } from 'react'
import { BrowserRouter } from 'react-router-dom'
import { useAuthStore } from '../stores/authStore'
import { AppErrorBoundary } from './AppErrorBoundary'
import { AuthPage } from '../features/auth/AuthPage'
import '../App.css'

const AppShell = lazy(() => import('./AppShell').then((module) => ({ default: module.AppShell })))

export function App() {
  const bootstrap = useAuthStore((state) => state.bootstrap)
  const isReady = useAuthStore((state) => state.isReady)
  const token = useAuthStore((state) => state.token)

  useEffect(() => {
    void bootstrap()
  }, [bootstrap])

  if (!isReady) {
    return <div className="boot-screen">正在连接教学系统...</div>
  }

  return (
    <BrowserRouter>
      <AppErrorBoundary>
        {token ? (
          <Suspense fallback={<div className="boot-screen">正在加载工作台...</div>}>
            <AppShell />
          </Suspense>
        ) : <AuthPage />}
      </AppErrorBoundary>
    </BrowserRouter>
  )
}

export default App
