import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('student sees diagnostic agent stages during scenario troubleshooting', async ({ page }) => {
  await page.route('**/api/v1/scenarios/scenario-agent/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'e2e-agent-session',
      status: 'active',
      question_snapshot: scenarioQuestion(),
    })
  })

  await page.route('**/api/v1/scenarios/sessions/e2e-agent-session/messages', async (route) => {
    await fulfillSSE(route, [
      ['stage', { step: 'agent_intent', message: '正在分析你的排查意图' }],
      ['stage', { step: 'agent_policy', message: '正在检查是否会泄露根因' }],
      ['stage', { step: 'agent_clue', message: '正在匹配可释放线索' }],
      ['stage', { step: 'agent_reply', message: '正在生成教学化回复' }],
      ['finish', {
        message: {
          id: 'agent-message-1',
          session_id: 'e2e-agent-session',
          turn_number: 1,
          role: 'assistant',
          user_content: '我想先看日志和发布时间',
          assistant_content: '你获得了一条有效线索：异常开始时间与一次配置发布高度重合。',
          response_meta: {
            response_type: 'partial',
            revealed_clue_id: 'c1',
            hint_level: 1,
            is_answer_leak: false,
            is_distractor: false,
            is_sanitized: false,
            provider: 'mock',
            validated: true,
            agent_trace: agentTrace(),
          },
          created_at: new Date().toISOString(),
        },
        response_meta: {
          response_type: 'partial',
          revealed_clue_id: 'c1',
          hint_level: 1,
          is_answer_leak: false,
          is_distractor: false,
          is_sanitized: false,
          provider: 'mock',
          validated: true,
          agent_trace: agentTrace(),
        },
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
          hint_level: 1,
          no_new_clue_streak: 0,
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

  await expect(page.getByTestId('agent-stage-list')).toContainText('正在分析你的排查意图')
  await expect(page.getByTestId('agent-stage-list')).toContainText('正在匹配可释放线索')
  await expect(page.getByText('Agent 已执行 6 个安全步骤')).toBeVisible()
  await expect(page.locator('.message-thread')).not.toContainText('root_cause')
  await expect(page.locator('.message-thread')).not.toContainText('standard_procedure')
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

function agentTrace() {
  const now = new Date().toISOString()
  return {
    run_id: 'run-e2e',
    agent: 'diagnostic_agent',
    mode: 'server_react',
    tool_count: 6,
    started_at: now,
    finished_at: now,
    steps: [
      { name: 'detect_root_cause_leak', kind: 'tool', status: 'success', summary: '未发现直接结论泄露风险', started_at: now, ended_at: now },
      { name: 'find_triggered_clue', kind: 'tool', status: 'success', summary: '命中可释放线索', started_at: now, ended_at: now },
      { name: 'compute_hint', kind: 'tool', status: 'success', summary: '提示等级保持不变', started_at: now, ended_at: now },
      { name: 'build_context_summary', kind: 'tool', status: 'success', summary: '未达到上下文压缩阈值', started_at: now, ended_at: now },
      { name: 'rewrite_teaching_reply', kind: 'tool', status: 'success', summary: '回复已完成模型改写', started_at: now, ended_at: now },
      { name: 'safety_rewrite', kind: 'tool', status: 'success', summary: '回复通过安全检查', started_at: now, ended_at: now },
    ],
  }
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
