import { spawnSync } from 'node:child_process'

const args = process.argv.slice(2)
const scriptArgs = ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', 'scripts/reset-demo-data.ps1']

if (args.includes('--start-api')) scriptArgs.push('-StartApi')
if (args.includes('--detached')) scriptArgs.push('-Detached')

console.log('重置后端 Docker 演示数据：将删除 PostgreSQL Docker volume，并在下次 API 启动时重建种子数据。')

const result = spawnSync('powershell.exe', scriptArgs, {
  cwd: process.cwd(),
  stdio: 'inherit',
  shell: false,
})

if (result.error) {
  console.error(`执行重置脚本失败：${result.error.message}`)
  process.exit(1)
}

process.exit(result.status ?? 0)
