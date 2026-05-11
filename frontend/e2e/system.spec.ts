import { expect, type Page, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs } from './helpers/auth'

test('admin can inspect system status', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/system')

  await expectNoWhiteScreen(page)
  await expect(page.getByRole('heading', { name: '系统状态' })).toBeVisible()
  await expect(page.locator('body')).not.toContainText('比赛现场确认 API、数据、Redis、AI Router、敏感检测和演示脚本入口。')
  const overviewMetrics = page.locator('.metric-row').first()
  await expect(overviewMetrics.getByText('用户数')).toBeVisible()
  await expect(overviewMetrics.getByText('题目数')).toBeVisible()
  await expect(overviewMetrics.getByText('AI生成题')).toBeVisible()
  await expect(overviewMetrics.getByText('AI任务')).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: '存储运行模式' })).toBeVisible()
  await expect(page.locator('.system-service').filter({ hasText: 'AI Provider' })).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: '演示账号' })).toBeVisible()
  await expect(page.locator('.system-service').filter({ hasText: 'Seed Data' })).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: '模型参数' })).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: 'Schema 校验' })).toBeVisible()
  const scenarioSchema = page.locator('.schema-validator-row').filter({ hasText: 'scenario_question' })
  await expect(scenarioSchema).toBeVisible()
  await expect(scenarioSchema).toContainText('SC-03')
  await expect(scenarioSchema).toContainText('v1.0.0')
  await expect(scenarioSchema.locator('.schema-status-ok')).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: 'Prompt 模板' })).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: '审计与限流' })).toBeVisible()
})

test('admin sees prompt save failures without losing edits', async ({ page }) => {
  await mockSystemStatus(page)
  let payload: Record<string, unknown> | undefined
  await page.route('**/api/v1/admin/prompts/scenario_generate', async (route) => {
    payload = route.request().postDataJSON() as Record<string, unknown>
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ code: 400, message: 'prompt content is too short' }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/system')
  await expect(page.getByRole('heading', { name: '系统状态' })).toBeVisible()

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await prompt.getByRole('button', { name: '加载编辑原文' }).click()
  await prompt.locator('textarea').fill('bad')
  await prompt.getByRole('button', { name: /保存 Prompt/ }).click()

  await expect(page.locator('.inline-error')).toContainText('prompt content is too short')
  await expect(page.locator('.success-line')).toHaveCount(0)
  await expect(prompt.locator('textarea')).toHaveValue('bad')
  expect(payload?.render_engine).toBe('go_template')
  await expectNoWhiteScreen(page)
})

test('system status prompt summary does not render raw prompt content', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  await expect(page.locator('.panel-title').filter({ hasText: 'Prompt 模板' })).toBeVisible()
  await expect(page.locator('body')).toContainText('原文已从系统状态脱敏')
  await expect(page.locator('body')).toContainText('go_template')
  await expect(page.locator('body')).not.toContainText('按专业域、难度和类型生成结构化情景题 JSON。')
  await expectNoWhiteScreen(page)
})

test('prompt template secondary meta stays on its own line', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' }).first()
  const title = prompt.locator('strong').first()
  const meta = prompt.locator('span').first()

  await expect(title).toBeVisible()
  await expect(meta).toBeVisible()

  const titleBox = await title.boundingBox()
  const metaBox = await meta.boundingBox()
  expect(titleBox).not.toBeNull()
  expect(metaBox).not.toBeNull()
  if (titleBox && metaBox) {
    expect(metaBox.y).toBeGreaterThan(titleBox.y + 4)
  }

  await expectNoWhiteScreen(page)
})

test('router tags panel should not stretch to match decision list height', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/system')

  const grid = page.locator('.system-signal-grid')
  const tagsPanel = grid.locator('.system-signal-column .panel').filter({ hasText: 'Router 运行标签' }).first()
  const decisionsPanel = grid.locator('> .panel').filter({ hasText: '最近决策' }).first()

  await expect(tagsPanel).toBeVisible()
  await expect(decisionsPanel).toBeVisible()

  const tagsBox = await tagsPanel.boundingBox()
  const decisionsBox = await decisionsPanel.boundingBox()
  expect(tagsBox).not.toBeNull()
  expect(decisionsBox).not.toBeNull()
  if (tagsBox && decisionsBox) {
    expect(tagsBox.height + 120).toBeLessThan(decisionsBox.height)
  }

  await expectNoWhiteScreen(page)
})

