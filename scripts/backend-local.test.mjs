import assert from 'node:assert/strict'
import { buildBackendEnv, ensurePostgresReady, parseDotEnv } from './backend-local-runtime.mjs'

const env = buildBackendEnv({}, {})

assert.equal(env.STORE_MODE, 'postgres', 'local backend should default to persistent Postgres store')
assert.equal(env.DATABASE_URL, 'postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable', 'local backend should provide a default local Postgres URL')
assert.equal(env.REDIS_URL, '', 'local backend should run without Redis by default')
assert.equal('JWT_SECRET' in env, false, 'local backend should require JWT_SECRET from shell or .env when running persistent mode')

const dotenvEnv = buildBackendEnv({}, { JWT_SECRET: 'dotenv-secret-2026', STORE_MODE: 'memory' })
assert.equal(dotenvEnv.JWT_SECRET, 'dotenv-secret-2026', 'local backend should read JWT_SECRET from root .env')
assert.equal(dotenvEnv.STORE_MODE, 'memory', 'local backend should read STORE_MODE from root .env')

const shellEnv = buildBackendEnv({ JWT_SECRET: 'shell-secret-2026' }, { JWT_SECRET: 'dotenv-secret-2026' })
assert.equal(shellEnv.JWT_SECRET, 'shell-secret-2026', 'shell environment should override .env values')

assert.deepEqual(
  parseDotEnv('JWT_SECRET="quoted-secret-2026"\nSTORE_MODE=memory # local demo\n# ignored\nexport PORT=9090\n'),
  { JWT_SECRET: 'quoted-secret-2026', STORE_MODE: 'memory', PORT: '9090' },
  'dotenv parser should support quoted values, comments, and export prefix',
)

await assert.rejects(
  ensurePostgresReady('postgres://teaching:teaching@127.0.0.1:1/teaching_mvp?sslmode=disable'),
  (error) => {
    assert.match(error.message, /PostgreSQL|数据库|5432/)
    assert.match(error.message, /docker compose up -d postgres|STORE_MODE=memory/)
    return true
  },
  'local backend should fail fast with a helpful message when PostgreSQL is unavailable',
)

console.log('backend-local runtime defaults and PostgreSQL preflight are covered')
