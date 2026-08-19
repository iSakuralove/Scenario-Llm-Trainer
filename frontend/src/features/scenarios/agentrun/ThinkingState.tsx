import styles from './AgentRun.module.css'

interface ThinkingStateProps {
  label: string
}

export function ThinkingState({ label }: ThinkingStateProps) {
  return (
    <div className={styles.thinkingState} role="status" aria-live="polite">
      <span className={styles.breathingDot} aria-hidden="true" />
      <span>{label}</span>
      <span className={styles.shimmer} aria-hidden="true" />
    </div>
  )
}
