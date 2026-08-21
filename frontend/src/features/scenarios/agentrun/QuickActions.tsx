import type { ScenarioAllowedAction } from '../../../types/agentRun'
import { ToolKindIcon } from './ToolKindIcon'
import styles from './AgentRun.module.css'

interface QuickActionsProps {
  actions: ScenarioAllowedAction[]
  disabled?: boolean
  onSelect: (action: ScenarioAllowedAction) => void
}

// QuickActions 只渲染 Runtime 下发的结构化动作；按钮表达抽象下一步检查方向，
// 不携带答案关键词。点击产生 StructuredUserAction，与自然语言共用同一轮预算。
export function QuickActions({ actions, disabled = false, onSelect }: QuickActionsProps) {
  if (actions.length === 0) return null

  return (
    <div className={styles.quickActions} data-testid="agent-run-quick-actions">
      {actions.map((action) => (
        <button
          key={action.action_id}
          type="button"
          className={styles.quickActionButton}
          disabled={disabled}
          onClick={() => onSelect(action)}
          data-action-id={action.action_id}
        >
          <ToolKindIcon kind={action.tool_kind} size={14} />
          <span>{action.title}</span>
        </button>
      ))}
    </div>
  )
}