test('router tags and error trace should stack in the same left column', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/system')

  const grid = page.locator('.system-signal-grid')
  const leftColumn = grid.locator('.system-signal-column')

  await expect(grid).toBeVisible()
  await expect(leftColumn).toBeVisible()
  await expect(leftColumn).toContainText('Router 运行标签')
  await expect(leftColumn).toContainText('最近错误与追踪')
  await expect(grid.locator('> .panel').filter({ hasText: '最近决策' })).toBeVisible()

  await expectNoWhiteScreen(page)
})

test('admin prompt engine selection survives status refresh before save', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await prompt.getByLabel('Render Engine').selectOption('jinja2')
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  await expect(prompt.getByLabel('Render Engine')).toHaveValue('jinja2')
  await expectNoWhiteScreen(page)
})

test('admin prompt content draft survives status refresh before save', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await prompt.getByRole('button', { name: '加载编辑原文' }).click()
  await prompt.locator('textarea').fill('保留中的 Prompt 草稿')
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  await expect(prompt.locator('textarea')).toHaveValue('保留中的 Prompt 草稿')
  await expectNoWhiteScreen(page)
})

test('admin prompt engine draft saves after status refresh without loading prompt source', async ({ page }) => {
  await mockSystemStatus(page)
  let payload: Record<string, unknown> | undefined
  await page.route('**/api/v1/admin/prompts/scenario_generate', async (route) => {
    payload = route.request().postDataJSON() as Record<string, unknown>
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          name: 'scenario_generate',
          task: '情景题生成',
          default: '按专业域、难度和类型生成结构化情景题 JSON。',
          content: '按专业域、难度和类型生成结构化情景题 JSON。',
          render_engine: 'jinja2',
          updated_at: new Date().toISOString(),
          is_modified: true,
          validator: 'scenario_question',
        },
      }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await prompt.getByLabel('Render Engine').selectOption('jinja2')
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })
  await prompt.getByRole('button', { name: /保存 Prompt/ }).click()

  expect(payload?.content).toBeUndefined()
  expect(payload?.render_engine).toBe('jinja2')
  await expect(page.locator('.success-line')).toContainText('已保存 Prompt：scenario_generate')
  await expect(page.locator('.inline-error')).toHaveCount(0)
  await expectNoWhiteScreen(page)
})

