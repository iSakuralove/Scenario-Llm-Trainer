import type { ReactNode } from 'react'
import { Bot, Printer } from 'lucide-react'
import { domains } from '../../lib/domain'
export { MarkdownPreview } from './MarkdownPreview'
export { MarkdownComposer } from './MarkdownComposer'

export function HeaderBlock({ icon, title, description, action }: { icon: ReactNode; title: string; description: string; action?: ReactNode }) {
  return (
    <header className="page-header">
      <div className="title-cluster">
        <span className="title-icon">{icon}</span>
        <div>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
      </div>
      {action}
    </header>
  )
}

export function Metric({ label, value, variant = 'default' }: { label: string; value: string | number; variant?: 'default' | 'compact' }) {
  return (
    <div className={`metric metric-${variant}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

export function Segmented({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="segmented">
      <button className={value === '' ? 'active' : ''} onClick={() => onChange('')}>全部</button>
      {domains.map((item) => (
        <button key={item.value} className={value === item.value ? 'active' : ''} onClick={() => onChange(item.value)}>
          {item.label}
        </button>
      ))}
    </div>
  )
}

export function SegmentedOptions({ value, onChange, options }: { value: string; onChange: (value: string) => void; options: Array<{ value: string; label: string }> }) {
  return (
    <div className="segmented compact-segmented">
      {options.map((item) => (
        <button key={item.value || 'all'} className={value === item.value ? 'active' : ''} onClick={() => onChange(item.value)}>
          {item.label}
        </button>
      ))}
    </div>
  )
}

export function Select({ value, onChange, options }: { value: string; onChange: (value: string) => void; options: Array<{ value: string; label: string }> }) {
  return (
    <select value={value} onChange={(event) => onChange(event.target.value)}>
      {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
    </select>
  )
}

export function Loading({ title }: { title: string }) {
  return <div className="loading"><Bot size={18} />{title}...</div>
}

export function EmptyState({ title, description, action }: { title: string; description: string; action: ReactNode }) {
  return (
    <div className="empty-state">
      <h2>{title}</h2>
      <p>{description}</p>
      {action}
    </div>
  )
}

export function PrintButton({ label = '打印/导出 PDF' }: { label?: string }) {
  return (
    <button className="ghost-button compact no-print" type="button" onClick={() => window.print()}>
      <Printer size={16} />
      {label}
    </button>
  )
}
