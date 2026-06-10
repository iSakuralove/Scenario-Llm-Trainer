import net from 'node:net'
import { readFileSync } from 'node:fs'

const defaultDatabaseURL = 'postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable'

export function buildBackendEnv(sourceEnv = process.env, fileEnv = loadDotEnv()) {
  const mergedEnv = {
    ...fileEnv,
    ...sourceEnv,
  }
  const env = {
    ...mergedEnv,
    PORT: mergedEnv.PORT || '8080',
    STORE_MODE: mergedEnv.STORE_MODE || 'postgres',
    DATABASE_URL: mergedEnv.DATABASE_URL || defaultDatabaseURL,
    REDIS_URL: mergedEnv.REDIS_URL || '',
  }
  if (mergedEnv.JWT_SECRET) {
    env.JWT_SECRET = mergedEnv.JWT_SECRET
  }
  return env
}

export function loadDotEnv(fileURL = new URL('../.env', import.meta.url)) {
  try {
    return parseDotEnv(readFileSync(fileURL, 'utf8'))
  } catch (error) {
    if (error?.code === 'ENOENT') return {}
    throw error
  }
}

export function parseDotEnv(content) {
  const env = {}
  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/)
    if (!match) continue
    let value = match[2].trim()
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1)
    } else {
      value = value.replace(/\s+#.*$/, '').trim()
    }
    env[match[1]] = value
  }
  return env
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