test('admin can bulk save all prompt engines as jinja2 and go_template and preserve after refresh', async ({ page }) => {
  const promptTemplateState = [
    {
      name: 'scenario_generate',
      task: '情景题生成',
      validator: 'scenario_question',
      render_engine: 'go_template',
      content: '模板当前内容',
      default: '模板默认内容',
    },
    {
      name: 'community_structure',
      task: 'UGC 结构化',
      validator: 'scenario_content_preview',
      render_engine: 'go_template',
      content: '模板当前内容',
      default: '模板默认内容',
    },
  ]

  await mockSystemStatus(page, () => ({
    prompt_templates: promptTemplateState.map((item) => ({
      name: item.name,
      task: item.task,
      updated_at: new Date().toISOString(),
      is_modified: item.render_engine === 'jinja2',
      validator: item.validator,
      render_engine: item.render_engine,
      summary: 'default prompt template',
      content_length: item.content.length,
      default_length: item.default.length,
    })),
  }))

  const payloads: Array<{ name: string; render_engine?: unknown; content?: unknown }> = []
  await page.route('**/api/v1/admin/prompts/*', async (route) => {
    const url = new URL(route.request().url())
    const name = url.pathname.split('/').pop() ?? 'unknown'
    const payload = route.request().postDataJSON() as Record<string, unknown>
    payloads.push({ name, render_engine: payload.render_engine, content: payload.content })
    const template = promptTemplateState.find((item) => item.name === name)
    if (template && typeof payload.render_engine === 'string') {
      template.render_engine = payload.render_engine
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          name,
          task: name === 'scenario_generate' ? '情景题生成' : name,
          default: template?.default ?? '模板默认内容',
          content: template?.content ?? '模板当前内容',
          render_engine: template?.render_engine ?? 'jinja2',
          updated_at: new Date().toISOString(),
          is_modified: true,
          validator: name === 'scenario_generate' ? 'scenario_question' : 'scenario_content_preview',
        },
      }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/system')

  await page.getByRole('button', { name: '全部保存为 jinja2' }).click()
  await expect(page.locator('.success-line')).toContainText('已批量保存')

  expect(payloads.length).toBeGreaterThan(0)
  const firstBatchCount = payloads.length
  for (const payload of payloads.slice(0, firstBatchCount)) {
    expect(payload.render_engine).toBe('jinja2')
    expect(typeof payload.content).toBe('string')
    expect(String(payload.content).length).toBeGreaterThan(0)
  }

  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await expect(prompt.getByLabel('Render Engine')).toHaveValue('jinja2')
  await expect(page.locator('.success-line')).toContainText('已批量保存')
  const firstMessage = await page.locator('.success-line').textContent()
  const firstSavedCount = Number(firstMessage?.match(/已批量保存\s+(\d+)\s+个 Prompt/)?.[1] ?? '0')
  expect(firstSavedCount).toBeGreaterThan(0)
  expect(firstSavedCount).toBe(payloads.length)

  await page.getByRole('button', { name: '全部保存为 go_template' }).click()
  await expect(page.locator('.success-line')).toContainText('go_template')

  expect(payloads.length).toBeGreaterThan(firstBatchCount)
  const secondBatch = payloads.slice(firstBatchCount)
  for (const payload of secondBatch) {
    expect(payload.render_engine).toBe('go_template')
    expect(typeof payload.content).toBe('string')
    expect(String(payload.content).length).toBeGreaterThan(0)
  }

  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  await expect(prompt.getByLabel('Render Engine')).toHaveValue('go_template')
  await expect(page.locator('.success-line')).toContainText('已批量保存')
  const secondMessage = await page.locator('.success-line').textContent()
  const secondSavedCount = Number(secondMessage?.match(/已批量保存\s+(\d+)\s+个 Prompt/)?.[1] ?? '0')
  expect(secondSavedCount).toBeGreaterThan(0)
  expect(secondSavedCount).toBe(secondBatch.length)
  await expect(page.locator('.inline-error')).toHaveCount(0)
  await expectNoWhiteScreen(page)
})

test('admin prompt save requires loaded source before editing', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await expect(prompt.locator('textarea')).toHaveCount(0)
  await prompt.getByRole('button', { name: /保存 Prompt/ }).click()

  await expect(page.locator('.inline-error')).toContainText('请先加载编辑原文')
  await expect(prompt.locator('textarea')).toHaveCount(0)
  await expect(prompt.locator('.review-turn')).toContainText('原文已从系统状态脱敏')
  await expectNoWhiteScreen(page)
})

test('admin sees jinja2 backend dependency error when prompt save fails', async ({ page }) => {
  await mockSystemStatus(page)
  await page.route('**/api/v1/admin/prompts/scenario_generate', async (route) => {
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ code: 400, message: 'render prompt scenario_generate: exec: "python": executable file not found in $PATH' }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/system')

  const prompt = page.locator('.prompt-admin-item').filter({ hasText: 'scenario_generate' })
  await prompt.getByRole('button', { name: '加载编辑原文' }).click()
  await prompt.getByLabel('Render Engine').selectOption('jinja2')
  await prompt.locator('textarea').fill('你是 {{ Domain }} 助手。')
  await prompt.getByRole('button', { name: /保存 Prompt/ }).click()

  await expect(page.locator('.inline-error')).toContainText('python')
  await expect(prompt.locator('textarea')).toHaveValue('你是 {{ Domain }} 助手。')
  await expectNoWhiteScreen(page)
})

test('admin sees sensitive detection schema status', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const sensitiveSchema = page.locator('.schema-validator-row').filter({ hasText: 'sensitive_check' })
  await expect(sensitiveSchema).toBeVisible()
  await expect(sensitiveSchema).toContainText('sensitive_detection')
  await expect(sensitiveSchema).toContainText('v1.0.0')
  await expect(sensitiveSchema.locator('.schema-status-ok')).toBeVisible()
  await expectNoWhiteScreen(page)
})

