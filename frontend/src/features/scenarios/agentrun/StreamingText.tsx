import type { CSSProperties } from 'react'
import styles from './AgentRun.module.css'

interface StreamingTextProps {
  chunks: string[]
  active?: boolean
}

export function StreamingText({ chunks, active = false }: StreamingTextProps) {
  return (
    <span className={styles.streamingText}>
      {chunks.map((chunk, index) => (
        <span
          className={styles.streamChunk}
          key={`${index}-${chunk}`}
          style={{ '--chunk-index': Math.min(index, 8) } as CSSProperties}
        >
          {chunk}
        </span>
      ))}
      {active && <span className={styles.streamCursor} aria-hidden="true" />}
    </span>
  )
}
