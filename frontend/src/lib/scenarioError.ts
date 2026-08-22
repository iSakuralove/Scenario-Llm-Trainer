const stableMessages: Record<string, string> = {
  'session not found': '排查会话不存在或已失效',
  'session is abandoned': '排查会话已结束，请重新开始',
  'session is not active': '排查会话已结束，请重新开始',
  'scenario session is not active': '排查会话已结束，请重新开始',
  'content is required': '请输入排查内容',
  'request_id is invalid': '本轮请求标识无效，请重新发送',
  'max turns reached, please submit an answer': '本轮次已用完，请提交排查结论',
}

export function sanitizeScenarioErrorMessage(message?: string): string {
  const normalized = message?.trim() ?? ''
  if (!normalized) return '本轮处理失败，请重试。'
  const canonical = normalized.replace(/[。.!！]+$/g, '').trim().toLowerCase()
  const stableMessage = stableMessages[canonical]
  if (stableMessage) return stableMessage
  // 公共学生界面不显示数据库连接串、主机解析、驱动和堆栈细节。
  if (/(postgres|postgresql|mysql|redis|sqlstate|dial tcp|hostname|lookup .*127\.0\.0\.1|connection refused|stack trace|panic)/i.test(normalized)) {
    return '本轮处理失败，请重试。'
  }
  if (/^(agent_|stream_|scenario_|reply_echoed_user_message|turn_failed|public_boundary_rejected)/i.test(canonical)) {
    return '本轮处理失败，请重试。'
  }
  return normalized
}