test('admin sees sensitive detection service summary without card overflow', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const service = page.locator('.system-service').filter({ hasText: 'Sensitive Detection' })
  await expect(service).toBeVisible()
  await expect(service.locator('.system-service-heading')).toBeVisible()
  await expect(service).toContainText('正常')
  await expect(service).toContainText('sensitive_check')

  const box = await service.boundingBox()
  const detailBox = await service.locator('p').boundingBox()
  expect(box).not.toBeNull()
  expect(detailBox).not.toBeNull()
  if (box && detailBox) {
    expect(detailBox.x + detailBox.width).toBeLessThanOrEqual(box.x + box.width + 1)
  }
  await expectNoWhiteScreen(page)
})

test('admin sees diagnostic agent summary', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const panel = page.locator('.panel').filter({ hasText: 'Agent 运行摘要' })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('最近运行')
  await expect(panel).toContainText('安全重写')
  await expect(panel).toContainText('diagnostic_agent')
  await expect(panel).toContainText('失败次数')
  await expect(page.getByTestId('agent-summary-total')).toHaveText('3')
  await expect(page.getByTestId('agent-summary-failed')).toHaveText('2')
  await expect(page.getByTestId('agent-summary-safety-rewritten')).toHaveText('1')
  await expect(page.locator('body')).not.toContainText('agent_trace')
  await expect(page.locator('body')).not.toContainText('root_cause')
  await expectNoWhiteScreen(page)
})

test('agent breakdown placeholder aligns with its section content', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  const container = page.locator('.system-agent-breakdown')
  const card = page.locator('.system-agent-breakdown-empty')

  await expect(container).toBeVisible()
  await expect(card).toBeVisible()

  const containerBox = await container.boundingBox()
  const cardBox = await card.boundingBox()
  expect(containerBox).not.toBeNull()
  expect(cardBox).not.toBeNull()
  if (containerBox && cardBox) {
    expect(cardBox.x - containerBox.x).toBeLessThanOrEqual(4)
  }

  await expectNoWhiteScreen(page)
})

test('admin sees ai config save failures without losing edits', async ({ page }) => {
  await mockSystemStatus(page)
  await page.route('**/api/v1/admin/ai-config', async (route) => {
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ code: 400, message: 'model is required' }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/system')
  await expect(page.getByRole('heading', { name: '系统状态' })).toBeVisible()

  await page.getByLabel('Model').fill('')
  await page.getByRole('button', { name: /保存配置/ }).click()

  await expect(page.locator('.inline-error')).toContainText('model is required')
  await expect(page.locator('.success-line')).toHaveCount(0)
  await expect(page.getByLabel('Model')).toHaveValue('')
  await expect(page.getByLabel('Top P')).toHaveValue('0.8')
  await expect(page.getByLabel('Top K')).toHaveValue('24')
  await expectNoWhiteScreen(page)
})

test('admin ai config draft survives status refresh before save', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  await page.getByLabel('Model').fill('draft-model')
  await page.getByLabel('Top P').fill('0.3')
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  await expect(page.getByLabel('Model')).toHaveValue('draft-model')
  await expect(page.getByLabel('Top P')).toHaveValue('0.3')
  await expectNoWhiteScreen(page)
})

test('admin sees router status snapshot when telemetry is empty', async ({ page }) => {
  await mockSystemStatus(page)

  await loginAs(page, 'admin')
  await page.goto('/system')

  await expect(page.locator('main')).toContainText('llm-router-status')
  await expect(page.locator('main')).toContainText('状态检查')
  await expect(page.locator('main')).not.toContainText('未知任务')
  await expectNoWhiteScreen(page)
})

