import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('student can recover scenario session after page reload', async ({ page }) => {
  const question = scenarioQuestion('e2e-scenario-recovery-question', 'E2E 排查会话恢复题目')

  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [question], total: 1 })
  })

  await page.route('**/api/v1/scenarios/e2e-scenario-recovery-question/sessions', async (route) => {
    await fulfill(route, {
      session_id: 'e2e-scenario-recovery-session',
      status: 'active',
      question_snapshot: question,
    })
  })

  await page.route('**/api/v1/scenarios/sessions/e2e-scenario-recovery-session', async (route) => {
    await fulfill(route, {
      session: scenarioSession('e2e-scenario-recovery-session', question),
      messages: [{
        id: 'e2e-scenario-message-1',
        session_id: 'e2e-scenario-recovery-session',
        turn_number: 1,
        role: 'assistant',
        user_content: '先看慢查询日志。',
        assistant_content: '可以，先比对最近 30 分钟的慢查询与变更记录。',
        response_meta: {
          response_type: 'hint',
          hint_level: 1,
          is_answer_leak: false,
          is_distractor: false,
          is_sanitized: false,
        },
        created_at: new Date().toISOString(),
      }],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).first().click()
  await expect(page.getByText('渐进式排查会话')).toBeVisible()

  await page.reload()

  await expect(page.getByText('渐进式排查会话')).toBeVisible()
  await expect(page.getByText('E2E 排查会话恢复题目')).toBeVisible()
  await expect(page.getByText('可以，先比对最近 30 分钟的慢查询与变更记录。')).toBeVisible()
})

test('student can recover interview session after page reload', async ({ page }) => {
  const question = interviewQuestion()
  const session = interviewSession('e2e-interview-recovery-session')

  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: session.id,
      status: 'active',
      question,
      session,
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-interview-recovery-session', async (route) => {
    await fulfill(route, {
      session,
      question,
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.getByTestId('interview-track-section').getByRole('button', { name: '开始面试' }).click()
  await expect(page.getByText(question.title)).toBeVisible()

  await page.reload()

  await expect(page.getByText(question.title)).toBeVisible()
  await expect(page.locator('.scenario-meta')).toContainText(question.difficulty)
  await expect(page.locator('.answer-panel')).toContainText('回答')
})

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '用于刷新恢复验证的排查题目。',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['恢复', 'E2E'],
    content: {
      reveal_strategy: {
        surface_clues: [],
        deep_clues: [],
        distractors: [],
      },
      architecture_diagram: 'graph TD\nA[Client] --> B[Gateway]\nB --> C[PostgreSQL]',
      reference_links: [],
    },
    status: 'active',
    source: 'seed',
    created_by: 'demo-user',
    version: 1,
    is_sanitized: false,
  }
}

function scenarioSession(sessionId: string, question: ReturnType<typeof scenarioQuestion>) {
  return {
    id: sessionId,
    user_id: 'demo-user',
    question_id: question.id,
    status: 'active',
    current_turn: 1,
    max_turns: 50,
    revealed_clue_ids: [],
    question_snapshot: question,
    hint_level: 1,
    no_new_clue_streak: 0,
    started_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
  }
}

function interviewQuestion() {
  return {
    id: 'e2e-interview-recovery-question',
    title: 'E2E 面试会话恢复题目',
    description: '说明如何定位数据库慢查询并恢复服务。',
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
    question_id: 'e2e-interview-recovery-question',
    status: 'active',
    current_round: 1,
    max_rounds: 2,
    submissions: [],
    evaluations: [],
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

test('student can open scenario session directly without router state', async ({ page }) => {
  const question = scenarioQuestion('e2e-scenario-direct-question', 'E2E 直接访问排查会话')

  await page.route('**/api/v1/scenarios/sessions/e2e-scenario-direct-session', async (route) => {
    await fulfill(route, {
      session: scenarioSession('e2e-scenario-direct-session', question),
      messages: [],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios/session/e2e-scenario-direct-session')

  await expect(page.getByText('E2E 直接访问排查会话')).toBeVisible()
  await expect(page.getByText('渐进式排查会话')).toBeVisible()
})

test('scenario session uses the troubleshooting workshop dark style', async ({ page }) => {
  const question = scenarioQuestion('e2e-scenario-style-question', 'E2E 排查会话深色风格')

  await page.route('**/api/v1/scenarios/sessions/e2e-scenario-style-session', async (route) => {
    await fulfill(route, {
      session: scenarioSession('e2e-scenario-style-session', question),
      messages: [],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios/session/e2e-scenario-style-session')

  const layout = page.locator('.scenario-session-page')
  const contextPane = page.locator('.context-pane')
  const chatPane = page.locator('.chat-pane')
  await expect(layout).toBeVisible()
  await expect(contextPane).toBeVisible()
  await expect(chatPane).toBeVisible()

  await expect(contextPane).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(chatPane).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(contextPane.getByText('题目快照')).toHaveCSS('color', 'rgb(246, 250, 254)')
  await expect(chatPane.getByText('渐进式排查会话')).toHaveCSS('color', 'rgb(246, 250, 254)')
})

test('student can open interview session directly without router state', async ({ page }) => {
  const question = interviewQuestion()
  const session = interviewSession('e2e-interview-direct-session')

  await page.route('**/api/v1/interviews/sessions/e2e-interview-direct-session', async (route) => {
    await fulfill(route, {
      session,
      question,
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews/session/e2e-interview-direct-session')

  await expect(page.getByText(question.title)).toBeVisible()
  await expect(page.locator('.scenario-meta')).toContainText(question.difficulty)
})

test('interview session uses the dark workshop style', async ({ page }) => {
  const question = interviewQuestion()
  const session = interviewSession('e2e-interview-style-session')

  await page.route('**/api/v1/interviews/sessions/e2e-interview-style-session', async (route) => {
    await fulfill(route, {
      session,
      question,
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews/session/e2e-interview-style-session')

  const layout = page.locator('.interview-session-page')
  const questionPanel = page.locator('.interview-question')
  const answerPanel = page.locator('.answer-panel')
  await expect(layout).toBeVisible()
  await expect(questionPanel).toBeVisible()
  await expect(answerPanel).toBeVisible()

  await expect(layout).toHaveCSS('color', 'rgb(230, 237, 245)')
  await expect(questionPanel).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(answerPanel).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(questionPanel.getByText(question.title)).toHaveCSS('color', 'rgb(246, 250, 254)')
  await expect(answerPanel.locator('.panel-title')).toHaveCSS('color', 'rgb(246, 250, 254)')

  const questionVisualWeight = await questionPanel.evaluate((panel) => {
    const title = panel.querySelector('h2')
    const description = panel.querySelector('p')
    if (!title || !description) return null
    const titleStyle = getComputedStyle(title)
    const descriptionStyle = getComputedStyle(description)
    const titleBox = title.getBoundingClientRect()
    const descriptionBox = description.getBoundingClientRect()
    return {
      titleFontSize: Number.parseFloat(titleStyle.fontSize),
      titleLineHeight: Number.parseFloat(titleStyle.lineHeight),
      titleWeight: Number.parseInt(titleStyle.fontWeight, 10),
      titleHeight: titleBox.height,
      descriptionFontSize: Number.parseFloat(descriptionStyle.fontSize),
      descriptionLineHeight: Number.parseFloat(descriptionStyle.lineHeight),
      descriptionTop: descriptionBox.top,
      titleBottom: titleBox.bottom,
    }
  })
  expect(questionVisualWeight).not.toBeNull()
  expect(questionVisualWeight!.titleFontSize).toBeGreaterThanOrEqual(36)
  expect(questionVisualWeight!.titleWeight).toBeGreaterThanOrEqual(850)
  expect(questionVisualWeight!.titleLineHeight).toBeLessThanOrEqual(questionVisualWeight!.titleFontSize * 1.1)
  expect(questionVisualWeight!.descriptionFontSize).toBeGreaterThanOrEqual(16)
  expect(questionVisualWeight!.descriptionLineHeight).toBeGreaterThanOrEqual(26)
  expect(questionVisualWeight!.descriptionTop - questionVisualWeight!.titleBottom).toBeGreaterThanOrEqual(10)
})
