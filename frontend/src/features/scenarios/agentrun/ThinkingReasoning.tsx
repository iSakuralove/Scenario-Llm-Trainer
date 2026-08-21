import { ChevronDown } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ScenarioPublicReasoningSummary } from '../../../types/agentRun'
import styles from './AgentRun.module.css'

interface ThinkingReasoningProps {
  items: ScenarioPublicReasoningSummary[]
  /** 测试专用原始 reasoning 增量；正式环境始终为空。 */
  rawChunks?: string[]
  rawActive?: boolean
  rawElapsedSeconds?: number
}

export function ThinkingReasoning({
  items,
  rawChunks = [],
  rawActive = false,
  rawElapsedSeconds,
}: ThinkingReasoningProps) {
  // AICSS 风格：进行中自动展开，完成后默认折叠；历史回放不会因为
  // 组件重新挂载而把整段调试思维重新铺开。
  const [rawOpen, setRawOpen] = useState(rawActive)
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [elapsedSeconds, setElapsedSeconds] = useState(Math.max(1, rawElapsedSeconds ?? 1))
  const rawStartedAt = useRef<number | null>(null)
  const wasRawActive = useRef(false)
  const rawViewportRef = useRef<HTMLDivElement>(null)
  const rawText = rawChunks.join('')

  useEffect(() => {
    if (rawActive && !wasRawActive.current) {
      rawStartedAt.current = Date.now()
      setElapsedSeconds(1)
      setRawOpen(true)
    } else if (!rawActive && wasRawActive.current) {
      if (rawStartedAt.current !== null) {
        setElapsedSeconds(Math.max(1, Math.round((Date.now() - rawStartedAt.current) / 1000)))
      }
      setRawOpen(false)
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
    const viewport = rawViewportRef.current
    if (!viewport || !rawActive) return
    viewport.scrollTop = viewport.scrollHeight
  }, [rawActive, rawText])

  // 调试流刚建立时可能尚未收到第一段 reasoning；仍要先展示 AICSS
  // 风格的“思考中…”容器和光标，避免页面看起来像没有流式处理。
  if (items.length === 0 && rawText === '' && !rawActive) return null
  const shownElapsedSeconds = rawElapsedSeconds ?? elapsedSeconds

  return (
    <div className={styles.reasoningStack}>
      {(rawText !== '' || rawActive) && (
        <div className={`${styles.rawReasoningBlock} ${rawActive ? styles.rawReasoningActive : ''}`} data-testid="scenario-agent-raw-reasoning">
          <button
            className={`${styles.rawReasoningButton} ${!rawActive ? styles.rawReasoningClickable : ''}`}
            type="button"
            onClick={() => {
              if (!rawActive) setRawOpen((current) => !current)
            }}
            aria-expanded={rawOpen}
            aria-label="切换思考过程"
          >
            {rawActive ? (
              <span className={`${styles.rawReasoningTitle} ${styles.rawReasoningShimmer}`}>思考中…</span>
            ) : (
              <span className={styles.rawReasoningTitle}><strong>思考</strong> 用时 {shownElapsedSeconds} 秒</span>
            )}
            {!rawActive && <ChevronDown className={rawOpen ? styles.chevronOpen : ''} size={13} aria-hidden="true" />}
          </button>
          <div className={`${styles.rawReasoningPanel} ${rawOpen ? styles.rawReasoningPanelOpen : ''}`}>
            <div ref={rawViewportRef} className={styles.rawReasoningText} aria-live="polite">
              {rawText}
              {rawActive && <span className={styles.rawReasoningCursor} aria-hidden="true" />}
            </div>
          </div>
        </div>
      )}

      {items.length > 0 && (
        <div className={styles.reasoningBlock}>
          <button
            className={styles.disclosureButton}
            type="button"
            onClick={() => setSummaryOpen((current) => !current)}
            aria-expanded={summaryOpen}
          >
            <ChevronDown className={summaryOpen ? styles.chevronOpen : ''} size={15} aria-hidden="true" />
            查看处理摘要
            <span>{items.length} 项</span>
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