test('admin router statistics refresh after telemetry changes', async ({ page }) => {
  let totalCalls = 0
  await mockSystemStatus(page, () => ({
    ai: {
      provider: 'mock',
      model: 'mock',
      fallback: true,
      telemetry: totalCalls > 0 ? {
        total_calls: totalCalls,
        successful_calls: totalCalls,
        failed_calls: 0,
        fallback_calls: 0,
        stream_calls: 0,
        json_calls: totalCalls,
        safety_rewrites: 0,
        validation_errors: 0,
        provider_calls: { mock: totalCalls },
        task_calls: { scenario_generate: totalCalls },
        recent_decisions: [{
          trace_id: `llm-router-scenario-generate-${totalCalls}`,
          task: 'scenario_generate',
          provider: 'mock',
          model: 'mock',
          output_mode: 'json',
          stream: false,
          safety_policy: 'default',
          fallback_chain: ['mock'],
          validation: { required: true, schema: 'scenario_question', status: 'passed' },
          safety: { policy: 'default', status: 'passed', blocked: false },
          started_at: new Date().toISOString(),
          completed_at: new Date().toISOString(),
          latency_ms: 12,
          status: 'ok',
        }],
        updated_at: new Date().toISOString(),
      } : undefined,
    },
  }))

  await loginAs(page, 'admin')
  await page.goto('/system')
  const routerStats = page.locator('.panel').filter({ hasText: 'Router 统计' })
  await expect(routerStats).toContainText('总调用')
  await expect(routerStats).toContainText('0')

  totalCalls = 2
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('ai-router:refresh'))
  })

  await expect(routerStats).toContainText('2')
  await expect(page.locator('main')).toContainText('场景生成')
  await expect(page.locator('main')).toContainText('llm-router-scenario-generate-2')
  await expectNoWhiteScreen(page)
})

