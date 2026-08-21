import { expect, type Page, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('student sees safe Codex-style run events during scenario troubleshooting', async ({ page }) => {
  await page.route('**/api/v1/scenarios/scenario-agent/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'e2e-agent-session',
      status: 'active',
      question_snapshot: scenarioQuestion(),
    })
  })

  await page.route('**/api/v1/scenarios/sessions/e2e-agent-session/messages', async (route) => {
	const runEvents = scenarioRunEvents()
	await fulfillSSE(route, [
	  ...runEvents.map((event) => ['run_event', event] as [string, unknown]),
	  ['finish', {
		message: {
          id: 'agent-message-1',
          session_id: 'e2e-agent-session',
          turn_number: 1,
          role: 'assistant',
          user_content: '我想先看日志和发布时间',
		  assistant_content: '你获得了一条有效线索：异常开始时间与一次配置发布高度重合。',
		  response_meta: {
			response_type: 'mentor_reply',
			request_id: 'run-e2e',
			revision: 1,
			run_events: runEvents,
		  },
          created_at: new Date().toISOString(),
        },
		response_meta: {
		  response_type: 'mentor_reply',
		  request_id: 'run-e2e',
		  revision: 1,
		  run_events: runEvents,
		},
		run_events: runEvents,
        session_status: 'active',
        session: {
          id: 'e2e-agent-session',
          user_id: 'demo-user',
          question_id: 'scenario-agent',
          status: 'active',
          current_turn: 1,
          max_turns: 50,
          revealed_clue_ids: ['c1'],
          question_snapshot: scenarioQuestion(),
		  state_revision: 1,
          started_at: new Date().toISOString(),
          last_active_at: new Date().toISOString(),
        },
      }],
    ])
  })

  await page.route('**/api/v1/scenarios**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, { list: [scenarioQuestion()], total: 1 })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill('我想先看日志和发布时间')
  await page.getByRole('button', { name: '发送' }).click()

  await expect(page.getByTestId('scenario-agent-run')).toContainText('异常开始时间与一次配置发布高度重合')
  // 内部机器汇报自己的工作对学生零信息量，还暴露了安全机制的存在——事件仍在
  // SSE 中下发（后端强制每轮一条 guard_passed），但不渲染。
  for (const machineChatter of ['回复已通过安全校验', '导师回复已整理', '本轮状态已确认', '正在整理公开观察']) {
    await expect(page.locator('.message-thread')).not.toContainText(machineChatter)
  }
  // 本轮理解摘要不再折叠：它是学生判断"Agent 到底听懂没有"的唯一依据，
  // 而且现在来自模型真实产出的 public_summary，不是每轮一样的固定文案。
  await expect(page.getByTestId('agent-run-understanding')).toContainText('已识别你希望核对日志与发布时间。')
  await expect(page.getByRole('button', { name: /调用工具/ })).toHaveCount(0)
  await expect(page.locator('.message-thread')).not.toContainText('root_cause')
  await expect(page.locator('.message-thread')).not.toContainText('standard_procedure')
})

