import { Database, Gauge, Link2, ScrollText, Settings2, Wrench } from 'lucide-react'

// tool_kind → 图标的固定映射（Runtime 下发稳定 tool_kind，前端不猜）。
// 独立成文件：react-refresh 要求组件文件只导出组件。
export function ToolKindIcon({ kind, size = 14 }: { kind: string; size?: number }) {
  switch (kind) {
    case 'logs':
      return <ScrollText size={size} aria-hidden="true" />
    case 'metrics':
      return <Gauge size={size} aria-hidden="true" />
    case 'config':
      return <Settings2 size={size} aria-hidden="true" />
    case 'database':
    case 'data':
      return <Database size={size} aria-hidden="true" />
    case 'dependency':
      return <Link2 size={size} aria-hidden="true" />
    default:
      return <Wrench size={size} aria-hidden="true" />
  }
}