test('admin sees provider pool capability matrix and fallback order', async ({ page }) => {
  await mockAdminLogin(page)
  await mockSystemStatus(page, () => ({
    ai: {
      provider: 'deepseek',
      model: 'deepseek-chat',
      fallback: true,
      provider_pool: {
        active_provider: 'deepseek',
        fallback_order: ['deepseek', 'qwen', 'ernie', 'openai_compatible', 'mock'],
        degraded_count: 3,
        updated_at: '2026-05-06T10:00:00Z',
        providers: [
          {
            provider: 'deepseek',
            model: 'deepseek-chat',
            transport: 'openai-compatible',
            supports_streaming: true,
            supports_json: true,
            supports_tools: false,
            temperature: true,
            top_p: true,
            top_k: true,
            max_tokens: 8192,
            cost_tier: 'standard',
            health: 'ok',
            status: 'ok',
            supported_tasks: ['scenario_generate'],
            priority: 10,
            enabled: true,
            call_count: 7,
            last_checked_at: '2026-05-06T09:59:00Z',
          },
          {
            provider: 'qwen',
            model: 'qwen-max',
            transport: 'openai-compatible',
            supports_streaming: true,
            supports_json: true,
            supports_tools: false,
            temperature: true,
            top_p: true,
            top_k: true,
            max_tokens: 8192,
            cost_tier: 'standard',
            health: 'missing',
            status: 'missing',
            supported_tasks: ['scenario_generate'],
            priority: 20,
            enabled: false,
            call_count: 0,
            fallback_reason: 'missing api key',
          },
          {
            provider: 'ernie',
            model: 'ernie-4.0-turbo-8k',
            transport: 'openai-compatible',
            supports_streaming: true,
            supports_json: true,
            supports_tools: false,
            temperature: true,
            top_p: true,
            top_k: true,
            max_tokens: 8192,
            cost_tier: 'standard',
            health: 'missing',
            status: 'missing',
            supported_tasks: ['scenario_generate'],
            priority: 30,
            enabled: false,
            call_count: 0,
            fallback_reason: 'missing api key',
          },
          {
            provider: 'openai_compatible',
            model: 'glm-4.5',
            transport: 'openai-compatible',
            supports_streaming: false,
            supports_json: true,
            supports_tools: false,
            temperature: true,
            top_p: false,
            top_k: true,
            max_tokens: 4096,
            cost_tier: 'premium',
            health: 'degraded',
            status: 'degraded',
            supported_tasks: ['scenario_reply'],
            priority: 20,
            enabled: true,
            call_count: 3,
            last_error_type: 'rate_limit',
            last_error: 'Provider returned a long rate limit message that should wrap instead of overflowing the matrix cell.',
            fallback_reason: 'rate limit fallback',
            last_checked_at: '2026-05-06T09:58:00Z',
            rate_limit: { enabled: true, remaining: 0, reset_at: '2026-05-06T10:15:00Z' },
          },
          {
            provider: 'mock',
            model: 'mock',
            transport: 'mock',
            supports_streaming: false,
            supports_json: true,
            supports_tools: false,
            temperature: false,
            top_p: false,
            top_k: false,
            max_tokens: 2048,
            cost_tier: 'free',
            health: 'ok',
            status: 'standby',
            supported_tasks: [],
            priority: 99,
            enabled: true,
            call_count: 0,
            fallback_reason: 'final safety fallback',
          },
        ],
        recent_attempts: [
          {
            provider: 'openai_compatible',
            model: 'glm-4.5',
            success: false,
            error_type: 'rate_limit',
            fallback_reason: 'rate limit fallback',
            latency_ms: 1200,
            started_at: '2026-05-06T09:57:00Z',
            completed_at: '2026-05-06T09:57:01Z',
          },
        ],
      },
    },
  }))

  await loginAs(page, 'admin')
  await page.goto('/system')

  const panel = page.getByTestId('provider-pool-panel')
  await expect(panel).toBeVisible()
  await expect(page.getByTestId('fallback-order')).toContainText('deepseek')
  await expect(page.getByTestId('fallback-order')).toContainText('qwen')
  await expect(page.getByTestId('fallback-order')).toContainText('ernie')
  await expect(page.getByTestId('fallback-order')).toContainText('openai_compatible')
  await expect(page.getByTestId('fallback-order')).toContainText('mock')

  await expect(page.getByTestId('provider-row-deepseek')).toContainText('deepseek-chat')
  await expect(page.getByTestId('provider-row-deepseek')).toContainText('7')
  await expect(page.getByTestId('provider-row-deepseek')).toContainText('stream')
  await expect(page.getByTestId('provider-row-deepseek')).toContainText('json')
  await expect(page.getByTestId('provider-row-deepseek')).toContainText('top-k')

  await expect(page.getByTestId('provider-row-qwen')).toContainText('disabled')
  await expect(page.getByTestId('provider-row-qwen')).toContainText('missing')
  await expect(page.getByTestId('provider-row-ernie')).toContainText('disabled')
  await expect(page.getByTestId('provider-row-ernie')).toContainText('missing')

  const degraded = page.getByTestId('provider-row-openai_compatible')
  await expect(degraded).toContainText('degraded')
  await expect(degraded).toContainText('rate_limit')
  await expect(degraded).toContainText('rate limit fallback')
  await expect(page.locator('body')).not.toContainText('Provider returned a long rate limit message')
  await expect(panel).toContainText('1')
  await expectNoWhiteScreen(page)
})

async function mockAdminLogin(page: Page) {
  const adminUser = {
    id: 'user-admin',
    username: 'admin',
    email: 'admin@example.com',
    role: 'admin',
    profile: { preferred_domains: [], target_level: 'advanced', points: 0, badges: [] },
  }
  await page.route('**/api/v1/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'ok',
        data: {
          user: adminUser,
          access_token: 'e2e-admin-token',
          refresh_token: 'e2e-admin-refresh',
        },
      }),
    })
  })
  await page.route('**/api/v1/users/me', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, message: 'ok', data: adminUser }),
    })
  })
}

