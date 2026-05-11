import { spawn } from 'node:child_process'

const isWindows = process.platform === 'win32'
const shell = isWindows ? 'powershell.exe' : undefined
const useDockerBackend = process.argv.slice(2).includes('--docker')
const backendScript = useDockerBackend ? 'dev:backend:docker' : 'dev:backend'

const commands = [
  {
    name: 'backend',
    command: isWindows
      ? ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', `npm run ${backendScript}`]
      : ['npm', ['run', backendScript]],
  },
  {
    name: 'frontend',
    command: isWindows
      ? ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', 'npm run dev:frontend']
      : ['npm', ['run', 'dev:frontend']],
  },
]

const children = []
let shuttingDown = false

function spawnCommand(entry) {
  let child
  if (isWindows) {
    child = spawn(shell, entry.command, {
      cwd: process.cwd(),
      env: process.env,
      stdio: ['inherit', 'pipe', 'pipe'],
    })
  } else {
    child = spawn(entry.command[0], entry.command[1], {
      cwd: process.cwd(),
      env: process.env,
      stdio: ['inherit', 'pipe', 'pipe'],
    })
  }

  child.stdout.on('data', (chunk) => prefix(entry.name, chunk, false))
  child.stderr.on('data', (chunk) => prefix(entry.name, chunk, true))
  child.on('exit', (code) => {
    if (shuttingDown) return
    if (code && code !== 0) {
      console.error(`[${entry.name}] 退出码 ${code}`)
      shutdown(code)
    }
  })
  children.push(child)
}

function prefix(name, chunk, error) {
  const lines = chunk.toString().split(/\r?\n/)
  for (const line of lines) {
    if (!line) continue
    const text = `[${name}] ${line}`
    if (error) console.error(text)
    else console.log(text)
  }
}

function shutdown(code = 0) {
  shuttingDown = true
  for (const child of children) {
    if (!child.killed) child.kill('SIGINT')
  }
  process.exitCode = code
}

process.on('SIGINT', () => shutdown(0))
process.on('SIGTERM', () => shutdown(0))

const backendLabel = useDockerBackend ? 'Docker API' : '本地 Go API'
console.log(`启动后端 ${backendLabel} 与前端 Vite。前端：http://localhost:5173，后端：http://localhost:8080`)
for (const entry of commands) spawnCommand(entry)
