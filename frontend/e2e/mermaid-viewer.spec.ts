import { expect, type Page, type Route, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs } from './helpers/auth'

test('scenario mermaid viewer supports source and fullscreen inspection', async ({ page }) => {
  await mockStudentLogin(page)
  const question = scenarioQuestion('e2e-mermaid-viewer-question', 'E2E Mermaid 查看器题目')
  await mockScenarioSessionDetail(page, 'e2e-mermaid-viewer-session', question)

  await loginAs(page, 'student')
  await page.goto('/scenarios/session/e2e-mermaid-viewer-session')

  const viewer = page.getByTestId('mermaid-viewer').first()
  await expect(viewer).toBeVisible()
  await expect(viewer.locator('.mermaid-diagram svg')).toBeVisible()
  await expect(viewer.getByRole('button', { name: '放大图形' })).toHaveCount(0)
  await expect(viewer.getByRole('button', { name: '缩小图形' })).toHaveCount(0)
  await expect(viewer.getByRole('button', { name: '重置图形缩放' })).toHaveCount(0)

  await expect(viewer).toHaveCSS('resize', 'both')
  const beforeResize = await viewer.boundingBox()
  expect(beforeResize).not.toBeNull()
  await page.mouse.move(beforeResize!.x + beforeResize!.width - 3, beforeResize!.y + beforeResize!.height - 3)
  await page.mouse.down()
  await page.mouse.move(beforeResize!.x + beforeResize!.width + 90, beforeResize!.y + beforeResize!.height + 70)
  await page.mouse.up()
  const afterResize = await viewer.boundingBox()
  expect(afterResize).not.toBeNull()
  expect(afterResize!.width).toBeGreaterThanOrEqual(beforeResize!.width)
  expect(afterResize!.height).toBeGreaterThan(beforeResize!.height)

  await viewer.getByRole('button', { name: '查看源码' }).click()
  await expect(viewer.getByText('Gateway["API Gateway"] --> Redis["Redis"]')).toBeVisible()
  await expect(viewer.locator('.mermaid-diagram svg')).toHaveCount(0)

  await viewer.getByRole('button', { name: '全屏查看' }).click()
  const overlay = page.getByTestId('mermaid-fullscreen-viewer')
  await expect(overlay).toBeVisible()
  await expect(overlay.getByText('Gateway["API Gateway"] --> Redis["Redis"]')).toBeVisible()

  await overlay.getByRole('button', { name: '退出全屏' }).click()
  await expect(overlay).toHaveCount(0)
  await expectNoWhiteScreen(page)
})

test('scenario mermaid viewer shows loading before suggesting source on render failure', async ({ page }) => {
  await mockStudentLogin(page)
  const question = scenarioQuestion('e2e-mermaid-invalid-question', 'E2E Mermaid 失败态题目')
  question.content.architecture_diagram = [
    'graph TD',
    '  Broken["未闭合节点"',
    '  Broken --> DB["PostgreSQL"]',
  ].join('\n')

  await mockScenarioSessionDetail(page, 'e2e-mermaid-invalid-session', question)

  await loginAs(page, 'student')
  await page.goto('/scenarios/session/e2e-mermaid-invalid-session')

  const viewer = page.getByTestId('mermaid-viewer').first()
  await expect(viewer).toBeVisible()
  await expect(viewer.locator('.mermaid-loading')).toContainText('正在加载图形')
  await expect(viewer.getByText('暂不可渲染')).toHaveCount(0)
  await expect(viewer.getByText('图形渲染失败，建议查看源码。')).toBeVisible()

  await viewer.getByRole('button', { name: '查看源码' }).click()
  await expect(viewer.getByText('Broken["未闭合节点"')).toBeVisible()
  await expectNoWhiteScreen(page)
})

test('scenario mermaid viewer renders fallback graph from server without compact fallback', async ({ page }) => {
  await mockStudentLogin(page)
  const question = scenarioQuestion('e2e-mermaid-repaired-question', 'E2E Mermaid 服务端兜底题目')
  question.content.architecture_diagram = [
    'graph TD',
    'A["企业内网DNS解析故障排查"] --> B["network"]',
    'B --> C["用户访问内部应用返回 Server not found"]',
    'C --> D["先确认问题是否只影响内网域名解析"]',
  ].join('\n')
  question.content.diagram_status = 'fallback'
  question.content.diagram_warnings = ['mermaid square labels must not contain raw parentheses or braces']

  await mockScenarioSessionDetail(page, 'e2e-mermaid-repaired-session', question)

  await loginAs(page, 'student')
  await page.goto('/scenarios/session/e2e-mermaid-repaired-session')

  const viewer = page.getByTestId('mermaid-viewer').first()
  await expect(viewer).toBeVisible()
  await expect(page.getByText('架构图已自动简化')).toBeVisible()
  await expect(viewer.locator('.mermaid-fallback')).toHaveCount(0)
  await expect(viewer.locator('.mermaid-diagram svg')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('图形渲染失败')
  await expectNoWhiteScreen(page)
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
  await page.route('**/api/v1/users/me/dashboard', async (route) => {
    await fulfill(route, dashboardPayload(studentUser))
  })
  await page.route('**/api/v1/system/ai', async (route) => {
    await fulfill(route, { provider: 'mock', model: 'mock', fallback: true })
  })
}

async function mockScenarioSessionDetail(page: Page, sessionId: string, question: ReturnType<typeof scenarioQuestion>) {
  await page.route(`**/api/v1/scenarios/sessions/${sessionId}`, async (route) => {
    await fulfill(route, {
      session: {
        id: sessionId,
        user_id: 'user-student',
        question_id: question.id,
        status: 'active',
        current_turn: 0,
        max_turns: 50,
        revealed_clue_ids: [],
        question_snapshot: question,
        hint_level: 1,
        no_new_clue_streak: 0,
        started_at: new Date().toISOString(),
        last_active_at: new Date().toISOString(),
      },
      messages: [],
    })
  })
}

function dashboardPayload(user: {
  id: string
  username: string
  email: string
  role: string
  profile: {
    target_level: string
    preferred_domains: string[]
    capability_radar: Record<string, number>
    weak_points: unknown[]
    total_stats: { scenarios_solved: number; interviews_taken: number; average_score: number; streak_days: number }
    updated_at: string
  }
  created_at: string
}) {
  return {
    user,
    stats: user.profile.total_stats,
    capability_radar: user.profile.capability_radar,
    weak_points: user.profile.weak_points,
    recommendations: [],
    learning_plan: {
      generated_at: new Date().toISOString(),
      summary: 'E2E 默认仪表盘摘要',
      target_level: user.profile.target_level,
      focus_domains: user.profile.preferred_domains,
      domain_insights: [],
      recommendations: [],
      review_plan: [],
    },
    review_calendar: {
      generated_at: new Date().toISOString(),
      checkin_dates: [],
      streak_days: 0,
      today_checked: false,
      today: new Date().toISOString().slice(0, 10),
      review_plan: [],
      focus_domains: user.profile.preferred_domains,
      next_action: '完成一轮排查训练',
    },
  }
}

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '用于验证 Mermaid 图形查看能力的排查场景。',
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
        '  Gateway["API Gateway"] --> Redis["Redis"]',
        '  Redis --> DB["PostgreSQL"]',
      ].join('\n'),
      diagram_status: 'validated',
      diagram_warnings: [],
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
