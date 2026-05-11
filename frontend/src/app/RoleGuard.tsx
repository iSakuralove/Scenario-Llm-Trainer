import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useAuthStore } from '../stores/authStore'
import type { UserRole } from '../types'
import { getDefaultRouteForRole } from './routes'

interface RoleGuardProps {
  allow: UserRole[]
  children: ReactNode
}

export function RoleGuard({ allow, children }: RoleGuardProps) {
  const user = useAuthStore((state) => state.user)
  const location = useLocation()

  if (!user || !allow.includes(user.role)) {
    return <Navigate to={getDefaultRouteForRole(user?.role)} replace state={{ from: location.pathname }} />
  }

  return children
}
