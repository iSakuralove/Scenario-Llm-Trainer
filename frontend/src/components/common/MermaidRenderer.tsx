import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Code2, Eye, Maximize2, Minimize2 } from 'lucide-react'
import mermaid from 'mermaid'

type RenderedMermaid = {
  code: string
  svg: string
}

type MermaidViewMode = 'diagram' | 'source'

export function MermaidRenderer({ code }: { code: string }) {
  const reactId = useId()
  const id = useMemo(() => `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, '')}`, [reactId])
  const text = code.trim()
  const closeFullscreenRef = useRef<HTMLButtonElement | null>(null)
  const [rendered, setRendered] = useState<RenderedMermaid | null>(null)
  const [failedCode, setFailedCode] = useState('')
  const [renderError, setRenderError] = useState('')
  const [viewMode, setViewMode] = useState<MermaidViewMode>('diagram')
  const [isFullscreen, setIsFullscreen] = useState(false)

  useEffect(() => {
    let cancelled = false
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'base' })

    if (!text) {
      const timer = window.setTimeout(() => {
        if (cancelled) return
        setRendered(null)
        setFailedCode('')
        setRenderError('')
      }, 0)
      return () => {
        cancelled = true
        window.clearTimeout(timer)
      }
    }

    const timer = window.setTimeout(() => {
      const renderID = `${id}-${Date.now()}`
      void mermaid.parse(text)
        .then(() => mermaid.render(renderID, text))
        .then((result) => {
          if (cancelled) return
          if (result.svg.includes('Syntax error in text') || result.svg.includes('mermaid version')) {
            setFailedCode(text)
            setRenderError('Mermaid 返回了语法错误占位图。')
            return
          }
          setRendered({ code: text, svg: result.svg })
          setFailedCode('')
          setRenderError('')
        })
        .catch((err) => {
          if (cancelled) return
          setFailedCode(text)
          setRenderError(err instanceof Error ? err.message : 'Mermaid 渲染失败。')
        })
    }, 420)

    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [text, id])

  useEffect(() => {
    if (!isFullscreen) return undefined
    const previousOverflow = document.body.style.overflow
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsFullscreen(false)
      }
    }
    document.body.style.overflow = 'hidden'
    document.addEventListener('keydown', onKeyDown)
    window.setTimeout(() => closeFullscreenRef.current?.focus(), 0)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [isFullscreen])

  if (!text) {
    return <div className="mermaid-box mermaid-fallback">输入 Mermaid 代码后会显示图形预览。</div>
  }

  const isLoading = Boolean(text && failedCode !== text && rendered?.code !== text)
  const isUpdating = Boolean(isLoading && rendered?.svg && rendered.code !== text)
  const isError = failedCode === text
  const sourceCode = isError ? text : rendered?.code ?? text

  function renderToolbar(fullscreen = false) {
    return (
      <div className="mermaid-toolbar">
        <div className="mermaid-status-line">
          {isLoading && !rendered?.svg && <span className="mermaid-render-chip">正在加载图形</span>}
          {isUpdating && <span className="mermaid-render-chip">正在更新预览</span>}
          {isError && rendered?.svg && <span className="mermaid-render-chip error">当前图形渲染失败，保留上次预览</span>}
          {isError && !rendered?.svg && <span className="mermaid-render-chip error">图形渲染失败</span>}
          {!isLoading && !isError && rendered?.svg && <span className="mermaid-render-chip success">图形已校验</span>}
        </div>
        <div className="mermaid-actions">
          <button
            className="ghost-button compact"
            type="button"
            onClick={() => setViewMode((current) => (current === 'diagram' ? 'source' : 'diagram'))}
          >
            {viewMode === 'diagram' ? <Code2 size={16} /> : <Eye size={16} />}
            {viewMode === 'diagram' ? '查看源码' : '查看图形'}
          </button>
          <button
            ref={fullscreen ? closeFullscreenRef : undefined}
            className="ghost-button compact"
            type="button"
            onClick={() => setIsFullscreen((current) => !current)}
          >
            {fullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
            {fullscreen ? '退出全屏' : '全屏查看'}
          </button>
        </div>
      </div>
    )
  }

  function renderBody() {
    if (viewMode === 'source') {
      return (
        <pre className="mermaid-source" data-testid="mermaid-source"><code>{sourceCode}</code></pre>
      )
    }
    if (rendered?.svg) {
      return <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: rendered.svg }} />
    }
    if (isLoading) {
      return (
        <div className="mermaid-loading" role="status">
          <span aria-hidden="true" />
          <strong>正在加载图形</strong>
          <small>正在校验 Mermaid 语法并生成预览。</small>
        </div>
      )
    }
    return (
      <div className="mermaid-fallback">
        <strong>图形渲染失败，建议查看源码。</strong>
        {import.meta.env.DEV && renderError && <small>{renderError}</small>}
      </div>
    )
  }

  function renderViewer(fullscreen = false) {
    return (
      <div
        className={`mermaid-box mermaid-shell ${isLoading ? 'is-loading' : ''} ${isUpdating ? 'is-updating' : ''} ${fullscreen ? 'mermaid-fullscreen-box' : ''}`}
        data-testid="mermaid-viewer"
      >
        {renderToolbar(fullscreen)}
        <div className="mermaid-content">{renderBody()}</div>
      </div>
    )
  }

  return (
    <>
      {renderViewer()}
      {isFullscreen && (
        <div className="mermaid-overlay" role="dialog" aria-modal="true" aria-label="Mermaid 全屏查看" data-testid="mermaid-fullscreen-viewer">
          <div className="mermaid-modal">
            {renderViewer(true)}
          </div>
        </div>
      )}
    </>
  )
}
