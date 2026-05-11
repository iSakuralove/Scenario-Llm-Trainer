import type { UserRole } from '../types'

export function roleLabel(role?: UserRole) {
  if (role === 'admin') return '管理员'
  if (role === 'instructor') return '讲师'
  return '学员'
}

export function systemStatusLabel(status: string) {
  const labels: Record<string, string> = {
    ok: '正常',
    degraded: '需关注',
    fallback: '兜底中',
    disabled: '未启用',
    missing: '缺失',
  }
  return labels[status] ?? status
}
