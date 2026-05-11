import { spawn } from 'node:child_process'
import { buildBackendEnv, ensurePostgresReady } from './backend-local-runtime.mjs'

const args = process.argv.slice(2)
const checkOnly = args.includes('--check')
const isWindows = process.platform === 'win32'
const command = isWindows ? 'go.exe' : 'go'
const goArgs = checkOnly ? ['test', './...'] : ['run', './cmd/server']
const env = buildBackendEnv(process.env)

async function main() {
  if (!checkOnly) {
    console.log(`启动本地后端 API：http://localhost:${env.PORT}，STORE_MODE=${env.STORE_MODE}`)
    if (env.STORE_MODE === 'memory') {
      console.warn('当前为临时内存模式：生成题目、AI 任务和会话会在后端进程退出后丢失。')
    } else {
      console.log(`持久化数据库：${maskDatabaseURL(env.DATABASE_URL)}`)
      await ensurePostgresReady(env.DATABASE_URL)
    }
  }

  const child = spawn(command, goArgs, {
    cwd: 'backend',
    env,
    stdio: 'inherit',
  })

  child.on('exit', (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal)
      return
    }
    process.exit(code ?? 0)
  })
}

function maskDatabaseURL(databaseURL) {
  try {
    const url = new URL(databaseURL)
    if (url.password) url.password = '***'
    return url.toString()
  } catch {
    return databaseURL
  }
}

main().catch((error) => {
  console.error(error?.message || error)
  if (error?.cause) {
    console.error(`原因：${error.cause.message || error.cause}`)
  }
  process.exit(1)
})
