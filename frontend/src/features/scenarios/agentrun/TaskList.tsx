import type { ScenarioTaskPayload } from '../../../types/agentRun'
import styles from './AgentRun.module.css'
import { ToolCallStatusIcon } from './ToolCallStatusIcon'

interface TaskListProps {
  tasks: ScenarioTaskPayload[]
  active: boolean
}

// TaskList 内嵌在 Agent 流中：处理中展开显示每项状态，完成后保留摘要并折叠详情。
// 只在单轮出现多个任务/工具时由 AgentRun 决定渲染。
export function TaskList({ tasks, active }: TaskListProps) {
  if (tasks.length === 0) return null
  const done = tasks.filter((task) => task.state === 'completed' || task.state === 'already_completed').length
  const allDone = !active && done === tasks.length

  return (
    <div className={styles.taskList} data-testid="agent-run-task-list">
      <div className={styles.taskListHeading}>
        <span>{allDone ? `已完成 ${done} 项检查` : `本轮检查 ${done}/${tasks.length}`}</span>
      </div>
      <ol className={styles.taskListItems}>
        {tasks.map((task) => (
          <li
            key={task.task_id}
            className={[
              task.state === 'running' ? styles.taskItemRunning : '',
              task.state === 'completed' || task.state === 'already_completed' ? styles.taskItemCompleted : '',
            ].filter(Boolean).join(' ')}
            data-tool-state={task.state}
          >
            <ToolCallStatusIcon key={task.state} state={task.state} />
            <span>{task.title}</span>
            <small>{taskStateLabel(task.state)}</small>
          </li>
        ))}
      </ol>
    </div>
  )
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
