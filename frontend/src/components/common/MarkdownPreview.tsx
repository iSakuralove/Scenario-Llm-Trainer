import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MermaidRenderer } from './MermaidRenderer'

export function MarkdownPreview({ content, emptyText = '预览会显示在这里。' }: { content: string; emptyText?: string }) {
  const text = content.trim()

  if (!text) {
    return <div className="markdown-preview empty">{emptyText}</div>
  }

  return (
    <div className="markdown-preview">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          pre({ children }) {
            return <>{children}</>
          },
          code({ className, children, ...props }) {
            const language = /language-(\w+)/.exec(className || '')?.[1]
            const code = String(children).replace(/\n$/, '')
            if (language === 'mermaid') {
              return <MermaidRenderer code={code} />
            }
            if (language) {
              return <CodeBlock code={code} language={language} />
            }
            return <code className={className} {...props}>{children}</code>
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

function CodeBlock({ code, language }: { code: string; language: string }) {
  return (
    <div className="code-window">
      <div className="code-window-header">
        <span className="window-dot red" />
        <span className="window-dot yellow" />
        <span className="window-dot green" />
        <strong>{languageLabel(language)}</strong>
      </div>
      <pre>
        <code className={`language-${language}`}>
          {highlightCode(code, language)}
        </code>
      </pre>
    </div>
  )
}

const keywordMap: Record<string, Set<string>> = {
  python: new Set([
    'and', 'as', 'assert', 'async', 'await', 'break', 'class', 'continue', 'def', 'elif', 'else', 'except', 'False',
    'finally', 'for', 'from', 'global', 'if', 'import', 'in', 'is', 'lambda', 'None', 'nonlocal', 'not', 'or', 'pass',
    'raise', 'return', 'True', 'try', 'while', 'with', 'yield',
  ]),
  java: new Set([
    'abstract', 'boolean', 'break', 'case', 'catch', 'class', 'continue', 'else', 'extends', 'final', 'finally', 'for',
    'if', 'implements', 'import', 'instanceof', 'interface', 'new', 'private', 'protected', 'public', 'return', 'static',
    'super', 'switch', 'this', 'throw', 'throws', 'try', 'void', 'while',
  ]),
  javascript: new Set([
    'async', 'await', 'break', 'case', 'catch', 'class', 'const', 'continue', 'default', 'else', 'export', 'extends',
    'finally', 'for', 'from', 'function', 'if', 'import', 'let', 'new', 'return', 'switch', 'this', 'throw', 'try',
    'var', 'while',
  ]),
  typescript: new Set([
    'async', 'await', 'break', 'case', 'catch', 'class', 'const', 'continue', 'default', 'else', 'export', 'extends',
    'finally', 'for', 'from', 'function', 'if', 'implements', 'import', 'interface', 'let', 'new', 'private', 'public',
    'readonly', 'return', 'type', 'var', 'while',
  ]),
  go: new Set([
    'break', 'case', 'chan', 'const', 'continue', 'defer', 'else', 'fallthrough', 'for', 'func', 'go', 'goto', 'if',
    'import', 'interface', 'map', 'package', 'range', 'return', 'select', 'struct', 'switch', 'type', 'var',
  ]),
  sql: new Set([
    'ADD', 'ALTER', 'AND', 'AS', 'ASC', 'BETWEEN', 'BY', 'CREATE', 'DELETE', 'DESC', 'DISTINCT', 'DROP', 'EXISTS',
    'EXPLAIN', 'FROM', 'GROUP', 'HAVING', 'IN', 'INDEX', 'INSERT', 'INTO', 'JOIN', 'KEY', 'LEFT', 'LIKE', 'LIMIT',
    'NOT', 'NULL', 'ON', 'OR', 'ORDER', 'PRIMARY', 'RIGHT', 'SELECT', 'SET', 'TABLE', 'UPDATE', 'VALUES', 'WHERE',
  ]),
  bash: new Set(['case', 'cd', 'do', 'done', 'echo', 'elif', 'else', 'export', 'fi', 'for', 'function', 'if', 'in', 'then', 'while']),
  json: new Set(['true', 'false', 'null']),
  yaml: new Set(['true', 'false', 'null']),
}

function highlightCode(code: string, language: string) {
  const normalizedLanguage = language.toLowerCase()
  const keywords = keywordMap[normalizedLanguage] ?? new Set<string>()
  const tokens = tokenizeCode(code, normalizedLanguage, keywords)
  return tokens.map((token, index) => (
    token.kind === 'plain'
      ? token.value
      : <span className={`token ${token.kind}`} key={`${token.kind}-${index}`}>{token.value}</span>
  ))
}

function tokenizeCode(code: string, language: string, keywords: Set<string>) {
  const tokens: Array<{ kind: string; value: string }> = []
  const pattern = /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|--[^\n]*|#[^\n]*|\/\/[^\n]*|\/\*[\s\S]*?\*\/|\b\d+(?:\.\d+)?\b|\b[A-Za-z_][A-Za-z0-9_]*\b|\s+|.)/g
  for (const match of code.matchAll(pattern)) {
    const value = match[0]
    if (/^["'`]/.test(value)) {
      tokens.push({ kind: 'string', value })
    } else if (/^(--|#|\/\/|\/\*)/.test(value) && language !== 'python') {
      tokens.push({ kind: 'comment', value })
    } else if (language === 'python' && value.startsWith('#')) {
      tokens.push({ kind: 'comment', value })
    } else if (/^\d/.test(value)) {
      tokens.push({ kind: 'number', value })
    } else if (keywords.has(value) || keywords.has(value.toUpperCase())) {
      tokens.push({ kind: 'keyword', value })
    } else if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(value) && code.slice((match.index ?? 0) + value.length).trimStart().startsWith('(')) {
      tokens.push({ kind: 'function', value })
    } else {
      tokens.push({ kind: 'plain', value })
    }
  }
  return tokens
}

function languageLabel(language: string) {
  const labels: Record<string, string> = {
    bash: 'Shell',
    javascript: 'JavaScript',
    json: 'JSON',
    python: 'Python',
    sql: 'SQL',
    typescript: 'TypeScript',
    yaml: 'YAML',
  }
  return labels[language.toLowerCase()] ?? language
}
