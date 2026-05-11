import assert from 'node:assert/strict'
import { buildBackendEnv, ensurePostgresReady } from './backend-local-runtime.mjs'

const env = buildBackendEnv({})

assert.equal(env.STORE_MODE, 'postgres', 'local backend should default to persistent Postgres store')
assert.equal(env.DATABASE_URL, 'postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable', 'local backend should provide a default local Postgres URL')
assert.equal(env.REDIS_URL, '', 'local backend should run without Redis by default')

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
