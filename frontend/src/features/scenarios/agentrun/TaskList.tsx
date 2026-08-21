import { CheckCircle2, CircleDashed, Loader2, XCircle } from 'lucide-react'
import type { ScenarioTaskPayload } from '../../../types/agentRun'
import styles from './AgentRun.module.css'

interface TaskListProps {
  tasks: ScenarioTaskPayload[]
  active: boolean
}

// TaskList 内嵌在 Agent 流中：处理中展开显示每项状态，完成后保留摘要并折叠详情。
// 只在单轮出现多个任务/工具时由 AgentRun 决定渲染。
export function TaskList({ tasks, active }: TaskListProps) {
  if (tasks.length === 0) return null
  const done = tasks.filter((task) => task.state === 'completed').length
  const allDone = !active && done === tasks.length

  return (
    <div className={styles.taskList} data-testid="agent-run-task-list">
      <div className={styles.taskListHeading}>
        <span>{allDone ? `已完成 ${done} 项检查` : `本轮检查 ${done}/${tasks.length}`}</span>
      </div>
      {!allDone && (
        <ol className={styles.taskListItems}>
          {tasks.map((task) => (
            <li key={task.task_id} className={task.state === 'running' ? styles.taskItemRunning : ''}>
              <TaskStateIcon state={task.state} />
              <span>{task.title}</span>
              <small>{taskStateLabel(task.state)}</small>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

function TaskStateIcon({ state }: { state: ScenarioTaskPayload['state'] }) {
  switch (state) {
    case 'completed':
      return <CheckCircle2 size={14} aria-hidden="true" />
    case 'running':
      return <Loader2 size={14} aria-hidden="true" className={styles.taskSpinner} />
    case 'failed':
    case 'rejected':
    case 'unsupported':
    case 'expired':
      return <XCircle size={14} aria-hidden="true" />
    default:
      return <CircleDashed size={14} aria-hidden="true" />
  }
}

function taskStateLabel(state: ScenarioTaskPayload['state']): string {
  switch (state) {
    case 'pending':
      return '排队中'
    case 'running':
      return '进行中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    case 'unsupported':
      return '不支持'
    case 'rejected':
      return '已跳过'
    case 'expired':
      return '已过期'
    case 'already_completed':
      return '此前已完成'
    default:
      return state
  }
}
