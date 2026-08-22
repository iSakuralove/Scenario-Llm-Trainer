import { CheckCircle2, CircleDashed, Loader2, XCircle } from 'lucide-react'
import type { ToolCallState } from '../../../types/agentRun'
import styles from './AgentRun.module.css'

interface ToolCallStatusIconProps {
  state: ToolCallState
  size?: number
}

/**
 * 工具调用状态图标：状态只来自 Runtime 事件，不使用前端定时器伪造进度。
 * pending = 等待调用，running = 正在执行，completed = 已完成，其他终态 = 失败/跳过。
 */
export function ToolCallStatusIcon({ state, size = 15 }: ToolCallStatusIconProps) {
  const visualState = visualStateFor(state)
  return (
    <span
      className={styles.toolStatusIcon}
      data-state={visualState}
      role="img"
      aria-label={toolStateLabel(state)}
    >
      <CircleDashed
        className={`${styles.toolStatusGlyph} ${styles.toolStatusPending}`}
        size={size}
        aria-hidden="true"
      />
      <Loader2
        className={`${styles.toolStatusGlyph} ${styles.toolStatusRunning}`}
        size={size}
        aria-hidden="true"
      />
      <CheckCircle2
        className={`${styles.toolStatusGlyph} ${styles.toolStatusCompleted}`}
        size={size}
        aria-hidden="true"
      />
      <XCircle
        className={`${styles.toolStatusGlyph} ${styles.toolStatusFailed}`}
        size={size}
        aria-hidden="true"
      />
    </span>
  )
}

function visualStateFor(state: ToolCallState): 'pending' | 'running' | 'completed' | 'failed' {
  if (state === 'running') return 'running'
  if (state === 'completed' || state === 'already_completed') return 'completed'
  if (state === 'failed' || state === 'unsupported' || state === 'rejected' || state === 'expired') {
    return 'failed'
  }
  return 'pending'
}

function toolStateLabel(state: ToolCallState): string {
  switch (state) {
    case 'pending':
      return '等待调用'
    case 'running':
      return '工具执行中'
    case 'completed':
      return '工具调用完成'
    case 'already_completed':
      return '工具此前已完成'
    case 'failed':
      return '工具调用失败'
    case 'unsupported':
      return '工具不支持'
    case 'rejected':
      return '工具调用已跳过'
    case 'expired':
      return '工具调用已过期'
    default:
      return '工具状态未知'
  }
}
