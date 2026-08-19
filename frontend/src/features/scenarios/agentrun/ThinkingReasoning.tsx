import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import type { ScenarioPublicReasoningSummary } from '../../../types/agentRun'
import styles from './AgentRun.module.css'

interface ThinkingReasoningProps {
  items: ScenarioPublicReasoningSummary[]
}

export function ThinkingReasoning({ items }: ThinkingReasoningProps) {
  const [open, setOpen] = useState(false)
  if (items.length === 0) return null

  return (
    <div className={styles.reasoningBlock}>
      <button
        className={styles.disclosureButton}
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
      >
        <ChevronDown className={open ? styles.chevronOpen : ''} size={15} aria-hidden="true" />
        查看处理摘要
        <span>{items.length} 项</span>
      </button>
      <div className={`${styles.disclosureGrid} ${open ? styles.disclosureGridOpen : ''}`}>
        <div className={styles.disclosureInner}>
          <ol className={styles.reasoningList}>
            {items.map((item, index) => (
              <li key={`${item.stage}-${item.text}-${index}`}>{item.text}</li>
            ))}
          </ol>
        </div>
      </div>
    </div>
  )
}
