const stableMessages: Record<string, string> = {
  'agent circuit open': '排查服务暂时不可用，请稍后重试',
  'agent timeout': '本轮处理超时，请重试',
  'agent unavailable': '排查服务暂时不可用，请稍后重试',
  'agent contract mismatch': '排查服务契约不兼容',
  'agent upstream error': '排查服务返回异常',
  'agent not configured': '排查服务尚未配置',
  '这轮没有生成完整回复，请重试': '这轮没有生成完整回复，请重试',
  '排查服务暂时不可用，请稍后重试': '排查服务暂时不可用，请稍后重试',
  '本轮处理超时，请重试': '本轮处理超时，请重试',
  '排查服务契约不兼容': '排查服务契约不兼容',
  '排查服务返回异常': '排查服务返回异常',
  '排查服务尚未配置': '排查服务尚未配置',
  '排查服务返回了无效结果': '排查服务返回了无效结果',
  '会话状态已更新，请重新发送本轮内容': '会话状态已更新，请重新发送本轮内容',
  '该请求标识已被其他内容使用': '该请求标识已被其他内容使用',
  '本轮处理失败，请重试': '本轮处理失败，请重试。',
  '本轮流式响应无效，请重试': '本轮流式响应无效，请重试。',
  '流式响应缺少完成事件，请重试': '流式响应缺少完成事件，请重试。',
  '请求超时，请稍后重试或检查 ai provider 配置': '请求超时，请稍后重试或检查 AI Provider 配置',
  '无法连接后端 api，请确认服务已启动后刷新页面重试': '无法连接后端 API，请确认服务已启动后刷新页面重试',
  '浏览器不支持流式响应': '浏览器不支持流式响应',
  '流式请求失败': '流式请求失败',
  '本轮响应归属无效，请重试': '本轮响应归属无效，请重试',
  '恢复排查消息失败': '恢复排查消息失败',
  '读取排查会话失败': '读取排查会话失败',
  '消息发送失败': '消息发送失败',
  '快捷操作失败': '快捷操作失败',
  '放弃会话失败': '放弃会话失败',
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
  return '本轮处理失败，请重试。'
}