async function mockSystemStatus(page: Page, overrides?: () => Record<string, unknown>) {
  const editablePrompts = [
    {
      name: 'scenario_generate',
      task: '情景题生成',
      default: '按专业域、难度和类型生成结构化情景题 JSON。',
      content: '按专业域、难度和类型生成结构化情景题 JSON。',
      render_engine: 'go_template',
      updated_at: new Date().toISOString(),
      is_modified: false,
      validator: 'scenario_question',
    },
    {
      name: 'community_structure',
      task: 'UGC 结构化',
      default: '模板默认内容',
      content: '模板当前内容',
      render_engine: 'go_template',
      updated_at: new Date().toISOString(),
      is_modified: false,
      validator: 'scenario_content_preview',
    },
  ]
  await page.route('**/api/v1/admin/prompts', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: { list: editablePrompts },
      }),
    })
  })
  await page.route('**/api/v1/system/status', async (route) => {
    const data = {
      generated_at: new Date().toISOString(),
      services: [
        { name: 'API', status: 'ok', detail: 'ok' },
        { name: 'AI Provider', status: 'fallback', detail: 'mock fallback active' },
        { name: 'Sensitive Detection', status: 'ok', detail: 'rule and model safety checks use sensitive_check via https://api.deepseek.com/v1/chat/completions' },
        { name: 'Seed Data', status: 'ok', detail: 'seed ready' },
      ],
      ai: { provider: 'mock', model: 'mock', fallback: true },
      ai_config: {
        provider: 'mock',
        model: 'mock',
        temperature: 0.2,
        top_p: 0.8,
        top_k: 24,
        max_tokens: 3072,
        stream_enabled: true,
        fallback_model: 'mock',
        updated_at: new Date().toISOString(),
      },
      prompt_templates: [
        {
          name: 'scenario_generate',
          task: '情景题生成',
          updated_at: new Date().toISOString(),
          is_modified: false,
          validator: 'scenario_question',
          summary: 'default prompt template, 23 characters',
          content_length: 23,
          default_length: 23,
        },
        {
          name: 'community_structure',
          task: 'UGC 结构化',
          updated_at: new Date().toISOString(),
          is_modified: false,
          validator: 'scenario_content_preview',
          summary: 'default prompt template, 24 characters',
          content_length: 24,
          default_length: 24,
        },
      ],
      schema_validators: [
        {
          name: 'scenario_question',
          schema_name: 'scenario_question',
          target: 'SC-03 情景题完整内容',
          task: 'SC-03',
          version: '1.0.0',
          description: '情景题生成 JSON Schema',
          status: 'ok',
        },
        {
          name: 'sensitive_detection',
          schema_name: 'sensitive_check',
          target: 'UGC 敏感信息检测结果',
          task: 'sensitive_detection',
          version: '1.0.0',
          description: '敏感信息风险、来源与脱敏建议 Schema',
          status: 'ok',
        },
      ],
      rate_limit: { enabled: false, detail: 'noop' },
      sensitive_detection: {
        status: 'ok',
        provider: 'mock',
        model: 'mock',
        fallback_count: 0,
        fallback_used: false,
        rule_enabled: true,
        model_enabled: true,
        schema: 'sensitive_check',
        detail: 'rule and model safety checks ready',
        checked_actions: ['community.create', 'community.draft_update', 'community.submit'],
      },
      audit_summary: {
        total_recent: 1,
        by_action: { 'agent.diagnostic_run': 1 },
        latest: [{
          id: 'audit-agent-1',
          actor_id: 'user-demo',
          action: 'agent.diagnostic_run',
          resource_type: 'scenario_session',
          resource_id: 'session-1',
          metadata: {
            agent: 'diagnostic_agent',
            agent_trace: '{"steps":[{"metadata":{"root_cause":"hidden"}}]}',
          },
          created_at: '2026-05-04T10:30:00Z',
        }],
      },
      agent_summary: {
        total_recent: 3,
        latest_agent: 'diagnostic_agent',
        latest_run_at: '2026-05-04T10:30:00Z',
        failed_recent: 2,
        safety_rewritten_recent: 1,
      },
      recent_ai_errors: [],
      store: { mode: 'postgres', persistent: true },
      counts: { users: 3, scenarios: 2, active_scenarios: 2, community_posts: 0, pending_ugc: 0, generated_scenarios: 1, ai_jobs: 2 },
      demo_accounts: [{ role: 'admin', username: 'admin', purpose: '系统管理' }],
      runbook: [{ title: '演示验收', command: '.\\scripts\\demo-acceptance.ps1' }],
    }
    const overrideData = overrides?.() ?? {}
    Object.assign(data, overrideData)
    if (overrideData.ai && typeof overrideData.ai === 'object') {
      data.ai = { ...data.ai, ...(overrideData.ai as Record<string, unknown>) }
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data,
      }),
    })
  })
}
