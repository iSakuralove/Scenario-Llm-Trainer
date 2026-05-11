import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const script = readFileSync(new URL('./dev-all.mjs', import.meta.url), 'utf8')

assert.match(script, /includes\(['"]--docker['"]\)/, 'Docker backend mode should require an explicit --docker flag')
assert.match(
  script,
  /backendScript\s*=\s*useDockerBackend\s*\?\s*['"]dev:backend:docker['"]\s*:\s*['"]dev:backend['"]/,
  'dev:all should start the local Go backend by default',
)
assert.doesNotMatch(
  script,
  /name:\s*['"]backend['"][\s\S]*?dev:backend:docker/,
  'default backend command must not require Docker',
)
assert.match(script, /--docker/, 'dev:all should keep an explicit Docker backend mode')

console.log('dev-all script defaults to local Go backend')
