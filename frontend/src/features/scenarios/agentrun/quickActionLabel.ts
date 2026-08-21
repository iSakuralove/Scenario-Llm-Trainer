import type { ScenarioAllowedAction, ScenarioRunEventAny } from '../../../types/agentRun'
import { isScenarioRunEventV2 } from '../../../types/agentRun'

const GENERIC_QUICK_ACTION_TITLES = new Set([
  '可选检查',
  '公开观察',
  '快捷检查',
  '发起了一次快捷检查',
])

const ACTION_LABELS_BY_ID: Record<string, string> = {
  'inspect:logs.callback_timeout': '回调访问日志',
  'inspect:change.gateway_release': '网关 VIP 发布记录',
  'inspect:config.route_diff': '网关 VIP 后端池与路由差异',
  'inspect:database.order_write': '订单库回调写入日志',
  'inspect:database.slow_query': 'MySQL 慢查询日志',
}

const KIND_LABELS: Record<string, string> = {
  logs: '日志',
  metrics: '指标',
  config: '配置',
  database: '数据库',
  dependency: '依赖',
  data: '数据',
}

/**
 * QuickAction 的公开标题只允许表达可区分的检查对象。
 * 历史会话可能已经把 title 落成通用占位词，此时 action_id 是唯一可靠主键。
 */
export function quickActionLabel(action: Pick<ScenarioAllowedAction, 'action_id' | 'tool_kind' | 'title'>): string {
  const title = action.title.trim()
  const label = title.replace(/^(查看|检查|查询)\s*/, '').trim()
  if (label && !GENERIC_QUICK_ACTION_TITLES.has(label)) return label

  const actionLabel = ACTION_LABELS_BY_ID[action.action_id]
  if (actionLabel) return actionLabel

  return KIND_LABELS[action.tool_kind] || '公开观察'
}

/** 从本轮公开事件中恢复 QuickAction 的动作 ID，供历史/断线恢复显示使用。 */
export function quickActionIdFromEvents(events: ScenarioRunEventAny[]): string | undefined {
  for (const event of events) {
    if (isScenarioRunEventV2(event)) {
      if (event.kind !== 'tool_result') continue
      const actionId = event.payload.tool_result.tool_id.trim()
      if (actionId.startsWith('inspect:')) return actionId
      continue
    }
    if (event.kind === 'observation_result') {
      const actionId = event.observation?.action?.trim()
      if (actionId?.startsWith('inspect:')) return actionId
    }
  }
  return undefined
}

/**
 * 统一处理已提交消息、活动中的 QuickAction 和旧会话占位标题。
 * 普通自然语言不做改写；仅当标题是通用占位词时按动作 ID 恢复对象名。
 */
export function resolveQuickActionUserLabel(
  title: string,
  options: { actionId?: string; toolKind?: string; events?: ScenarioRunEventAny[] } = {},
): string {
  const trimmedTitle = title.trim()
  const normalizedTitle = trimmedTitle.replace(/^(查看|检查|查询)\s*/, '').trim()
  if (normalizedTitle && !GENERIC_QUICK_ACTION_TITLES.has(normalizedTitle)) return trimmedTitle

  const actionId = options.actionId || quickActionIdFromEvents(options.events ?? []) || ''
  return quickActionLabel({
    action_id: actionId,
    tool_kind: options.toolKind || inferToolKind(actionId),
    title: trimmedTitle,
  })
}

function inferToolKind(actionId: string): string {
  const [, remainder = ''] = actionId.split(':', 2)
  return remainder.split('.', 1)[0] || ''
}
