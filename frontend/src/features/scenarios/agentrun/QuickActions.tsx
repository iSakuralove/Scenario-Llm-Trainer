import type { ScenarioAllowedAction } from '../../../types/agentRun'
import { ToolKindIcon } from './ToolKindIcon'
import { quickActionLabel } from './quickActionLabel'
import styles from './AgentRun.module.css'

interface QuickActionsProps {
  actions: ScenarioAllowedAction[]
  disabled?: boolean
  onSelect: (action: ScenarioAllowedAction) => void
}

// QuickActions 只渲染 Runtime 下发的结构化动作；公开文案保留检查对象，
// 去掉“查看/检查/查询”等指令式前缀，不把动作改成无法区分的统一占位词。
export function QuickActions({ actions, disabled = false, onSelect }: QuickActionsProps) {
  if (actions.length === 0) return null

  return (
    <div className={styles.quickActions} data-testid="agent-run-quick-actions">
      {actions.map((action) => (
        (() => {
          const label = quickActionLabel(action)
          return (
            <button
              key={action.action_id}
              type="button"
              className={styles.quickActionButton}
              disabled={disabled}
              onClick={() => onSelect(action)}
              data-action-id={action.action_id}
              aria-label={label}
            >
              <ToolKindIcon kind={action.tool_kind} size={14} />
              <span>{label}</span>
            </button>
          )
        })()
      ))}
    </div>
  )
}
