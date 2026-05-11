import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test.describe('排查工坊展示', () => {
  test('scenarios page presents the posterized troubleshooting workshop', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    await expect(page.locator('.scenarios-workshop-page')).toBeVisible()
    await expect(page.getByRole('heading', { name: '排查工坊' })).toBeVisible()
    await expect(page.getByText('Agent Guided Lab')).toBeVisible()
    await expect(page.locator('.scenario-workshop-metrics .metric')).toHaveCount(4)
    await expect(page.getByText('故障案例流')).toBeVisible()
    await expect(page.locator('.scenario-workshop-board')).toBeVisible()
    await expect(page.locator('.scenario-agent-path')).toHaveCount(0)
    await expect(page.getByText('横向卡片保留标题、描述、领域、难度和训练入口；长描述在真实网页中保持稳定截断。')).toHaveCount(0)
  })

  test('scenario cards stay inside the board when viewport narrows', async ({ page }) => {
    await page.setViewportSize({ width: 1267, height: 715 })
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    const board = page.locator('.scenario-workshop-board')
    await expect(board).toBeVisible()

    const boardBox = await board.boundingBox()
    expect(boardBox).not.toBeNull()

    const cardBoxes = await page.locator('.scenario-card').evaluateAll((cards) =>
      cards.map((card) => {
        const rect = card.getBoundingClientRect()
        return { left: rect.left, right: rect.right }
      }),
    )

    for (const cardBox of cardBoxes) {
      expect(cardBox.left).toBeGreaterThanOrEqual(boardBox!.x - 1)
      expect(cardBox.right).toBeLessThanOrEqual(boardBox!.x + boardBox!.width + 1)
    }
  })

  test('first scenario card only uses the red accent while hovered', async ({ page }) => {
    await page.setViewportSize({ width: 1267, height: 715 })
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    const card = page.locator('.scenario-card').first()
    const cardIndex = card.locator('.scenario-card-topline > span').first()
    await expect(card).toBeVisible()

    const railBefore = await card.evaluate((node) => window.getComputedStyle(node, '::before').backgroundColor)
    expect(railBefore).toBe('rgb(142, 240, 219)')
    await expect.poll(
      async () => cardIndex.evaluate((node) => window.getComputedStyle(node).color),
    ).toBe('rgb(142, 240, 219)')

    await card.hover()
    await expect.poll(
      async () => card.evaluate((node) => window.getComputedStyle(node, '::before').backgroundColor),
    ).toBe('rgb(255, 90, 54)')
    await expect.poll(
      async () => cardIndex.evaluate((node) => window.getComputedStyle(node).color),
    ).toBe('rgb(255, 90, 54)')

    await page.mouse.move(8, 8)
    await expect.poll(
      async () => card.evaluate((node) => window.getComputedStyle(node, '::before').backgroundColor),
    ).toBe('rgb(142, 240, 219)')
    await expect.poll(
      async () => cardIndex.evaluate((node) => window.getComputedStyle(node).color),
    ).toBe('rgb(142, 240, 219)')
  })

  test('pending review metric uses a distinct accent color from the red hover emphasis', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    const pendingMetric = page.locator('.scenario-workshop-metrics .metric.tone-orange').first()
    await expect(pendingMetric).toBeVisible()

    const small = pendingMetric.locator('small')
    await expect.poll(
      async () => small.evaluate((node) => window.getComputedStyle(node).color),
    ).not.toBe('rgb(255, 90, 54)')
  })

  test('scenario filters keep domain direction separated on compact desktop', async ({ page }) => {
    await page.setViewportSize({ width: 1022, height: 715 })
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    const filters = page.locator('.scenario-filters')
    const primary = page.locator('.scenario-filter-primary')
    const secondary = page.locator('.scenario-filter-secondary')
    const tagRow = page.locator('.scenario-tag-filter-row')
    await expect(filters).toBeVisible()
    await expect(primary).toBeVisible()
    await expect(secondary).toBeVisible()
    await expect(tagRow).toBeVisible()
    await expect(primary.getByText('大方向')).toBeVisible()

    const primaryBox = await primary.boundingBox()
    const secondaryBox = await secondary.boundingBox()
    const tagRowBox = await tagRow.boundingBox()
    expect(primaryBox).not.toBeNull()
    expect(secondaryBox).not.toBeNull()
    expect(tagRowBox).not.toBeNull()
    expect(secondaryBox!.y).toBeGreaterThan(primaryBox!.y + primaryBox!.height - 2)
    expect(tagRowBox!.y).toBeGreaterThan(secondaryBox!.y + secondaryBox!.height - 2)

    const buttons = await primary.locator('.segmented button').evaluateAll((nodes) =>
      nodes.map((node) => {
        const rect = node.getBoundingClientRect()
        return {
          width: rect.width,
          height: rect.height,
          text: node.textContent?.trim() ?? '',
        }
      }),
    )

    expect(buttons.length).toBeGreaterThan(4)
    for (const button of buttons) {
      expect(button.width).toBeGreaterThan(42)
      expect(button.height).toBeLessThan(62)
      expect(button.height / button.width).toBeLessThan(1.05)
      expect(button.text.length).toBeGreaterThan(0)
    }
  })

  test('tag text filter does not render a redundant clear button when reset filters already exists', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    await page.getByPlaceholder('输入或点选标签').fill('索引')

    await expect(page.getByRole('button', { name: '重置筛选' })).toBeVisible()
    await expect(page.getByRole('button', { name: '清空标签筛选' })).toHaveCount(0)
  })

  test('pagination is centered at the bottom of the scenario board', async ({ page }) => {
    await page.setViewportSize({ width: 1267, height: 715 })
    await loginAs(page, 'admin')
    await page.goto('/scenarios')

    const board = page.locator('.scenario-workshop-board')
    const pagination = board.locator('.pagination-bar')
    await expect(pagination).toBeVisible()

    await expect(page.locator('.scenario-workshop-layout > .pagination-bar')).toHaveCount(0)

    const boardBox = await board.boundingBox()
    const paginationBox = await pagination.boundingBox()
    expect(boardBox).not.toBeNull()
    expect(paginationBox).not.toBeNull()

    const boardCenter = boardBox!.x + boardBox!.width / 2
    const paginationCenter = paginationBox!.x + paginationBox!.width / 2
    expect(Math.abs(boardCenter - paginationCenter)).toBeLessThanOrEqual(2)
    expect(paginationBox!.y).toBeGreaterThan(boardBox!.y + boardBox!.height * 0.70)
    expect(paginationBox!.y + paginationBox!.height).toBeLessThanOrEqual(boardBox!.y + boardBox!.height + 1)
  })

  test('scenario cards show check only after troubleshooting and answer submission', async ({ page }) => {
    const completedQuestion = scenarioQuestion('e2e-history-completed', 'E2E 已提交答案题目')
    const activeOnlyQuestion = scenarioQuestion('e2e-history-active-only', 'E2E 仅开始排查题目')
    const evaluatedWithoutAnswerQuestion = scenarioQuestion('e2e-history-no-answer', 'E2E 无答案评估题目')

    await page.route('**/api/v1/scenarios**', async (route) => {
      const url = new URL(route.request().url())
      if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
        await route.fallback()
        return
      }
      await fulfill(route, {
        list: [completedQuestion, activeOnlyQuestion, evaluatedWithoutAnswerQuestion],
        total: 3,
      })
    })
    await page.route('**/api/v1/users/me/history', async (route) => {
      await fulfill(route, {
        scenarios: [
          scenarioSession('session-completed', completedQuestion, {
            status: 'evaluated',
            user_answer: '根因是连接池耗尽，已提交最终答案。',
            evaluation_result: { is_correct: true, match_degree: 0.86, missing_points: [], standard_procedure: [] },
            score: { efficiency: 85, accuracy: 88, clue_usage: 80, total: 86 },
            ended_at: new Date().toISOString(),
          }),
          scenarioSession('session-active-only', activeOnlyQuestion, {
            status: 'active',
          }),
          scenarioSession('session-no-answer', evaluatedWithoutAnswerQuestion, {
            status: 'evaluated',
          }),
        ],
        interviews: [],
        community_posts: [],
      })
    })

    await loginAs(page, 'student')
    await page.goto('/scenarios')

    const completedCard = page.getByTestId('scenario-card-e2e-history-completed')
    const activeOnlyCard = page.getByTestId('scenario-card-e2e-history-active-only')
    const noAnswerCard = page.getByTestId('scenario-card-e2e-history-no-answer')

    await expect(completedCard.getByLabel('已排查并提交答案')).toBeVisible()
    await expect(completedCard.locator('.scenario-complete-symbol')).toHaveText('√')
    await expect(completedCard.getByLabel('已排查并提交答案')).toContainText('86分')
    await expect(completedCard).toContainText('再次排查')
    await expect(activeOnlyCard.getByLabel('已排查并提交答案')).toHaveCount(0)
    await expect(noAnswerCard.getByLabel('已排查并提交答案')).toHaveCount(0)
  })
})

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '用于验证排查历史完成标记的真实故障情景。',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['E2E', 'history'],
    content: {
      reveal_strategy: {
        surface_clues: [],
        deep_clues: [],
        distractors: [],
      },
      architecture_diagram: '',
      reference_links: [],
    },
    status: 'active',
    source: 'seed',
    created_by: 'demo-user',
    version: 1,
    is_sanitized: false,
  }
}

function scenarioSession(
  id: string,
  question: ReturnType<typeof scenarioQuestion>,
  patch: Partial<Record<string, unknown>>,
) {
  return {
    id,
    user_id: 'demo-user',
    question_id: question.id,
    status: 'active',
    current_turn: 0,
    max_turns: 50,
    revealed_clue_ids: [],
    question_snapshot: question,
    hint_level: 0,
    no_new_clue_streak: 0,
    started_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    ...patch,
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}