test('answer attempt shows the real compare_answer tool with public-only details', async ({ page }) => {
  const sessionId = 'e2e-answer-tool-session'
  const userContent = '我认为目前的证据还需要继续验证索引问题'
  const reply = '先补充一条能直接支撑该判断的公开观察。'
  const runEvents = [
    { request_id: 'run-answer-tool', sequence: 1, kind: 'user_message', status: 'completed', text: userContent },
    {
      request_id: 'run-answer-tool',
      sequence: 2,
      kind: 'reasoning_summary_completed',
      status: 'completed',
      reasoning: { stage: 'understanding_message', text: '已识别你正在提交一个待验证的答案表述。' },
    },
    {
      request_id: 'run-answer-tool',
      sequence: 3,
      kind: 'tool_started',
      status: 'started',
      tool: { name: 'compare_answer', redacted_arguments: {}, duration_ms: 0 },
    },
    {
      request_id: 'run-answer-tool',
      sequence: 4,
      kind: 'tool_result',
      status: 'completed',
      tool: {
        name: 'compare_answer',
        redacted_arguments: {},
        duration_ms: 18,
        result: {
          tool: 'compare_answer',
          status: 'completed',
          user_points: [userContent],
          support_status: 'needs_more_evidence',
          next_action: '继续补充能支撑这个结论的直接观察。',
        },
      },
    },
    {
      request_id: 'run-answer-tool',
      sequence: 5,
      kind: 'tool_completed',
      status: 'completed',
      tool: {
        name: 'compare_answer',
        redacted_arguments: {},
        duration_ms: 18,
        result: {
          tool: 'compare_answer',
          status: 'completed',
          user_points: [userContent],
          support_status: 'needs_more_evidence',
          next_action: '继续补充能支撑这个结论的直接观察。',
        },
      },
    },
    { request_id: 'run-answer-tool', sequence: 6, kind: 'mentor_buffered', status: 'completed', summary: '导师回复已整理。' },
    { request_id: 'run-answer-tool', sequence: 7, kind: 'guard_passed', status: 'completed', summary: '回复已通过安全校验。' },
    { request_id: 'run-answer-tool', sequence: 8, kind: 'proposal_approved', status: 'completed', summary: '本轮状态已确认。' },
    { request_id: 'run-answer-tool', sequence: 9, kind: 'reply_delta', status: 'running', text: reply },
    { request_id: 'run-answer-tool', sequence: 10, kind: 'turn_completed', status: 'completed', summary: '本轮排查已完成。' },
  ]

  await setupScenarioEntry(page, sessionId)
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}/messages`, async (route) => {
    await fulfillSSE(route, [
      ...runEvents.map((event) => ['run_event', event] as [string, unknown]),
      ['finish', scenarioFinishPayload(sessionId, userContent, reply, runEvents)],
    ])
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill(userContent)
  await page.getByRole('button', { name: '发送' }).click()

  const run = page.getByTestId('scenario-agent-run')
  const toolButton = run.getByRole('button', { name: /对比答案与已公开证据/ })
  await expect(toolButton).toContainText('已返回')
  await toolButton.click()
  await expect(run).toContainText('答案对比')
  await expect(run).toContainText('还需要更多直接观察')
  await expect(run).toContainText('继续补充能支撑这个结论的直接观察。')
  // compare_answer 已无参数：界面不出现任何 answer_attempt_id 字样。
  await expect(run).not.toContainText('answer_attempt_id')
  for (const forbidden of ['claim_alignment', 'completion_allowed', 'missing_evidence', 'correct', 'target']) {
    await expect(run).not.toContainText(forbidden)
  }
  const dimensions = await run.evaluate((element) => ({ clientWidth: element.clientWidth, scrollWidth: element.scrollWidth }))
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth + 1)
  const replyChunk = run.locator('span').filter({ hasText: reply }).last()
  await expect(replyChunk).toBeVisible()
  expect(await replyChunk.evaluate((element) => getComputedStyle(element).animationName)).toBe('none')
})

test('stream reconnect reuses request_id and resumes after the latest sequence without duplicates', async ({ page }) => {
  const sessionId = 'e2e-reconnect-session'
  const userContent = '继续检查公开观察'
  const reply = '断线后继续显示同一条已验证回复。'
  const requests: Array<Record<string, unknown>> = []
  let callCount = 0

  await setupScenarioEntry(page, sessionId)
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}/messages`, async (route) => {
    callCount++
    requests.push(JSON.parse(route.request().postData() ?? '{}') as Record<string, unknown>)
    if (callCount === 1) {
      await fulfillSSE(route, [
        ['run_event', { request_id: 'run-reconnect', sequence: 1, kind: 'user_message', status: 'completed', text: userContent }],
        ['run_event', {
          request_id: 'run-reconnect', sequence: 2, kind: 'reasoning_summary_completed', status: 'completed',
          reasoning: { stage: 'understanding_message', text: '已识别本轮公开排查方向。' },
        }],
        ['run_event', { request_id: 'run-reconnect', sequence: 3, kind: 'response_summary', status: 'completed', summary: '正在整理公开观察。' }],
      ])
      return
    }
    const remaining = [
      { request_id: 'run-reconnect', sequence: 3, kind: 'response_summary', status: 'completed', summary: '正在整理公开观察。' },
      { request_id: 'run-reconnect', sequence: 4, kind: 'mentor_buffered', status: 'completed', summary: '导师回复已整理。' },
      { request_id: 'run-reconnect', sequence: 5, kind: 'guard_passed', status: 'completed', summary: '回复已通过安全校验。' },
      { request_id: 'run-reconnect', sequence: 6, kind: 'proposal_approved', status: 'completed', summary: '本轮状态已确认。' },
      { request_id: 'run-reconnect', sequence: 7, kind: 'reply_delta', status: 'running', text: reply },
      { request_id: 'run-reconnect', sequence: 8, kind: 'turn_completed', status: 'completed', summary: '本轮排查已完成。' },
    ]
    await fulfillSSE(route, [
      ...remaining.map((event) => ['run_event', event] as [string, unknown]),
      ['finish', scenarioFinishPayload(sessionId, userContent, reply)],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill(userContent)
  await page.getByRole('button', { name: '发送' }).click()

  await expect(page.getByText(reply)).toBeVisible()
  expect(callCount).toBe(2)
  expect(requests[0].request_id).toBe(requests[1].request_id)
  expect(requests[1].after_sequence).toBe(3)
  const run = page.getByTestId('scenario-agent-run')
  // 重连去重：同一条摘要不能因为重放而渲染两次。
  await expect(run.getByText('已识别本轮公开排查方向。')).toHaveCount(1)
})

test('failed turn keeps the public error event and never renders a mentor reply', async ({ page }) => {
  const sessionId = 'e2e-failed-run-session'
  let callCount = 0
  await setupScenarioEntry(page, sessionId)
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}/messages`, async (route) => {
    callCount++
    await fulfillSSE(route, [
      ['run_event', { request_id: 'run-failed', sequence: 1, kind: 'user_message', status: 'completed', text: '测试失败边界' }],
      ['run_event', {
        request_id: 'run-failed', sequence: 2, kind: 'reasoning_summary_completed', status: 'completed',
        reasoning: { stage: 'understanding_message', text: '已接收本轮公开输入。' },
      }],
      ['run_event', {
        request_id: 'run-failed', sequence: 3, kind: 'turn_failed', status: 'failed',
        summary: '安全校验未通过，本轮未发布导师正文。', error_code: 'reply_guard_rejected',
      }],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill('测试失败边界')
  await page.getByRole('button', { name: '发送' }).click()

  await expect(page.getByText('安全校验未通过，本轮未发布导师正文。').first()).toBeVisible()
  expect(callCount).toBe(1)
  const run = page.getByTestId('scenario-agent-run')
  await expect(run.getByTestId('agent-run-reply')).toHaveCount(0)
  await expect(run).not.toContainText('UNSAFE_HALF_REPLY')
})

test('failed turn retracts reply text that already streamed to the screen', async ({ page }) => {
  // 上一个用例的 mock 流里没有 reply_delta，盖不到"正文已经流出、随后整轮失败"
  // 这个真实分支——那正是泄露发生的场景：分片已经上屏，收不回来。
  const sessionId = 'e2e-failed-after-delta-session'
  await setupScenarioEntry(page, sessionId)
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}/messages`, async (route) => {
    await fulfillSSE(route, [
      ['run_event', { request_id: 'run-late-fail', sequence: 1, kind: 'user_message', status: 'completed', text: '测试迟到失败' }],
      ['run_event', {
        request_id: 'run-late-fail', sequence: 2, kind: 'reasoning_summary_completed', status: 'completed',
        reasoning: { stage: 'understanding_message', text: '已接收本轮公开输入。' },
      }],
      ['run_event', { request_id: 'run-late-fail', sequence: 10001, kind: 'reply_delta', status: 'running', text: '真正根因是 ' }],
      ['run_event', { request_id: 'run-late-fail', sequence: 10002, kind: 'reply_delta', status: 'running', text: 'LEAKED_SECRET_VALUE' }],
      ['run_event', {
        request_id: 'run-late-fail', sequence: 10003, kind: 'turn_failed', status: 'failed',
        summary: '安全校验未通过，本轮未发布导师正文。', error_code: 'reply_guard_rejected',
      }],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill('测试迟到失败')
  await page.getByRole('button', { name: '发送' }).click()

  await expect(page.getByText('安全校验未通过，本轮未发布导师正文。').first()).toBeVisible()
  const run = page.getByTestId('scenario-agent-run')
  await expect(run.getByTestId('agent-run-reply')).toHaveCount(0)
  await expect(page.locator('.message-thread')).not.toContainText('LEAKED_SECRET_VALUE')
})

test('v2 run events render task list, quick actions and argument-free tool results', async ({ page }) => {
  const sessionId = 'e2e-v2-run-session'
  const userContent = '我想先看网关日志'
  const reply = '先从网关侧的公开观察入手。'
  const v2 = (sequence: number, kind: string, payload: Record<string, unknown>) => ({
    request_id: 'run-v2-e2e',
    sequence,
    kind,
    schema_version: 'hiddenworld.v2',
    state_revision: 1,
    payload,
  })
  const runEvents = [
    v2(1, 'turn_started', { turn_id: 'run-v2-e2e' }),
    v2(2, 'assistant_delta', { phase: 'understanding', markdown_ready_delta: '你想先核对网关侧的访问日志。' }),
    v2(3, 'task_upserted', { task: { task_id: 'task-logs', call_id: 'obs:inspect:logs.gateway', title: '查询网关访问日志', state: 'running', tool_ref: 'inspect:logs.gateway' } }),
    v2(4, 'task_upserted', { task: { task_id: 'task-metrics', call_id: 'obs:inspect:metrics.gateway', title: '查询网关指标', state: 'running', tool_ref: 'inspect:metrics.gateway' } }),
    v2(5, 'tool_result', { tool_result: { call_id: 'obs:inspect:logs.gateway', tool_id: 'inspect:logs.gateway', tool_kind: 'logs', result_status: 'succeeded', duration_ms: 14, content: { content_type: 'observation', markdown_ready: '10:00-10:20 网关 504 数量明显上升。', display_variant: 'log', meta: { tool_kind: 'logs' } } } }),
    v2(6, 'task_upserted', { task: { task_id: 'task-logs', state: 'completed' } }),
    v2(7, 'tool_result', { tool_result: { call_id: 'obs:inspect:metrics.gateway', tool_id: 'inspect:metrics.gateway', tool_kind: 'metrics', result_status: 'succeeded', duration_ms: 9, content: { content_type: 'observation', markdown_ready: '网关活跃连接数处于正常区间。', display_variant: 'tool_return', meta: { tool_kind: 'metrics', is_negative: true } } } }),
    v2(8, 'task_upserted', { task: { task_id: 'task-metrics', state: 'completed' } }),
    v2(9, 'assistant_delta', { phase: 'replying', markdown_ready_delta: reply }),
    // 兼容旧会话曾把所有标题落成“可选检查”：展示层必须按 action_id
    // 恢复具体对象名，不能让一组 QuickAction 退化成同一个占位词。
    v2(10, 'turn_completed', {
      next_actions: [
        { action_id: 'inspect:logs.callback_timeout', catalog_version: 'catalog-v2', tool_kind: 'logs', title: '可选检查' },
        { action_id: 'inspect:change.gateway_release', catalog_version: 'catalog-v2', tool_kind: 'config', title: '可选检查' },
        { action_id: 'inspect:config.route_diff', catalog_version: 'catalog-v2', tool_kind: 'config', title: '可选检查' },
        { action_id: 'inspect:database.order_write', catalog_version: 'catalog-v2', tool_kind: 'database', title: '可选检查' },
        { action_id: 'inspect:database.slow_query', catalog_version: 'catalog-v2', tool_kind: 'database', title: '可选检查' },
      ],
    }),
  ]
  let quickActionBody: Record<string, unknown> | null = null
  await setupScenarioEntry(page, sessionId)
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}/messages`, async (route) => {
    const request = route.request()
    if (request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      if (body.structured_user_action) {
        quickActionBody = body
        await fulfillSSE(route, [
          ['run_event', v2(1, 'turn_started', { turn_id: 'run-v2-quick' })],
          ['run_event', v2(2, 'tool_result', { tool_result: { call_id: 'obs:inspect:config.route_diff', tool_id: 'inspect:config.route_diff', tool_kind: 'config', result_status: 'succeeded', duration_ms: 8, content: { content_type: 'observation', markdown_ready: '路由配置里 canary 权重为 100。', display_variant: 'tool_return', meta: { tool_kind: 'config' } } } })],
          ['run_event', v2(3, 'assistant_delta', { phase: 'replying', markdown_ready_delta: '已按你的选择查看了路由配置。' })],
          ['run_event', v2(4, 'turn_completed', {})],
        ])
        return
      }
    }
    await fulfillSSE(route, [
      ...runEvents.map((event) => ['run_event', event] as [string, unknown]),
      ['finish', scenarioFinishPayload(sessionId, userContent, reply, runEvents)],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).click()
  await page.getByPlaceholder('输入你的排查提问...').fill(userContent)
  await page.getByRole('button', { name: '发送' }).click()

  const run = page.getByTestId('scenario-agent-run')
  // Task List：两个任务触发内嵌列表，完成后保留摘要。
  await expect(run.getByTestId('agent-run-task-list')).toBeVisible()
  await expect(run.getByTestId('agent-run-task-list')).toContainText('已完成 2 项检查')
  // observation/clue 只渲染 markdown_ready。
  await expect(run).toContainText('10:00-10:20 网关 504 数量明显上升。')
  await expect(run).toContainText('网关活跃连接数处于正常区间。')
  // QuickActions：turn_completed 下发结构化动作。
  const quickActions = page.getByTestId('agent-run-quick-actions')
  for (const label of [
    '回调访问日志',
    '网关 VIP 发布记录',
    '网关 VIP 后端池与路由差异',
    '订单库回调写入日志',
    'MySQL 慢查询日志',
  ]) {
    await expect(quickActions.getByRole('button', { name: label })).toBeVisible()
  }
  await expect(quickActions).not.toContainText('可选检查')
  const quickAction = quickActions.getByRole('button', { name: '网关 VIP 后端池与路由差异' })
  await expect(quickAction).toBeVisible()
  await quickAction.click()
  // QuickAction 产生新一轮 run：断言落在最新一条上。
  const latestRun = page.getByTestId('scenario-agent-run').last()
  await expect(latestRun).toContainText('路由配置里 canary 权重为 100。')
  // 点击产生 StructuredUserAction：空正文 + action_id + catalog_version。
  expect(quickActionBody).not.toBeNull()
  expect((quickActionBody as Record<string, unknown>).content).toBe('')
  expect(((quickActionBody as Record<string, unknown>).structured_user_action as Record<string, unknown>).action_id).toBe('inspect:config.route_diff')
  // 界面不出现身份文字与内部阶段事件。
  for (const identity of ['排查导师', 'Mentor', 'guard_passed', 'mentor_buffered', 'proposal_approved']) {
    await expect(page.locator('.message-thread')).not.toContainText(identity)
  }
})

test('student can review interview history questions and reports from launchpad', async ({ page }) => {
  const historySessions = [
    {
      id: 'e2e-history-final',
      user_id: 'demo-user',
      question_id: 'interview-history-question',
      status: 'final_evaluated',
      current_round: 2,
      max_rounds: 3,
      submissions: [{ round: 1, content: '我会先看慢查询日志。', type: 'text', submitted_at: new Date().toISOString() }],
      evaluations: [interviewEvaluation()],
      final_score: 82,
      final_report: '继续沉淀线上定位路径。',
    },
    {
      id: 'e2e-history-active',
      user_id: 'demo-user',
      question_id: 'interview-active-question',
      status: 'question_presented',
      current_round: 1,
      max_rounds: 3,
      submissions: [],
      evaluations: [],
    },
  ]

  await page.route('**/api/v1/users/me/history', async (route) => {
    await fulfillJSON(route, { scenarios: [], interviews: historySessions, community_posts: [] })
  })
  await page.route('**/api/v1/interviews/sessions/e2e-history-final', async (route) => {
    await fulfillJSON(route, {
      session: historySessions[0],
      question: {
        ...interviewQuestion(),
        id: 'interview-history-question',
        title: '历史面试题目：MySQL 慢查询定位',
        description: '请说明定位路径、关键命令、修复和回滚考虑。',
      },
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')

  const historyPanel = page.getByTestId('interview-history-panel')
  await expect(historyPanel).toContainText('历史面试')
  await expect(historyPanel.locator('.panel-title')).toHaveCSS('color', 'rgb(125, 211, 252)')
  await expect(historyPanel).toContainText('最终评价')
  await expect(historyPanel).toContainText('82 分')
  await expect(historyPanel).not.toContainText('请说明定位路径')

  await historyPanel.getByRole('button', { name: '查看题目' }).first().click()
  await expect(historyPanel).toContainText('历史面试题目：MySQL 慢查询定位')
  await expect(historyPanel).toContainText('请说明定位路径、关键命令、修复和回滚考虑。')

  await historyPanel.getByRole('link', { name: '历史报告' }).first().click()
  await expect(page).toHaveURL(/\/interviews\/session\/e2e-history-final\/report$/)
})

test('student sees streaming feedback while interview answer is evaluated', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, {
      session_id: 'e2e-interview-stream-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-interview-stream-session'), status: 'active', submissions: [], evaluations: [] },
    })
  })

  let releaseSubmit: (() => void) | undefined
  await page.route('**/api/v1/interviews/sessions/e2e-interview-stream-session/submit', async (route) => {
    await new Promise<void>((resolve) => {
      releaseSubmit = resolve
    })
    await fulfillSSE(route, [
      ['stage', { message: 'streaming feedback', step: 'llm' }],
      ['delta', { chunk: 'clear path, ', displayable: false }],
      ['delta', { chunk: '{"highlights":["json should stay hidden"]}', displayable: false }],
      ['delta', { chunk: '总分：86 分\n', displayable: true }],
      ['delta', { chunk: '亮点：定位路径清晰\n', displayable: true }],
      ['stage', { message: 'saving result', step: 'saving' }],
      ['finish', {
        evaluation: interviewEvaluation(),
        session_status: 'follow_up_1_presented',
        session: { ...interviewSession('e2e-interview-stream-session'), status: 'follow_up_1_presented', evaluations: [interviewEvaluation()] },
      }],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await expect(page.locator('.user-strip small')).toHaveText('用户')
  await expect(page.getByRole('button', { name: '隐藏全局导航' })).not.toHaveCSS('background-color', 'rgb(255, 255, 255)')
  await page.locator('button.primary-button').click()
  await page.locator('.answer-panel textarea').fill('Check slow logs, run EXPLAIN, then verify index coverage.')
  const submitButton = page.locator('.answer-panel button.primary-button')
  await submitButton.click()

  await expect(page.getByTestId('interview-stream-feedback')).toBeVisible()
  await expect(submitButton).toBeDisabled()
  releaseSubmit?.()
  await expect(page.getByTestId('interview-stream-feedback')).not.toContainText('highlights')
  await expect(page.getByTestId('interview-stream-feedback')).toContainText('总分')
  await expect(page.locator('.metric-row.compact-metrics')).toBeVisible()
})

test('interview session keeps follow-up question close to the answer editor', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, {
      session_id: 'e2e-interview-layout-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-interview-layout-session'), status: 'active', submissions: [], evaluations: [] },
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-interview-layout-session/submit', async (route) => {
    await fulfillSSE(route, [
      ['finish', {
        evaluation: { ...interviewEvaluation(), follow_up_triggered: true },
        session_status: 'follow_up_1_presented',
        session: {
          ...interviewSession('e2e-interview-layout-session'),
          status: 'follow_up_1_presented',
          current_round: 2,
          follow_up_question: '追问：如果 EXPLAIN 仍然显示走错索引，你下一步怎么验证并回滚？',
          evaluations: [{ ...interviewEvaluation(), follow_up_triggered: true }],
        },
      }],
    ])
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.locator('button.primary-button').click()

  await expect(page.getByTestId('answer-template-grid')).toBeHidden()

  await page.locator('.answer-panel textarea').fill('Check slow logs, run EXPLAIN, then verify index coverage.')
  await page.getByTestId('submit-interview-answer').click()

  const followUp = page.getByText('追问：如果 EXPLAIN 仍然显示走错索引，你下一步怎么验证并回滚？')
  await expect(followUp).toBeVisible()
  await expect(page.locator('.answer-panel')).toBeVisible()

  const verticalGap = await page.evaluate(() => {
    const question = document.querySelector('.interview-question')?.getBoundingClientRect()
    const answer = document.querySelector('.answer-panel')?.getBoundingClientRect()
    if (!question || !answer) return Number.POSITIVE_INFINITY
    return answer.top - question.bottom
  })
  expect(verticalGap).toBeLessThanOrEqual(24)
})

async function fulfillJSON(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

async function setupScenarioEntry(page: Page, sessionId: string) {
  await page.route('**/api/v1/scenarios/scenario-agent/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: sessionId,
      status: 'active',
      question_snapshot: scenarioQuestion(),
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, { list: [scenarioQuestion()], total: 1 })
  })
}

function scenarioFinishPayload(
  sessionId: string,
  userContent: string,
  reply: string,
  runEvents?: unknown[],
) {
  return {
    message: {
      id: `${sessionId}-message`,
      session_id: sessionId,
      turn_number: 1,
      role: 'assistant',
      user_content: userContent,
      assistant_content: reply,
      response_meta: {
        response_type: 'mentor_reply',
        request_id: 'run-finish',
        revision: 1,
        ...(runEvents ? { run_events: runEvents } : {}),
      },
      created_at: new Date().toISOString(),
    },
    response_meta: {
      response_type: 'mentor_reply',
      request_id: 'run-finish',
      revision: 1,
      ...(runEvents ? { run_events: runEvents } : {}),
    },
    ...(runEvents ? { run_events: runEvents } : {}),
    session_status: 'active',
    session: {
      id: sessionId,
      user_id: 'demo-user',
      question_id: 'scenario-agent',
      status: 'active',
      current_turn: 1,
      max_turns: 50,
      revealed_clue_ids: [],
      question_snapshot: scenarioQuestion(),
      state_revision: 1,
      started_at: new Date().toISOString(),
      last_active_at: new Date().toISOString(),
    },
  }
}

function scenarioQuestion() {
  return {
    id: 'scenario-agent',
    title: 'E2E Agent 排查题',
    description: '发布后接口错误率升高，需要逐步定位。',
    domain: 'database',
    difficulty: 'L3',
    scenario_type: 'performance',
    tags: ['变更', '日志'],
    content: {
      reveal_strategy: { surface_clues: [], deep_clues: [], distractors: [] },
      architecture_diagram: 'graph TD\nA[API] --> B[(DB)]',
      reference_links: [],
    },
    status: 'active',
    source: 'seed',
    created_by: 'user-admin',
    version: 1,
    is_sanitized: true,
  }
}

function scenarioRunEvents() {
	return [
	  { request_id: 'run-e2e', sequence: 1, kind: 'user_message', status: 'completed', text: '我想先看日志和发布时间' },
	  {
		request_id: 'run-e2e',
		sequence: 2,
		kind: 'reasoning_summary_completed',
		status: 'completed',
		reasoning: { stage: 'understanding_message', text: '已识别你希望核对日志与发布时间。' },
	  },
	  { request_id: 'run-e2e', sequence: 3, kind: 'mentor_buffered', status: 'completed', summary: '导师回复已整理。' },
	  { request_id: 'run-e2e', sequence: 4, kind: 'guard_passed', status: 'completed', summary: '回复已通过安全校验。' },
	  { request_id: 'run-e2e', sequence: 5, kind: 'proposal_approved', status: 'completed', summary: '本轮状态已确认。' },
	  { request_id: 'run-e2e', sequence: 6, kind: 'reply_delta', status: 'running', text: '你获得了一条有效线索：' },
	  { request_id: 'run-e2e', sequence: 7, kind: 'reply_delta', status: 'running', text: '异常开始时间与一次配置发布高度重合。' },
	  { request_id: 'run-e2e', sequence: 8, kind: 'turn_completed', status: 'completed', summary: '本轮排查已完成。' },
	]
}

async function fulfillSSE(route: Route, events: Array<[string, unknown]>) {
  await route.fulfill({
    contentType: 'text/event-stream',
    body: events.map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join(''),
  })
}

function interviewQuestion() {
  return {
    id: 'e2e-question',
    title: 'E2E database interview',
    description: 'Explain how to locate a slow MySQL query.',
    domain: 'database',
    difficulty: 'L3',
    question_type: 'scenario_analysis',
    evaluation_dimensions: [],
    follow_up_strategies: [],
  }
}

function interviewSession(sessionId: string) {
  return {
    id: sessionId,
    user_id: 'demo-user',
    question_id: 'e2e-question',
    status: 'active',
    current_round: 1,
    max_rounds: 2,
    submissions: [],
    evaluations: [],
  }
}

function interviewEvaluation() {
  return {
    round: 1,
    total_score: 86,
    dimension_scores: {
      technical_accuracy: 88,
      logical_completeness: 82,
      solution_feasibility: 86,
    },
    is_passed: true,
    highlights: ['clear path'],
    deficiencies: ['rollback verification can be more specific'],
    follow_up_triggered: false,
    created_at: new Date().toISOString(),
  }
}
