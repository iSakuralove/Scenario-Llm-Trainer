import type { AIJob, AIStatus } from '../types'

export function aiModeLabel(status: AIStatus | null) {
  if (!status) {
    return 'AI 模式检测中'
  }
  if (status.fallback) {
    const configured = status.configured_provider ? `，${status.configured_provider} 未就绪` : ''
    return `Mock LLM 离线演示${configured}`
  }
  if (status.provider === 'deepseek') {
    return `DeepSeek ${status.model}`
  }
  if (status.provider === 'openai_compatible') {
    return `第三方中转 ${status.model}`
  }
  return `${status.provider} ${status.model}`
}

export function aiHealthLabel(status: AIStatus | null) {
  if (!status) return 'unknown'
  if (status.health) return status.health
  if (status.fallback) return 'fallback'
  if (status.healthy === false) return 'degraded'
  return 'ok'
}

export function aiTaskLabel(task?: string) {
  const labels: Record<string, string> = {
    scenario_generate: '场景生成',
    community_structure: 'UGC 结构化',
    scenario_reply: '排查回复',
    interview_feedback: '面试评估',
    sensitive_check: '敏感检测',
    router_status: '状态检查',
  }
  return labels[task ?? ''] ?? task ?? '未知任务'
}

export function aiCapabilitySummary(status: AIStatus | null) {
  const capability = status?.capability
  if (!capability) {
    return '能力信息未上报'
  }
  const parts = [
    capability.supports_streaming ? '流式' : '非流式',
    capability.supports_json ? 'JSON' : 'Text',
    `token ${capability.max_tokens}`,
    capability.cost_tier,
  ]
  return parts.join(' · ')
}

export function aiProviderLabel(provider?: string, fallbackUsed?: boolean) {
  if (fallbackUsed) return 'Mock LLM 兜底'
  if (provider === 'deepseek') return 'DeepSeek deepseek-v4-flash'
  if (provider === 'openai_compatible') return '第三方中转站'
  return provider || '未知来源'
}

export function aiJobModelLabel(job: AIJob | null) {
  const model = job?.model?.trim()
  if (!model) return ''
  return `，模型：${model}`
}

export function aiJobStageLabel(job: AIJob | null) {
  if (!job) return '正在创建任务'
  if (job.status === 'queued') return '已进入生成队列'
  if (job.status === 'failed') return '生成失败'
  if (job.status === 'canceled') return '已停止生成'
  const labels: Record<string, string> = {
    calling_model: '正在调用模型',
    validating_output: '正在校验结构',
    canceled: '已停止生成',
    completed: '已写入题库',
  }
  return labels[job.stage] ?? job.stage
}

export function aiJobStageText(stage?: string, status?: AIJob['status']) {
  if (status === 'queued') return '已进入生成队列'
  if (status === 'failed') return '生成失败'
  if (status === 'canceled') return '已停止生成'
  const labels: Record<string, string> = {
    calling_model: '正在调用模型',
    validating_output: '正在校验结构',
    canceled: '已停止生成',
    completed: '已写入题库',
  }
  return stage ? (labels[stage] ?? stage) : '未知阶段'
}

export function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}
