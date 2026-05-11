import { expect, type Page, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('scenario workspace supports hiding global navigation and resizing work areas', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 760 })
  await mockStudentLogin(page)
  const question = scenarioQuestion('e2e-session-workspace-question', 'E2E 会话工作区题目')

  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [question], total: 1 })
  })
  await page.route('**/api/v1/scenarios/e2e-session-workspace-question/sessions', async (route) => {
    await fulfill(route, {
      session_id: 'e2e-session-workspace-session',
      status: 'active',
      question_snapshot: question,
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /开始排查/ }).first().click()

  await expect(page.getByTestId('global-sidebar')).toBeVisible()
  await page.getByRole('button', { name: '隐藏全局导航' }).click()
  await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/)
  await expect(page.getByTestId('global-sidebar')).toBeHidden()

  await page.getByRole('button', { name: '显示全局导航' }).click()
  await expect(page.locator('.app-shell')).not.toHaveClass(/sidebar-collapsed/)
  await expect(page.getByTestId('global-sidebar')).toBeVisible()

  const contextPane = page.getByTestId('session-context-pane')
  const contextHandle = page.getByTestId('session-context-resizer')
  await expect(contextPane.getByTestId('scenario-difficulty-badge')).toHaveText('难度 L2')
  await expect(page.getByRole('button', { name: '隐藏题目快照' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '显示题目快照' })).toHaveCount(0)
  const contextBefore = await contextPane.boundingBox()
  const contextHandleBox = await contextHandle.boundingBox()
  expect(contextBefore).not.toBeNull()
  expect(contextHandleBox).not.toBeNull()
  await page.mouse.move(contextHandleBox!.x + contextHandleBox!.width / 2, contextHandleBox!.y + contextHandleBox!.height / 2)
  await page.mouse.down()
  await page.mouse.move(contextHandleBox!.x - 90, contextHandleBox!.y + contextHandleBox!.height / 2)
  await page.mouse.up()
  const contextAfter = await contextPane.boundingBox()
  expect(contextAfter).not.toBeNull()
  expect(contextAfter!.width).toBeLessThan(contextBefore!.width - 40)

  const messageThread = page.getByTestId('session-message-thread')
  const threadBox = await messageThread.boundingBox()
  expect(threadBox).not.toBeNull()
  expect(threadBox!.height).toBeGreaterThan(300)

  await expect(page.getByTestId('scenario-answer-editor')).toHaveCount(0)
  await page.getByRole('button', { name: '展开最终答案区' }).click()
  await expect(page.getByTestId('scenario-answer-editor')).toBeVisible()

  const answerPanel = page.getByTestId('scenario-answer-panel')
  const answerHandle = page.getByTestId('scenario-answer-resizer')
  const answerBefore = await answerPanel.boundingBox()
  const answerHandleBox = await answerHandle.boundingBox()
  expect(answerBefore).not.toBeNull()
  expect(answerHandleBox).not.toBeNull()
  await page.mouse.move(answerHandleBox!.x + answerHandleBox!.width / 2, answerHandleBox!.y + answerHandleBox!.height / 2)
  await page.mouse.down()
  await page.mouse.move(answerHandleBox!.x + answerHandleBox!.width / 2, answerHandleBox!.y - 90)
  await page.mouse.up()
  const answerAfter = await answerPanel.boundingBox()
  expect(answerAfter).not.toBeNull()
  expect(answerAfter!.height).toBeGreaterThan(answerBefore!.height + 40)
})

async function mockStudentLogin(page: Page) {
  const studentUser = {
    id: 'user-student',
    username: 'demo',
    email: 'demo@example.com',
    role: 'student',
    profile: {
      target_level: 'L3',
      preferred_domains: ['database'],
      capability_radar: {},
      weak_points: [],
      total_stats: { scenarios_solved: 0, interviews_taken: 0, average_score: 0, streak_days: 0 },
      updated_at: new Date().toISOString(),
    },
    created_at: new Date().toISOString(),
  }
  await page.route('**/api/v1/auth/login', async (route) => {
    await fulfill(route, {
      user: studentUser,
      access_token: 'e2e-student-token',
      refresh_token: 'e2e-student-refresh',
    })
  })
  await page.route('**/api/v1/users/me', async (route) => {
    await fulfill(route, studentUser)
  })
  await page.route('**/api/v1/system/ai', async (route) => {
    await fulfill(route, { provider: 'mock', model: 'mock', fallback: true })
  })
}

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '用于验证会话工作区布局的排查场景。',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['E2E'],
    content: {
      reveal_strategy: {
        surface_clues: [],
        deep_clues: [],
        distractors: [],
      },
      architecture_diagram: [
        'graph TD',
        '  Client["Client"] --> Gateway["API Gateway"]',
        '  Gateway --> DB["PostgreSQL"]',
      ].join('\n'),
      reference_links: [],
    },
    status: 'active',
    source: 'seed',
    created_by: 'demo-user',
    version: 1,
    is_sanitized: false,
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}
