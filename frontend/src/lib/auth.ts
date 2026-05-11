import { useAuthStore } from '../stores/authStore'

export function useToken() {
  const token = useAuthStore((state) => state.token)
  if (!token) {
    throw new Error('missing auth token')
  }
  return token
}
