import { useLayoutEffect, useRef, useState } from 'react'
import type { KeyboardEvent, ReactNode } from 'react'
import { ChevronDown, Code2, Eye, List, Maximize2, Minimize2, Pencil, Quote, UploadCloud } from 'lucide-react'
import { MarkdownPreview } from './MarkdownPreview'

const MARKDOWN_FILE_ACCEPT = '.md,.markdown,text/markdown,text/plain'
const MARKDOWN_FILE_EXTENSIONS = new Set(['md', 'markdown'])
const MARKDOWN_FILE_MAX_BYTES = 512 * 1024

interface MarkdownComposerProps {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  placeholder: string
  editorLabel: string
  editorTestId: string
  fileInputTestId?: string
  previewEmptyText: string
  previewNote?: string
  onImportStatus?: (message: string) => void
  onImportError?: (message: string) => void
}

export function MarkdownComposer({
  value,
  onChange,
  disabled = false,
  placeholder,
  editorLabel,
  editorTestId,
  fileInputTestId,
  previewEmptyText,
  previewNote = '这是 Markdown 渲染预览，提交时仍会使用原始内容。',
  onImportStatus,
  onImportError,
}: MarkdownComposerProps) {
  const editorRef = useRef<HTMLTextAreaElement | null>(null)
  const pendingSelectionRef = useRef<{ start: number; end: number } | null>(null)
  const [previewMode, setPreviewMode] = useState<'edit' | 'preview'>('edit')
  const [isExpanded, setExpanded] = useState(false)

  useLayoutEffect(() => {
    const selection = pendingSelectionRef.current
    const textarea = editorRef.current
    if (!selection || !textarea) return
    pendingSelectionRef.current = null
    textarea.focus()
    textarea.setSelectionRange(selection.start, selection.end)
  }, [value])

  function commitValue(nextValue: string, selectionStart: number, selectionEnd: number) {
    pendingSelectionRef.current = { start: selectionStart, end: selectionEnd }
    onChange(nextValue)
  }

  function applySnippet(snippet: string) {
    const textarea = editorRef.current
    if (!textarea) {
      onChange(insertMarkdown(value, snippet))
      return
    }
    const result = insertSnippetAtSelection(textarea.value, textarea.selectionStart, textarea.selectionEnd, snippet)
    commitValue(result.value, result.selectionStart, result.selectionEnd)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    const transform = getEditorKeyTransform(event)
    if (!transform) return

    const textarea = event.currentTarget
    const result = transformAnswerText(textarea.value, textarea.selectionStart, textarea.selectionEnd, transform)
    if (!result) return

    event.preventDefault()
    commitValue(result.value, result.selectionStart, result.selectionEnd)
  }

  async function handleMarkdownFile(file: File | null) {
    if (!file) return
    const validationError = validateMarkdownFile(file)
    if (validationError) {
      onImportError?.(validationError)
      return
    }
    try {
      const content = await file.text()
      onChange(content)
      setPreviewMode('preview')
      onImportStatus?.(`已导入 Markdown：${file.name}`)
    } catch {
      onImportError?.('Markdown 文件读取失败，请重新选择')
    }
  }

  return (
    <div className={`markdown-composer ${isExpanded ? 'expanded' : ''}`}>
      <div className="markdown-toolbar" aria-label="Markdown 工具栏">
        <ToolbarSelect
          icon={<Pencil size={14} />}
          label="标题"
          ariaLabel="选择标题级别"
          options={headingOptions}
          onSelect={applySnippet}
        />
        <ToolbarSelect
          icon={<List size={14} />}
          label="列表"
          ariaLabel="选择列表类型"
          options={listOptions}
          onSelect={applySnippet}
        />
        <ToolbarSelect
          icon={<Code2 size={14} />}
          label="代码块"
          ariaLabel="选择代码块语言"
          options={codeBlockOptions}
          onSelect={applySnippet}
        />
        <ToolbarSelect
          icon={<Code2 size={14} />}
          label="Mermaid"
          ariaLabel="选择 Mermaid 图类型"
          options={mermaidOptions}
          onSelect={applySnippet}
        />
        <button type="button" onClick={() => applySnippet('>')} disabled={disabled} title="插入引用">
          <Quote size={14} />引用
        </button>
        <label className={`markdown-file-button ${disabled ? 'disabled' : ''}`}>
          <UploadCloud size={14} />导入 MD
          <input
            type="file"
            accept={MARKDOWN_FILE_ACCEPT}
            disabled={disabled}
            aria-label="导入 Markdown 文件"
            data-testid={fileInputTestId}
            onChange={(event) => {
              void handleMarkdownFile(event.target.files?.[0] ?? null)
              event.target.value = ''
            }}
          />
        </label>
        <button
          className="preview-toggle"
          type="button"
          onClick={() => setPreviewMode((mode) => (mode === 'edit' ? 'preview' : 'edit'))}
          title={previewMode === 'edit' ? '切换到预览' : '切换到编辑'}
        >
          <Eye size={14} />{previewMode === 'edit' ? '预览' : '编辑'}
        </button>
        <button
          className="expand-toggle"
          type="button"
          aria-pressed={isExpanded}
          onClick={() => setExpanded((expanded) => !expanded)}
          title={isExpanded ? '退出全屏编辑' : '全屏编辑'}
        >
          {isExpanded ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
          {isExpanded ? '退出全屏' : '全屏'}
        </button>
      </div>
      {previewMode === 'preview' && (
        <div className="markdown-preview-hint" role="note">
          {previewNote}
        </div>
      )}
      <div className={`markdown-workspace ${previewMode === 'preview' ? 'preview-only' : ''}`}>
        <textarea
          ref={editorRef}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          disabled={disabled}
          aria-label={editorLabel}
          data-testid={editorTestId}
          onKeyDown={handleKeyDown}
        />
        <div className="markdown-preview-panel">
          <div className="markdown-preview-header">
            <span><Eye size={14} /> Markdown 实时预览</span>
            <small>{previewMode === 'preview' ? '当前仅显示排版效果' : '编辑内容会同步渲染到这里'}</small>
          </div>
          <MarkdownPreview content={value} emptyText={previewEmptyText} />
        </div>
      </div>
    </div>
  )
}

function ToolbarSelect({
  icon,
  label,
  ariaLabel,
  options,
  onSelect,
}: {
  icon: ReactNode
  label: string
  ariaLabel: string
  options: MarkdownToolbarOption[]
  onSelect: (snippet: string) => void
}) {
  const [open, setOpen] = useState(false)

  return (
    <div
      className={`toolbar-menu ${open ? 'open' : ''}`}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setOpen(false)
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          setOpen(false)
        }
      }}
    >
      <button
        className="toolbar-menu-trigger"
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{icon}{label}</span>
        <ChevronDown size={14} />
      </button>
      {open && (
        <div className="toolbar-menu-options" role="menu">
          {options.map((option) => (
            <button
              key={option.label}
              type="button"
              role="menuitem"
              aria-label={option.label}
              onClick={() => {
                onSelect(option.snippet)
                setOpen(false)
              }}
            >
              <span className="menu-option-label">
                {option.badge && <span className={`language-badge ${option.badgeTone ?? 'neutral'}`} aria-hidden="true">{option.badge}</span>}
                <span>{option.displayLabel ?? option.label}</span>
              </span>
              {option.shortcut && <kbd aria-hidden="true">{option.shortcut}</kbd>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

type MarkdownToolbarOption = {
  label: string
  snippet: string
  displayLabel?: string
  badge?: string
  badgeTone?: string
  shortcut?: string
}

const headingOptions: MarkdownToolbarOption[] = [
  { label: '一级标题', displayLabel: '一级', snippet: '#', shortcut: 'Ctrl+1' },
  { label: '二级标题', displayLabel: '二级', snippet: '##', shortcut: 'Ctrl+2' },
  { label: '三级标题', displayLabel: '三级', snippet: '###', shortcut: 'Ctrl+3' },
]

const listOptions: MarkdownToolbarOption[] = [
  { label: '无序列表', displayLabel: '无序', snippet: '- ', shortcut: 'Ctrl+Shift+]' },
  { label: '有序列表', displayLabel: '有序', snippet: '1. ', shortcut: 'Ctrl+Shift+[' },
]

const codeBlockOptions: MarkdownToolbarOption[] = [
  { label: '纯文本', displayLabel: '文本', badge: '文', badgeTone: 'neutral', snippet: '```\n\n```', shortcut: 'Ctrl+Shift+K' },
  { label: 'SQL', badge: 'SQL', badgeTone: 'blue', snippet: '```sql\n\n```' },
  { label: 'Python', badge: 'PY', badgeTone: 'yellow', snippet: '```python\n\n```' },
  { label: 'Java', badge: 'JV', badgeTone: 'red', snippet: '```java\n\n```' },
  { label: 'JavaScript', displayLabel: 'JS', badge: 'JS', badgeTone: 'yellow', snippet: '```javascript\n\n```' },
  { label: 'TypeScript', displayLabel: 'TS', badge: 'TS', badgeTone: 'blue', snippet: '```typescript\n\n```' },
  { label: 'Go', badge: 'GO', badgeTone: 'cyan', snippet: '```go\n\n```' },
  { label: 'Shell', badge: 'SH', badgeTone: 'green', snippet: '```bash\n\n```' },
  { label: 'JSON', badge: '{}', badgeTone: 'neutral', snippet: '```json\n\n```' },
  { label: 'YAML', badge: 'YML', badgeTone: 'purple', snippet: '```yaml\n\n```' },
]

const mermaidOptions: MarkdownToolbarOption[] = [
  { label: '流程图', snippet: '```mermaid\ngraph LR\n\n```' },
  { label: '时序图', snippet: '```mermaid\nsequenceDiagram\n\n```' },
  { label: '状态图', snippet: '```mermaid\nstateDiagram-v2\n\n```' },
  { label: '思维导图', snippet: '```mermaid\nmindmap\n\n```' },
]

function insertMarkdown(current: string, snippet: string) {
  const trimmed = current.trimEnd()
  return `${trimmed}${trimmed ? '\n\n' : ''}${snippet}`
}

function insertSnippetAtSelection(value: string, selectionStart: number, selectionEnd: number, snippet: string) {
  const before = value.slice(0, selectionStart)
  const selected = value.slice(selectionStart, selectionEnd)
  const after = value.slice(selectionEnd)
  const prefix = before && !before.endsWith('\n') ? '\n\n' : ''
  const suffix = after && !after.startsWith('\n') ? '\n\n' : ''
  const content = selected ? snippet.replace(/\n\n(?=```$)/, `\n${selected}\n`) : snippet
  const nextValue = `${before}${prefix}${content}${suffix}${after}`
  const insertedStart = before.length + prefix.length
  const cursor = insertedStart + content.length
  return {
    value: nextValue,
    selectionStart: cursor,
    selectionEnd: cursor,
  }
}

type EditorKeyTransform =
  | { type: 'indent'; outdent: boolean }
  | { type: 'heading'; level: 1 | 2 | 3 }
  | { type: 'list'; ordered: boolean }
  | { type: 'code' }
  | { type: 'quote' }
  | { type: 'listEnter' }

function getEditorKeyTransform(event: KeyboardEvent<HTMLTextAreaElement>): EditorKeyTransform | null {
  const key = event.key.toLowerCase()
  const commandKey = event.ctrlKey || event.metaKey
  if (event.key === 'Tab') {
    return { type: 'indent', outdent: event.shiftKey }
  }
  if (event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) {
    return { type: 'listEnter' }
  }
  if (!commandKey || event.altKey) return null
  if (!event.shiftKey && (event.key === '1' || event.key === '2' || event.key === '3')) {
    return { type: 'heading', level: Number(event.key) as 1 | 2 | 3 }
  }
  if (event.shiftKey && (event.key === ']' || event.code === 'BracketRight')) {
    return { type: 'list', ordered: false }
  }
  if (event.shiftKey && (event.key === '[' || event.code === 'BracketLeft')) {
    return { type: 'list', ordered: true }
  }
  if (event.shiftKey && key === 'k') {
    return { type: 'code' }
  }
  if (event.shiftKey && key === 'q') {
    return { type: 'quote' }
  }
  return null
}

function transformAnswerText(value: string, selectionStart: number, selectionEnd: number, transform: EditorKeyTransform) {
  switch (transform.type) {
    case 'indent':
      return transform.outdent
        ? outdentAnswerText(value, selectionStart, selectionEnd)
        : indentAnswerText(value, selectionStart, selectionEnd)
    case 'heading':
      return transformCurrentLine(value, selectionStart, '#'.repeat(transform.level), /^#{1,6}\s*/)
    case 'list':
      return transformCurrentLine(value, selectionStart, transform.ordered ? '1.' : '-', /^(\s*)(?:[-*+]\s*|\d+\.\s*)/)
    case 'code':
      return wrapSelectionOrLine(value, selectionStart, selectionEnd, '```\n', '\n```')
    case 'quote':
      return transformCurrentLine(value, selectionStart, '>', /^>\s*/)
    case 'listEnter':
      return continueListOnEnter(value, selectionStart, selectionEnd)
    default:
      return { value, selectionStart, selectionEnd }
  }
}

function continueListOnEnter(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart !== selectionEnd) return null
  if (selectionStart < value.length && value[selectionStart] !== '\n') return null
  const range = getLineRange(value, selectionStart, selectionEnd)
  const beforeCursor = value.slice(range.start, selectionStart)
  const ordered = /^(\s*)(\d+)\.\s?(.*)$/.exec(beforeCursor)
  if (ordered) {
    const [, indent, numberText, content] = ordered
    if (!content.trim()) {
      return handleEmptyListEnter(value, range, indent, {
        markerPattern: /^(\s*)(\d+)\.\s?(.*)$/,
      })
    }
    const nextMarker = `${indent}${Number(numberText) + 1}. `
    return insertAtCursor(value, selectionStart, `\n${nextMarker}`)
  }
  const unordered = /^(\s*)([-*+])\s?(.*)$/.exec(beforeCursor)
  if (unordered) {
    const [, indent, bullet, content] = unordered
    if (!content.trim()) {
      return handleEmptyListEnter(value, range, indent, {
        markerPattern: /^(\s*)([-*+])\s?(.*)$/,
      })
    }
    return insertAtCursor(value, selectionStart, `\n${indent}${bullet} `)
  }
  return null
}

function handleEmptyListEnter(
  value: string,
  range: { start: number; end: number },
  indent: string,
  list: { markerPattern: RegExp },
) {
  const previous = getPreviousLine(value, range.start)
  if (previous) {
    const previousMatch = list.markerPattern.exec(previous.text)
    if (previousMatch && !String(previousMatch[3] ?? '').trim()) {
      const nextValue = `${value.slice(0, previous.start)}${indent}${value.slice(range.end)}`
      const nextCursor = previous.start + indent.length
      return { value: nextValue, selectionStart: nextCursor, selectionEnd: nextCursor }
    }
    if (previousMatch) {
      return removeCurrentLineMarker(value, range.start, range.end, indent)
    }
  }
  return removeCurrentLineMarker(value, range.start, range.end, indent)
}

function getPreviousLine(value: string, lineStart: number) {
  if (lineStart <= 0) return null
  const end = lineStart - 1
  const start = value.lastIndexOf('\n', Math.max(0, end - 1)) + 1
  return { start, end, text: value.slice(start, end) }
}

function insertAtCursor(value: string, cursor: number, inserted: string) {
  const nextCursor = cursor + inserted.length
  return {
    value: `${value.slice(0, cursor)}${inserted}${value.slice(cursor)}`,
    selectionStart: nextCursor,
    selectionEnd: nextCursor,
  }
}

function removeCurrentLineMarker(value: string, lineStart: number, lineEnd: number, replacement: string) {
  const nextValue = `${value.slice(0, lineStart)}${replacement}${value.slice(lineEnd)}`
  const nextCursor = lineStart + replacement.length
  return {
    value: nextValue,
    selectionStart: nextCursor,
    selectionEnd: nextCursor,
  }
}

function indentAnswerText(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart === selectionEnd) {
    return {
      value: `${value.slice(0, selectionStart)}  ${value.slice(selectionEnd)}`,
      selectionStart: selectionStart + 2,
      selectionEnd: selectionStart + 2,
    }
  }
  return transformSelectedLines(value, selectionStart, selectionEnd, (line) => `  ${line}`)
}

function outdentAnswerText(value: string, selectionStart: number, selectionEnd: number) {
  if (selectionStart === selectionEnd) {
    const beforeCursor = value.slice(Math.max(0, selectionStart - 2), selectionStart)
    if (beforeCursor === '  ') {
      return {
        value: `${value.slice(0, selectionStart - 2)}${value.slice(selectionStart)}`,
        selectionStart: selectionStart - 2,
        selectionEnd: selectionStart - 2,
      }
    }
    if (value[selectionStart - 1] === '\t') {
      return {
        value: `${value.slice(0, selectionStart - 1)}${value.slice(selectionStart)}`,
        selectionStart: selectionStart - 1,
        selectionEnd: selectionStart - 1,
      }
    }
  }
  return transformSelectedLines(value, selectionStart, selectionEnd, (line) => line.replace(/^( {1,2}|\t)/, ''))
}

function transformSelectedLines(value: string, selectionStart: number, selectionEnd: number, transformLine: (line: string) => string) {
  const range = getLineRange(value, selectionStart, selectionEnd)
  const block = value.slice(range.start, range.end)
  const nextBlock = block.split('\n').map(transformLine).join('\n')
  const nextValue = `${value.slice(0, range.start)}${nextBlock}${value.slice(range.end)}`
  const delta = nextBlock.length - block.length
  return {
    value: nextValue,
    selectionStart: Math.max(range.start, selectionStart + Math.min(0, delta)),
    selectionEnd: Math.max(range.start, selectionEnd + delta),
  }
}

function transformCurrentLine(value: string, cursor: number, marker: string, stripPattern: RegExp) {
  const range = getLineRange(value, cursor, cursor)
  const line = value.slice(range.start, range.end)
  const stripped = line.replace(stripPattern, '')
  const nextLine = stripped ? `${marker} ${stripped}` : `${marker} `
  const nextValue = `${value.slice(0, range.start)}${nextLine}${value.slice(range.end)}`
  const nextCursor = range.start + nextLine.length
  return { value: nextValue, selectionStart: nextCursor, selectionEnd: nextCursor }
}

function wrapSelection(value: string, selectionStart: number, selectionEnd: number, before: string, after: string) {
  const selected = value.slice(selectionStart, selectionEnd)
  const nextValue = `${value.slice(0, selectionStart)}${before}${selected}${after}${value.slice(selectionEnd)}`
  const innerStart = selectionStart + before.length
  const innerEnd = innerStart + selected.length
  return {
    value: nextValue,
    selectionStart: selected ? innerStart : innerStart,
    selectionEnd: selected ? innerEnd : innerStart,
  }
}

function wrapSelectionOrLine(value: string, selectionStart: number, selectionEnd: number, before: string, after: string) {
  if (selectionStart !== selectionEnd) {
    return wrapSelection(value, selectionStart, selectionEnd, before, after)
  }
  const range = getLineRange(value, selectionStart, selectionEnd)
  if (range.start === range.end) {
    return wrapSelection(value, selectionStart, selectionEnd, before, after)
  }
  return wrapSelection(value, range.start, range.end, before, after)
}

function getLineRange(value: string, selectionStart: number, selectionEnd: number) {
  const start = value.lastIndexOf('\n', Math.max(0, selectionStart - 1)) + 1
  const endLineBreak = value.indexOf('\n', selectionEnd)
  const end = endLineBreak === -1 ? value.length : endLineBreak
  return { start, end }
}

function validateMarkdownFile(file: File) {
  if (file.size <= 0) {
    return 'Markdown 文件为空，请重新选择'
  }
  if (file.size > MARKDOWN_FILE_MAX_BYTES) {
    return 'Markdown 文件不能超过 512KB'
  }
  const extension = file.name.split('.').pop()?.toLowerCase()
  const mimeType = file.type.trim().toLowerCase()
  if (!extension || !MARKDOWN_FILE_EXTENSIONS.has(extension)) {
    return '请上传 .md 或 .markdown 文件'
  }
  if (mimeType && mimeType !== 'text/markdown' && mimeType !== 'text/plain') {
    return '请上传 Markdown 文本文件'
  }
  return ''
}
