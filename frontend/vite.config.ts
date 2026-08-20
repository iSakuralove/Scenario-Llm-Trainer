import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // 缓存目录移出 node_modules，避免依赖重优化时触发宿主机的批量删除守卫。
  cacheDir: '../tmp/vite-cache',
})
