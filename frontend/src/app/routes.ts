import type { UserRole } from '../types'

const ROLE_DEFAULT_ROUTES: Record<UserRole, string> = {
  student: '/dashboard',
  instructor: '/community',
  admin: '/dashboard',
}

export function getDefaultRouteForRole(role: UserRole | undefined) {
  return role ? ROLE_DEFAULT_ROUTES[role] : '/dashboard'
}

export function isRouteAllowedForRole(pathname: string, role: UserRole | undefined) {
  if (!role) return false
  if (pathname === '/system' || pathname.startsWith('/system/')) {
    return role === 'admin'
  }
  return true
}
