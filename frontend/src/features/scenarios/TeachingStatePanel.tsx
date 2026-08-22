import type { CSSProperties } from 'react'
import type { ScenarioMasteryItem, ScenarioTeachingProjection } from '../../types'

interface TeachingStatePanelProps {
  projection?: ScenarioTeachingProjection
}

/**
 * 学生侧安全教学状态：只展示解释节奏、方向信号和掌握度汇总。
 * 原始 affect、假设 ID、证据 ID 和内部答案比较不进入这个组件。
 */
export function TeachingStatePanel({ projection }: TeachingStatePanelProps) {
  const mastery = projection?.mastery ?? {
    concepts: [],
    skills: [],
    concepts_covered: 0,
    concepts_total: 0,
    skills_covered: 0,
    skills_total: 0,
  }
  const concepts = mastery?.concepts ?? []
  const skills = mastery?.skills ?? []
  return (
    <section className="teaching-state-panel" aria-label="学习状态" data-testid="teaching-state-panel">
      <div className="teaching-state-heading">
        <strong>学习状态</strong>
        <span>{projection ? teachingStateLabel(projection.teaching_state) : '等待一轮对话'}</span>
      </div>
      {!projection ? (
        <p className="teaching-state-empty">完成一轮对话后，这里会显示导师当前的承接方式和解释深度。</p>
      ) : (
        <>
          <div className="teaching-state-grid">
            <div>
              <span>回应节奏</span>
              <strong>{teachingStateLabel(projection.teaching_state)}</strong>
            </div>
            <div>
              <span>方向信号</span>
              <strong data-direction={projection.direction_status}>{directionLabel(projection.direction_status)}</strong>
            </div>
            <div>
              <span>本轮进展</span>
              <strong>{progressLabel(projection.progress_assessment)}</strong>
            </div>
            <div>
              <span>解释深度</span>
              <strong>{detailLabel(projection.detail_level)}</strong>
            </div>
            {projection.focus && (
              <div>
                <span>当前焦点</span>
                <strong>{focusLabel(projection.focus)}</strong>
              </div>
            )}
          </div>
          <div className="mastery-summary">
            <div className="mastery-summary-heading">
              <span>概念掌握</span>
              <strong>{mastery.concepts_covered}/{mastery.concepts_total}</strong>
            </div>
            <MasteryList items={concepts} emptyText="还没有形成稳定的概念掌握信号。" />
            <div className="mastery-summary-heading">
              <span>排查能力</span>
              <strong>{mastery.skills_covered}/{mastery.skills_total}</strong>
            </div>
            <MasteryList items={skills} emptyText="能力权重会在学生实际展示后逐步出现。" />
          </div>
        </>
      )}
    </section>
  )
}

function MasteryList({ items, emptyText }: { items: ScenarioMasteryItem[]; emptyText: string }) {
  if (items.length === 0) return <p className="mastery-empty">{emptyText}</p>
  return (
    <div className="mastery-list">
      {items.map((item) => {
        const percent = Math.max(0, Math.min(100, Math.round(item.weight * 100)))
        const style = { '--mastery-width': `${percent}%` } as CSSProperties
        return (
          <div className="mastery-item" key={item.label}>
            <div className="mastery-item-label">
              <span>{item.label}</span>
              <strong aria-label={`${item.label}掌握权重 ${percent}%`}>{percent}%</strong>
            </div>
            <div className="mastery-track" aria-hidden="true">
              <span className="mastery-fill" style={style} />
            </div>
          </div>
        )
      })}
    </div>
  )
}

function teachingStateLabel(value: string): string {
  const labels: Record<string, string> = {
    guided_inquiry: '引导你自己连接证据',
    unsupported_hypothesis: '先核对这个方向',
    anti_guess_detected: '暂停猜测，回到事实',
    premature_conclusion: '结论还需要证据支撑',
    conclusion_grilling: '补完整因果链',
    evidence_reconstruction: '重建证据时间线',
    normal_diagnosis: '沿公开证据排查',
    debrief: '收束并复盘',
    casual_chat: '轻松交流后再回到题目',
    clarification: '先把问题说清楚',
    off_topic: '先拉回当前故障',
    garbage: '先整理本轮输入',
  }
  return labels[value] ?? '承接本轮信息'
}

function directionLabel(value: string): string {
  switch (value) {
    case 'aligned':
      return '沿证据推进'
    case 'needs_refocus':
      return '需要收拢范围'
    case 'off_topic':
      return '先回到当前故障'
    default:
      return '正在建立链路'
  }
}

function progressLabel(value: string): string {
  switch (value) {
    case 'progress':
      return '形成了新的推进'
    case 'partial':
      return '已有部分推进'
    case 'no_progress':
      return '暂未形成新推进'
    case 'unsupported':
      return '这条观察暂不支持当前判断'
    case 'contradictory':
      return '与已有方向出现冲突'
    case 'leak_risk':
      return '需要回到公开事实'
    default:
      return '正在判断'
  }
}

function detailLabel(value: string): string {
  switch (value) {
    case 'brief':
      return '简洁'
    case 'detailed':
      return '详细'
    default:
      return '均衡'
  }
}

function focusLabel(value: string): string {
  const labels: Record<string, string> = {
    logs: '日志',
    metrics: '指标',
    config: '配置',
    change: '变更',
    dependency: '依赖',
    data: '数据',
    resource: '资源',
  }
  return labels[value] ?? value
}
