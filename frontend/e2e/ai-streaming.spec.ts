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
  await expect(page.getByText('回复已通过安全校验')).toBeVisible()
  await page.getByRole('button', { name: /查看处理摘要/ }).click()
  await expect(page.getByText('已识别你希望核对日志与发布时间。')).toBeVisible()
  await expect(page.getByRole('button', { name: /调用工具/ })).toHaveCount(0)
  await expect(page.locator('.message-thread')).not.toContainText('root_cause')
  await expect(page.locator('.message-thread')).not.toContainText('standard_procedure')
})

test('answer attempt shows the real compare_answer tool with public-only details', async ({ page }) => {
  const sessionId = 'e2e-answer-tool-session'
  const userContent = '我认为目前的证据还需要继续验证索引问题'
  const reply = '先补充一条能直接支撑该判断的公开观察。'
  const attemptId = `answer-attempt-${'x'.repeat(80)}`
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
      tool: { name: 'compare_answer', redacted_arguments: { answer_attempt_id: attemptId }, duration_ms: 0 },
    },
    {
      request_id: 'run-answer-tool',
      sequence: 4,
      kind: 'tool_result',
      status: 'completed',
      tool: {
        name: 'compare_answer',
        redacted_arguments: { answer_attempt_id: attemptId },
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
        redacted_arguments: { answer_attempt_id: attemptId },
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
  const toolButton = run.getByRole('button', { name: /调用工具 compare_answer/ })
  await expect(toolButton).toContainText('已完成')
  await toolButton.click()
  await expect(run).toContainText('执行状态')
  await expect(run).toContainText('已完成')
  await expect(run).toContainText(`answer_attempt_id: ${attemptId}`)
  await expect(run).toContainText('还需要更多直接观察')
  await expect(run).toContainText('继续补充能支撑这个结论的直接观察。')
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
  await run.getByRole('button', { name: /查看处理摘要/ }).click()
  await expect(run.getByText('已识别本轮公开排查方向。')).toHaveCount(1)
  await expect(run.getByText('正在整理公开观察')).toHaveCount(1)
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
