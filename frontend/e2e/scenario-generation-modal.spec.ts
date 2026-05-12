import { expect, type Route, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs } from './helpers/auth'

test('student can open scenario generation modal and restore draft fields', async ({ page }) => {
  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  const dialog = page.getByRole('dialog', { name: '约束生成情景题' })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('生成难度').selectOption('L3')
  await dialog.getByRole('button', { name: '显示高级约束' }).click()
  await dialog.getByLabel('标题约束').fill('学生草稿标题')
  await dialog.locator('.scenario-generation-footer').getByRole('button', { name: '关闭' }).click()

  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  await expect(dialog.getByLabel('生成难度')).toHaveValue('L3')
  await expect(dialog.getByLabel('标题约束')).toHaveValue('学生草稿标题')
})

test('scenario generation modal sends constraints payload and shows source badges', async ({ page }) => {
  let requestPayload: Record<string, unknown> = {}
  const generatedQuestion = {
    ...scenarioQuestion('e2e-generation-modal-question', 'E2E 约束生成题目'),
    difficulty: 'L4',
    status: 'active',
    source: 'llm_generated',
    created_by: 'user-admin',
    creator_role: 'admin',
  }

  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    requestPayload = route.request().postDataJSON() as Record<string, unknown>
    await fulfill(route, {
      job: aiJob('e2e-generation-modal-job', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-generation-modal-job', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-modal-job', 'completed', 100, {
        stage: 'persisted',
        validated: true,
        result_question_id: generatedQuestion.id,
      }),
      question_id: generatedQuestion.id,
      question: generatedQuestion,
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    if (url.searchParams.get('difficulty') === 'L4') {
      await fulfill(route, { list: [generatedQuestion], total: 1 })
      return
    }
    await fulfill(route, { list: [scenarioQuestion('e2e-existing-generation-question', 'E2E 初始题目')], total: 1 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  const dialog = page.getByRole('dialog', { name: '约束生成情景题' })
  await dialog.getByLabel('生成难度').selectOption('L4')
  await dialog.getByRole('button', { name: '显示高级约束' }).click()
  await dialog.getByLabel('标题约束').fill('E2E L4 约束标题')
  await dialog.getByLabel('描述约束').fill('E2E 约束描述')
  await dialog.getByLabel('细分主题').fill('主从复制\n读流量')
  await dialog.getByLabel('根因提示').fill('从库延迟')
  await dialog.getByLabel('证据提示').fill('Seconds_Behind_Master 上升\n读请求超时')
  await dialog.getByLabel('线索提示').fill('主库正常\n慢 SQL 不明显')
  await dialog.getByRole('button', { name: '开始生成' }).click()

  await expect(page.locator('.scenario-card').first()).toContainText('L4')
  await expect(page.locator('.scenario-card').first()).toContainText('AI生成')
  await expect(page.locator('.scenario-card').first()).toContainText('admin')
  expect(requestPayload.difficulty).toBe('L4')
  expect(requestPayload.scenario_type).toBe('troubleshooting')
  expect(requestPayload.tags).toBeUndefined()
  expect(requestPayload.constraints).toEqual({
    title: 'E2E L4 约束标题',
    description: 'E2E 约束描述',
    topic_scope: ['主从复制', '读流量'],
    root_cause_hint: '从库延迟',
    evidence_hints: ['Seconds_Behind_Master 上升', '读请求超时'],
    clue_hints: ['主库正常', '慢 SQL 不明显'],
  })
  await expectNoWhiteScreen(page)
})

test('scenario generation prepends result without changing list filters', async ({ page }) => {
  const scenarioQueries: string[] = []
  const generatedQuestion = {
    ...scenarioQuestion('e2e-generation-prepend-question', 'E2E 生成后置顶题目'),
    domain: 'security',
    difficulty: 'L5',
    tags: ['AI生成', '安全'],
    status: 'active',
    source: 'llm_generated',
  }
  const existingQuestion = {
    ...scenarioQuestion('e2e-generation-existing-question', 'E2E 原有题目仍可见'),
    domain: 'database',
    difficulty: 'L2',
    tags: ['E2E', '原有', '保留筛选'],
    status: 'active',
    source: 'seed',
  }

  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-prepend-job', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-generation-prepend-job', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-prepend-job', 'completed', 100, {
        stage: 'persisted',
        validated: true,
        result_question_id: generatedQuestion.id,
      }),
      question_id: generatedQuestion.id,
      question: generatedQuestion,
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    scenarioQueries.push(url.search)
    await fulfill(route, { list: [existingQuestion], total: 18 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.locator('.filter-controls').getByLabel('难度').selectOption('L2')
  await page.getByPlaceholder('输入或点选标签').fill('保留筛选')
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => scenarioQueries.at(-1) ?? '').toContain('page=2')

  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  await page.getByRole('button', { name: '开始生成' }).click()

  await expect(page.locator('.scenario-card').first()).toContainText('E2E 生成后置顶题目')
  await expect(page.locator('.scenario-card').nth(1)).toContainText('E2E 原有题目仍可见')
  await expect(page.locator('.filter-controls').getByLabel('难度')).toHaveValue('L2')
  await expect(page.getByPlaceholder('输入或点选标签')).toHaveValue('保留筛选')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').not.toContain('difficulty=L5')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').not.toContain('domain=security')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').not.toContain('tag=AI')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').toContain('difficulty=L2')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').toContain('tag=%E4%BF%9D%E7%95%99%E7%AD%9B%E9%80%89')
  await expect.poll(() => scenarioQueries.at(-1) ?? '').toContain('page=2')
})

test('scenario generation modal closes immediately after submit while job is still running', async ({ page }) => {
  let releaseJobCreation: (() => void) | null = null
  const waitForJobCreation = new Promise<void>((resolve) => {
    releaseJobCreation = resolve
  })

  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    await waitForJobCreation
    await fulfill(route, {
      job: aiJob('e2e-generation-modal-close-job', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-generation-modal-close-job', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-modal-close-job', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  const dialog = page.getByRole('dialog', { name: '约束生成情景题' })
  await dialog.getByRole('button', { name: '开始生成' }).click()

  await expect(dialog).toHaveCount(0, { timeout: 250 })
  releaseJobCreation?.()
  await expect(page.locator('.generation-status')).toContainText(/AI 正在生成情景题|AI 姝ｅ湪鐢熸垚鎯呮櫙棰?/)
})

test('scenario generation failure keeps job provider model and stage metadata', async ({ page }) => {
  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-failed-meta-job', 'running', 35, {
        provider: 'deepseek',
        model: 'deepseek-v4-flash',
        stage: 'calling_model',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-generation-failed-meta-job', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-generation-failed-meta-job', 'failed', 100, {
        provider: 'deepseek',
        model: 'deepseek-v4-flash',
        stage: 'validating_output',
        error_message: '模型返回结构未通过校验，请重新生成题目。',
      }),
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [scenarioQuestion('e2e-existing-failed-meta-question', 'E2E 初始题目')], total: 1 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /生成题目|鐢熸垚棰樼洰/ }).click()
  await page.getByRole('button', { name: '开始生成' }).click()

  const status = page.locator('.generation-status')
  await expect(status).toContainText('题目生成失败')
  await expect(status).toContainText('e2e-gene')
  await expect(status).toContainText('DeepSeek deepseek-v4-flash')
  await expect(status).toContainText('deepseek-v4-flash')
  await expect(status).toContainText('正在校验结构')
  await expect(status).not.toContainText('任务 未知')
  await expect(status).not.toContainText('未返回 provider/model/stage 信息')
})

test('scenario page keeps filter paging requests and fork feedback in sync', async ({ page }) => {
  const queries: string[] = []
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    queries.push(url.search)
    const pageValue = url.searchParams.get('page') ?? '1'
    await fulfill(route, {
      list: [scenarioQuestion(`scenario-page-${pageValue}`, `Scenario Page ${pageValue}`)],
      total: 18,
    })
  })
  await page.route('**/api/v1/scenarios/*/fork', async (route) => {
    await fulfill(route, {
      id: 'fork-draft-1',
      title: 'Fork 后草稿',
      raw_content: 'fork draft content',
      domain: 'database',
      tags: ['fork'],
      status: 'draft',
      user_id: 'demo-user',
      created_at: new Date().toISOString(),
      ai_structured_content: {
        architecture_diagram: '',
        key_evidence: [],
        reference_links: [],
        reveal_strategy: { surface_clues: [], deep_clues: [], distractors: [] },
        root_cause: 'fork root cause',
        standard_procedure: [],
      },
      review_history: [],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await page.getByLabel('难度').selectOption('L4')
  await expect.poll(() => queries.at(-1)).toContain('difficulty=L4')

  await page.getByPlaceholder('输入或点选标签').fill('MySQL')
  await expect.poll(() => queries.at(-1)).toContain('tag=MySQL')

  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => queries.at(-1)).toContain('page=2')

  await page.getByRole('button', { name: '派生题目' }).click()
  await expect(page.locator('.inline-success')).toContainText('已创建“Fork 后草稿”草稿')
  await expect(page.getByRole('button', { name: '去编辑草稿' })).toBeVisible()
})

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '通过已有限制生成出来的训练题目。',
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
      architecture_diagram: '',
      reference_links: [],
    },
    status: 'active',
    source: 'llm_generated',
    created_by: 'demo-user',
    version: 1,
    is_sanitized: true,
  }
}

function aiJob(
  id: string,
  status: 'queued' | 'running' | 'completed' | 'failed',
  progress: number,
  overrides: Record<string, unknown> = {},
) {
  const now = new Date().toISOString()
  return {
    id,
    user_id: 'demo-user',
    kind: 'scenario_generation',
    status,
    stage: status,
    progress,
    provider: 'mock',
    validated: status === 'completed',
    fallback_used: false,
    created_at: now,
    started_at: now,
    completed_at: status === 'completed' || status === 'failed' ? now : undefined,
    updated_at: now,
    ...overrides,
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}
