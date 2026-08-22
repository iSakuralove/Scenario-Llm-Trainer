import { ChevronDown } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { ScenarioPublicReasoningSummary } from '../../../types/agentRun'
import styles from './AgentRun.module.css'

// 打字机节奏：基础约 40 字/秒；积压越大单步补得越多，突发大块增量也能在
// 几百毫秒内追平流，不会越落越远。
const TYPE_INTERVAL_MS = 24
const TYPE_BACKLOG_DIVISOR = 16
// 流结束且打字追平后稍等一拍再折叠，避免正文戛然而止。
const COLLAPSE_BEAT_MS = 360
// 视口渐隐高度，与 CSS 中 trViewport 的 max-height 保持同一套几何。
const FADE_PX = 16

interface ThinkingReasoningProps {
  items: ScenarioPublicReasoningSummary[]
  /** 测试专用原始 reasoning 增量；正式环境始终为空。 */
  rawChunks?: string[]
  rawActive?: boolean
  rawElapsedSeconds?: number
}

interface ViewportFade {
  overflow: boolean
  top: boolean
  bottom: boolean
}

export function ThinkingReasoning({
  items,
  rawChunks = [],
  rawActive = false,
  rawElapsedSeconds,
}: ThinkingReasoningProps) {
  const prefersReducedMotion = useMemo(
    () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false,
    [],
  )
  // AICSS 风格：思考中自动展开并跟随打字，完成后折叠为“思考 用时 N 秒”，
  // 用户可再点开自由滚动；历史回放不会重新铺开整段调试思维。
  const [open, setOpen] = useState(false)
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [elapsedSeconds, setElapsedSeconds] = useState(Math.max(1, rawElapsedSeconds ?? 1))
  const [typedLength, setTypedLength] = useState(rawActive ? 0 : rawChunks.join('').length)
  const [fade, setFade] = useState<ViewportFade>({ overflow: false, top: false, bottom: false })
  const rawStartedAt = useRef<number | null>(null)
  const wasRawActive = useRef(false)
  const userToggled = useRef(false)
  const viewportRef = useRef<HTMLDivElement>(null)

  const rawText = rawChunks.join('')
  const typingDone = prefersReducedMotion || typedLength >= rawText.length

  useEffect(() => {
    if (typingDone) return
    const timer = window.setInterval(() => {
      setTypedLength((prev) => {
        if (prev >= rawText.length) return prev
        const step = Math.max(1, Math.floor((rawText.length - prev) / TYPE_BACKLOG_DIVISOR))
        return Math.min(prev + step, rawText.length)
      })
    }, TYPE_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [rawText, typingDone, prefersReducedMotion])

  useEffect(() => {
    if (rawActive && !wasRawActive.current) {
      rawStartedAt.current = Date.now()
      setElapsedSeconds(1)
      userToggled.current = false
    } else if (!rawActive && wasRawActive.current && rawStartedAt.current !== null) {
      setElapsedSeconds(Math.max(1, Math.round((Date.now() - rawStartedAt.current) / 1000)))
    }
    wasRawActive.current = rawActive
  }, [rawActive])

  useEffect(() => {
    if (!rawActive || rawStartedAt.current === null) return
    const timer = window.setInterval(() => {
      if (rawStartedAt.current !== null) {
        setElapsedSeconds(Math.max(1, Math.round((Date.now() - rawStartedAt.current) / 1000)))
      }
    }, 1000)
    return () => window.clearInterval(timer)
  }, [rawActive])

  useEffect(() => {
    if (rawActive || !typingDone) return
    const timer = window.setTimeout(() => {
      if (!userToggled.current) setOpen(false)
    }, COLLAPSE_BEAT_MS)
    return () => window.clearTimeout(timer)
  }, [rawActive, typingDone])

  const expanded = rawActive || open
  const typedText = prefersReducedMotion ? rawText : rawText.slice(0, typedLength)

  const measureFade = () => {
    const el = viewportRef.current
    if (!el) return
    const next: ViewportFade = {
      overflow: el.scrollHeight > el.clientHeight + 1,
      top: el.scrollTop > 1,
      bottom: el.scrollTop + el.clientHeight < el.scrollHeight - 1,
    }
    setFade((prev) =>
      prev.overflow === next.overflow && prev.top === next.top && prev.bottom === next.bottom
        ? prev
        : next,
    )
  }

  // 思考中跟随打字进度钉在底部；完成后交给用户滚动，渐隐随滚动位置更新。
  useEffect(() => {
    const el = viewportRef.current
    if (!el || !expanded) return
    if (rawActive) el.scrollTop = el.scrollHeight
    measureFade()
  }, [typedText, expanded, rawActive])

  // 调试流刚建立时可能尚未收到第一段 reasoning；仍要先展示“思考中…”
  // 容器和光标，避免页面看起来像没有流式处理。
  if (items.length === 0 && rawText === '' && !rawActive) return null
  const shownElapsedSeconds = rawElapsedSeconds ?? elapsedSeconds
  const mask = fade.overflow
    ? `linear-gradient(to bottom, transparent 0, #000 ${fade.top ? FADE_PX : 0}px, #000 calc(100% - ${fade.bottom ? FADE_PX : 0}px), transparent 100%)`
    : 'none'

  return (
    <div className={styles.reasoningStack}>
      {(rawText !== '' || rawActive) && (
        <div className={styles.trBlock} data-testid="scenario-agent-raw-reasoning">
          <button
            type="button"
            className={`${styles.trHeader} ${!rawActive ? styles.trClickable : ''}`}
            onClick={() => {
              if (rawActive) return
              userToggled.current = true
              setOpen((current) => !current)
            }}
            aria-expanded={expanded}
            aria-label="切换思考过程"
          >
            {rawActive ? (
              <span className={`${styles.trLabel} ${styles.trShimmer}`}>思考中…</span>
            ) : (
              <span className={styles.trLabel}>
                <span className={styles.trVerb}>思考</span> 用时 {shownElapsedSeconds} 秒
              </span>
            )}
            {!rawActive && (
              <ChevronDown
                className={expanded ? styles.trChevronOpen : undefined}
                size={12}
                aria-hidden="true"
              />
            )}
          </button>
          <div className={`${styles.trCollapsible} ${expanded ? '' : styles.trCollapsed}`}>
            <div className={styles.trInner}>
              <div
                ref={viewportRef}
                className={`${styles.trViewport} ${!rawActive && open ? styles.trScroll : ''}`}
                style={{ maskImage: mask, WebkitMaskImage: mask }}
                onScroll={measureFade}
              >
                <p className={styles.trSentence}>
                  {typedText}
                  {(rawActive || !typingDone) && (
                    <span className={styles.trCursor} aria-hidden="true" />
                  )}
                </p>
              </div>
            </div>
          </div>
        </div>
      )}

      {items.length > 0 && (
        <div className={styles.reasoningBlock}>
          <button
            className={`${styles.trHeader} ${styles.trClickable}`}
            type="button"
            onClick={() => setSummaryOpen((current) => !current)}
            aria-expanded={summaryOpen}
          >
            <span className={styles.trLabel}>查看处理摘要</span>
            <span className={styles.trCount}>{items.length} 项</span>
            <ChevronDown
              className={summaryOpen ? styles.trChevronOpen : undefined}
              size={12}
              aria-hidden="true"
            />
          </button>
          <div className={`${styles.disclosureGrid} ${summaryOpen ? styles.disclosureGridOpen : ''}`}>
            <div className={styles.disclosureInner}>
              <ol className={styles.reasoningList}>
                {items.map((item, index) => (
                  <li key={`${item.stage}-${item.text}-${index}`}>{item.text}</li>
                ))}
              </ol>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
