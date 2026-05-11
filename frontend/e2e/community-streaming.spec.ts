import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('community preview shows streaming progress while post is structured and checked', async ({ page }) => {
  const post = communityPost('e2e-community-stream-post', `E2E streaming community post ${Date.now()}`)
  let releasePost: (() => void) | undefined

  await page.route('**/api/v1/community/posts**', async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname

    if (request.method() === 'GET') {
      await fulfillJSON(route, { list: [] })
      return
    }

    if (request.method() === 'POST' && pathname.endsWith('/community/posts')) {
      await new Promise<void>((resolve) => {
        releasePost = resolve
      })
      await fulfillSSE(route, [
        ['stage', { message: 'case received', step: 'received' }],
        ['stage', { message: 'streaming structure', step: 'llm' }],
        ['delta', { chunk: 'cache key changed', displayable: false }],
        ['stage', { message: 'schema ok', step: 'schema_validated' }],
        ['stage', { message: 'rules', step: 'rule_sensitive_check' }],
        ['stage', { message: 'model', step: 'model_sensitive_check' }],
        ['stage', { message: 'sanitized', step: 'sanitized' }],
        ['stage', { message: 'saving preview', step: 'saving' }],
        ['stage', { message: 'done', step: 'completed' }],
        ['finish', post],
      ])
      return
    }

    await route.fallback()
  })

  await loginAs(page, 'student')
  await page.goto('/community')
  await page.locator('section.form-panel input').first().fill(post.title)
  await page.locator('section.form-panel textarea').first().fill(post.raw_content)
  const publishButton = page.locator('section.form-panel button.primary-button')
  await publishButton.click()

  const status = page.getByTestId('community-preview-status')
  await expect(status).toBeVisible()
  await expect(publishButton).toBeDisabled()
  releasePost?.()
  await expect(status).toContainText('结构化预览已生成')
  await expect(status).toContainText('规则检测正在检查敏感信息')
  await expect(status).toContainText('模型检测正在识别敏感信息')
  await expect(status).toContainText('敏感字段已脱敏并生成处理建议')
  await expect(status).not.toContainText('cache key changed')
  await expect(status).not.toContainText('{re')
  await expect(page.locator('.scenario-card').filter({ hasText: post.title })).toBeVisible()
})

async function fulfillJSON(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

async function fulfillSSE(route: Route, events: Array<[string, unknown]>) {
  await route.fulfill({
    contentType: 'text/event-stream',
    body: events.map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join(''),
  })
}

function communityPost(id: string, title: string) {
  return {
    id,
    user_id: 'demo-user',
    title,
    raw_content: 'Cache key rules changed after release and database reads increased.',
    domain: 'database',
    tags: ['cache', 'change'],
    ai_structured_content: {
      root_cause: 'Cache key rules changed after release.',
      root_cause_keywords: ['cache', 'key', 'release'],
      key_evidence: ['cache hit ratio dropped', 'database reads increased'],
      standard_procedure: ['check release diff', 'compare key rules', 'rollback or dual-read old keys'],
      reveal_strategy: {
        surface_clues: [],
        deep_clues: [],
        distractors: [],
      },
      architecture_diagram: '',
      reference_links: [],
    },
    sensitive_check: { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
    review_history: [],
    status: 'pending_review',
    created_at: new Date().toISOString(),
  }
}
