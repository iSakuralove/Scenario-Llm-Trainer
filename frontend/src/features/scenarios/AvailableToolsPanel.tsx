import type { ScenarioAllowedAction } from '../../types/agentRun'
import { ToolKindIcon } from './agentrun'
import { quickActionLabel } from './agentrun/quickActionLabel'

interface AvailableToolsPanelProps {
  tools: ScenarioAllowedAction[]
  disabled?: boolean
  onSelect?: (action: ScenarioAllowedAction) => void
}

/**
 * 题目数据库经过 Runtime 当前状态过滤后的公开工具目录。
 * 这里显示“现在能调用什么”，不承担推荐排序；推荐仍由回合末的 QuickActions 负责。
 */
export function AvailableToolsPanel({ tools, disabled = false, onSelect }: AvailableToolsPanelProps) {
  return (
    <section className="available-tools-panel" aria-label="当前可用工具" data-testid="available-tools-panel">
      <div className="available-tools-heading">
        <strong>当前可用工具</strong>
        <span>{tools.length > 0 ? `${tools.length} 项可调用` : '暂无可调用工具'}</span>
      </div>
      {tools.length > 0 ? (
        <div className="available-tools-list">
          {tools.map((tool) => {
            const label = quickActionLabel(tool)
            if (!onSelect) {
              return (
                <div className="available-tool-item" key={tool.action_id}>
                  <ToolKindIcon kind={tool.tool_kind} size={14} />
                  <span>{label}</span>
                </div>
              )
            }
            return (
              <button
                key={tool.action_id}
                type="button"
                className="available-tool-item available-tool-button"
                disabled={disabled}
                onClick={() => onSelect(tool)}
                aria-label={`调用${label}`}
                title={`调用${label}`}
              >
                <ToolKindIcon kind={tool.tool_kind} size={14} />
                <span>{label}</span>
              </button>
            )
          })}
        </div>
      ) : (
        <p className="available-tools-empty">
          现在没有可直接调用的公开观察。你点名了一个不在当前目录里的工具时，导师会如实告诉你，而不是现场变出一个。
        </p>
      )}
    </section>
  )
}
