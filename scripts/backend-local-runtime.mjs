import net from 'node:net'

const defaultDatabaseURL = 'postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable'

export function buildBackendEnv(sourceEnv = process.env) {
  return {
    ...sourceEnv,
    PORT: sourceEnv.PORT || '8080',
    JWT_SECRET: sourceEnv.JWT_SECRET || 'local-dev-secret',
    STORE_MODE: sourceEnv.STORE_MODE || 'postgres',
    DATABASE_URL: sourceEnv.DATABASE_URL || defaultDatabaseURL,
    REDIS_URL: sourceEnv.REDIS_URL || '',
  }
}

export async function ensurePostgresReady(databaseURL, options = {}) {
  const url = new URL(databaseURL)
  const host = url.hostname || 'localhost'
  const port = Number(url.port || '5432')
  const timeoutMs = options.timeoutMs ?? 2000

  await new Promise((resolve, reject) => {
    const socket = net.connect({ host, port })

    const fail = (cause) => {
      socket.destroy()
      reject(buildPostgresPreflightError(databaseURL, host, port, cause))
    }

    socket.setTimeout(timeoutMs)
    socket.once('connect', () => {
      socket.end()
      resolve()
    })
    socket.once('timeout', () => fail(new Error(`connection timed out after ${timeoutMs}ms`)))
    socket.once('error', fail)
  })
}

function buildPostgresPreflightError(databaseURL, host, port, cause) {
  const masked = maskDatabaseURL(databaseURL)
  const message = [
    `数据库预检失败：无法连接到 PostgreSQL ${masked}`,
    `当前地址：${host}:${port}`,
    '请先启动数据库后再运行 `npm run dev:all`：',
    '1. `docker compose up -d postgres`',
    '2. 或显式切换临时模式：`$env:STORE_MODE="memory"; npm run dev:all`',
    '3. 或使用 Docker 后端：`npm run dev:all -- --docker`',
  ].join('\n')

  const error = new Error(message)
  error.cause = cause
  return error
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
