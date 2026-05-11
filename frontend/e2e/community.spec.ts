import { expect, type Page, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs, resetSession } from './helpers/auth'

const mockInstructorUserId = 'e2e-instructor-user'

test('community publish preview and dual review stay on-screen', async ({ page }) => {
  const state = await mockCommunityApi(page)
  const title = `E2E 缓存命中率下降案例 ${Date.now()}`

  await loginAs(page, 'student')
  await page.goto('/community')
  await expect(page.getByRole('heading', { name: '案例工坊' })).toBeVisible()
  await page.getByLabel('案例标题').fill(title)
  await page.getByLabel('原始描述').fill('发布后缓存 key 规则变化，命中率下降，数据库读请求升高。')
  await page.getByRole('button', { name: /发布预览|生成中/ }).click()

  await expectNoWhiteScreen(page)
  await expect(page.getByRole('heading', { name: '案例工坊' })).toBeVisible()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
  await expect(page.getByText('待讲师初审')).toBeVisible()
  await expect(page.getByText('demo-user')).toBeVisible()
  const createdCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(createdCard.getByText('CM 审核摘要')).toHaveCount(0)
  await createdCard.getByRole('button', { name: /展开详情/ }).click()
  await expect(createdCard.getByText('CM 审核摘要')).toBeVisible()

  await resetSession(page)
  await loginAs(page, 'instructor')
  await page.goto('/community')
  await expect(page.getByRole('button', { name: '初审队列' })).toBeVisible()
  const reviewCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(reviewCard).toBeVisible()
  await reviewCard.getByRole('button', { name: /展开详情/ }).click()
  const moderationBlock = reviewCard.locator('.structured-preview').filter({ hasText: 'CM 审核摘要' })
  await expect(moderationBlock).toContainText('建议人工重点复核')
  await expect(moderationBlock).toContainText('风险：中')
  await expect(moderationBlock).toContainText('需重点复核')
  await page.getByPlaceholder(/填写初审意见/).fill('结构清晰，可以提交终审。')
  await page.getByRole('button', { name: /初审通过/ }).click()
  await expectNoWhiteScreen(page)
  await expect.poll(() => state.post?.status).toBe('instructor_approved')

  await resetSession(page)
  await loginAs(page, 'admin')
  await page.goto('/community')
  const finalCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(page.getByRole('button', { name: '终审队列' })).toBeVisible()
  await expect(finalCard).toBeVisible()
  await finalCard.getByRole('button', { name: /展开详情/ }).click()
  await page.getByPlaceholder(/填写终审意见/).fill('终审通过，发布为正式题。')
  await page.getByRole('button', { name: /发布为题/ }).click()
  await expectNoWhiteScreen(page)
  await expect.poll(() => state.post?.status).toBe('published')

  await resetSession(page)
  await loginAs(page, 'instructor')
  await page.goto('/community?view=instructor_reviewed')
  await expect(page.getByRole('button', { name: '我已初审' })).toBeVisible()
  const reviewedCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(reviewedCard).toBeVisible()
  await expect(reviewedCard.getByText('已发布题库', { exact: true })).toBeVisible()
  await expect.poll(() => state.getQueries.includes('?view=instructor_reviewed')).toBe(true)
  await expect(page.getByRole('button', { name: '我已驳回' })).toBeVisible()
  await page.getByRole('button', { name: '初审队列' }).click()
  await expect(page).toHaveURL(/\/community\?status=pending_review/)
  await expect.poll(() => state.getQueries.includes('?status=pending_review')).toBe(true)
  await expect(page.getByRole('heading', { name: title })).toHaveCount(0)
  await page.getByRole('button', { name: '全部' }).click()
  await expect(page).toHaveURL(/\/community\?scope=all/)
  await expect(page.getByRole('heading', { name: title })).toBeVisible()

  await mockConvertedScenario(page, title)
  await resetSession(page)
  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await expect(page.getByRole('heading', { name: '排查工坊' })).toBeVisible()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
  await expect(page.getByText('脱敏选题卡片')).toBeVisible()
})

test('admin can delete published community post from published and all views', async ({ page }) => {
  const state = await mockCommunityApi(page)
  const title = `E2E 已发布删除案例 ${Date.now()}`

  await loginAs(page, 'student')
  await page.goto('/community')
  await page.getByLabel('案例标题').fill(title)
  await page.getByLabel('原始描述').fill('发布后缓存 key 规则变化，命中率下降，数据库读请求升高。')
  await page.getByRole('button', { name: /发布预览|生成中/ }).click()

  await resetSession(page)
  await loginAs(page, 'instructor')
  await page.goto('/community')
  const reviewCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await reviewCard.getByRole('button', { name: /展开详情/ }).click()
  await page.getByPlaceholder(/填写初审意见/).fill('结构清晰，可以提交终审。')
  await page.getByRole('button', { name: /初审通过/ }).click()

  await resetSession(page)
  await loginAs(page, 'admin')
  await page.goto('/community')
  const finalCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await finalCard.getByRole('button', { name: /展开详情/ }).click()
  await page.getByPlaceholder(/填写终审意见/).fill('终审通过，发布为正式题。')
  await page.getByRole('button', { name: /发布为题/ }).click()
  await expect.poll(() => state.post?.status).toBe('published')

  await page.goto('/community?status=published')
  const publishedCard = page.locator('article').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(publishedCard).toBeVisible()
  await publishedCard.getByRole('button', { name: /删除案例|删除中/ }).click()
  await expect.poll(() => state.post).toBeUndefined()
  await expect(page.getByRole('heading', { name: title })).toHaveCount(0)

  await page.goto('/community?scope=all')
  await expect(page.getByRole('heading', { name: title })).toHaveCount(0)
})

test('community all view renders posts as wide expandable feed cards', async ({ page }) => {
  await page.setViewportSize({ width: 1202, height: 955 })
  await mockCommunityApi(page)
  const title = `E2E 横排信息流案例 ${Date.now()}`
  const description = [
    '发布后缓存 key 规则发生变化，命中率异常下降，数据库读请求明显升高。',
    '排查时需要先确认变更窗口，再对比新旧 key 生成规则，并观察缓存集群的热点分布。',
    '如果只看数据库慢查询会误判为索引问题，实际根因在应用发布后的缓存兼容策略缺失。',
    '讲师审核时需要能横向扫读作者、标题和完整描述，并在展开后继续查看结构化审核信息。',
    '复盘记录还需要说明灰度批次、缓存预热是否完成、回滚窗口内的命中率曲线，以及业务高峰期是否触发降级保护。',
    '这些信息如果被压在窄卡片里会互相覆盖，审核者无法快速判断案例是否具备进入下一阶段的证据质量。',
  ].join('')

  await loginAs(page, 'student')
  await page.goto('/community')
  await page.getByLabel('案例标题').fill(title)
  await page.getByLabel('原始描述').fill(description)
  await page.getByRole('button', { name: /发布预览|生成中/ }).click()

  await resetSession(page)
  await loginAs(page, 'admin')
  await page.goto('/community?scope=all')

  const card = page.locator('article.community-post-card').filter({ has: page.getByRole('heading', { name: title }) })
  await expect(card).toBeVisible()
  await expect(card.getByText('demo-user')).toBeVisible()
  await expect(card.getByText('数据库', { exact: true })).toBeVisible()
  await expect(card.getByText('待讲师初审', { exact: true })).toBeVisible()

  const cardBox = await card.boundingBox()
  expect(cardBox?.width).toBeGreaterThan(620)

  const descriptionText = card.locator('.community-post-copy p')
  const collapsedDescriptionHeight = (await descriptionText.boundingBox())?.height ?? 0
  const expandDescriptionButton = card.getByRole('button', { name: /展开描述/ })
  await expect(expandDescriptionButton).toHaveAttribute('aria-expanded', 'false')
  await expandDescriptionButton.click()
  await expect(card.getByRole('button', { name: /收起描述/ })).toHaveAttribute('aria-expanded', 'true')
  const expandedDescriptionHeight = (await descriptionText.boundingBox())?.height ?? 0
  expect(expandedDescriptionHeight).toBeGreaterThan(collapsedDescriptionHeight + 20)

  const collapsedHeight = cardBox?.height ?? 0
  const expandButton = card.getByRole('button', { name: /展开详情/ })
  await expect(expandButton).toHaveAttribute('aria-expanded', 'false')
  const mainBeforeExpand = await card.locator('.community-post-main').boundingBox()
  await expandButton.click()
  await expect(card.getByText('CM 审核摘要')).toBeVisible()
  await expect(card.getByRole('button', { name: /收起详情/ })).toHaveAttribute('aria-expanded', 'true')
  const mainAfterExpand = await card.locator('.community-post-main').boundingBox()

  const expandedBox = await card.boundingBox()
  expect(expandedBox?.height).toBeGreaterThan(collapsedHeight + 80)
  expect(mainBeforeExpand).not.toBeNull()
  expect(mainAfterExpand).not.toBeNull()
  expect((mainAfterExpand?.width ?? 0) + 1).toBeGreaterThanOrEqual(mainBeforeExpand?.width ?? 0)
})

test('student community page renders dual-column workbench layout', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1024 })
  await mockCommunityApi(page)

  await loginAs(page, 'student')
  await page.goto('/community')

  await expect(page.getByRole('heading', { name: '案例工坊' })).toBeVisible()
  await expect(page.getByTestId('community-workbench')).toBeVisible()
  await expect(page.getByTestId('community-status-panel')).toBeVisible()
  await expect(page.getByText('把真实故障整理成可审核案例')).toBeVisible()
  await expect(page.getByText('我的案例状态')).toBeVisible()
  await expect(page.getByRole('button', { name: /发布预览|生成中/ })).toBeVisible()

  const workbenchBox = await page.getByTestId('community-workbench').boundingBox()
  const statusBox = await page.getByTestId('community-status-panel').boundingBox()
  expect(workbenchBox).not.toBeNull()
  expect(statusBox).not.toBeNull()
  expect((statusBox?.x ?? 0) - (workbenchBox?.x ?? 0)).toBeGreaterThan(320)
})

test('community status panel updates by role and deletion', async ({ page }) => {
  const state = await mockCommunityApi(page)
  const title = `E2E 状态统计案例 ${Date.now()}`

  await loginAs(page, 'student')
  await page.goto('/community')
  await page.getByLabel('案例标题').fill(title)
  await page.getByLabel('原始描述').fill('发布后缓存 key 规则变化，命中率下降，数据库读请求升高。')
  await page.getByRole('button', { name: /发布预览|生成中/ }).click()

  const statusPanel = page.getByTestId('community-status-panel')
  await expect(statusPanel.locator('.community-status-card').nth(1).locator('strong')).toHaveText('1')
  await expect(statusPanel).toContainText('讲师处理中')

  const createdCard = page.locator('article.community-post-card').filter({ has: page.getByRole('heading', { name: title }) })
  await createdCard.getByRole('button', { name: /删除案例|删除中/ }).click()
  await expect.poll(() => state.post).toBeUndefined()
  await expect(statusPanel.locator('.community-status-card').nth(1).locator('strong')).toHaveText('0')

  await page.route('**/api/v1/community/posts**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      list: [
        {
          ...communityPost('teacher-pending', '讲师待审核案例'),
          user_id: 'student-1',
          status: 'pending_review',
        },
        {
          ...communityPost('teacher-approved', '讲师已初审案例'),
          user_id: 'student-2',
          status: 'instructor_approved',
          review_history: [reviewHistoryItem(mockInstructorUserId, 'instructor_approve', 'pending_review', 'instructor_approved', '结构完整。')],
        },
      ],
    })
  })

  await resetSession(page)
  await loginAs(page, 'instructor')
  await page.goto('/community')
  const instructorStatusPanel = page.getByTestId('community-status-panel')
  await expect(instructorStatusPanel.locator('.community-status-card').nth(0)).toContainText('待初审')
  await expect(instructorStatusPanel.locator('.community-status-card').nth(1)).toContainText('我已初审')

  await page.route('**/api/v1/community/posts**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      list: [
        {
          ...communityPost('admin-final', '管理员终审案例'),
          user_id: 'student-3',
          status: 'instructor_approved',
        },
        {
          ...communityPost('admin-published', '管理员已发布案例'),
          user_id: 'student-4',
          status: 'published',
        },
      ],
    })
  })

  await resetSession(page)
  await loginAs(page, 'admin')
  await page.goto('/community')
  const adminStatusPanel = page.getByTestId('community-status-panel')
  await expect(adminStatusPanel.locator('.community-status-card').nth(0)).toContainText('终审队列')
  await expect(adminStatusPanel.locator('.community-status-card').nth(2)).toContainText('已发布')
})

async function mockCommunityApi(page: Page) {
  const state: { post?: Record<string, unknown>; getQueries: string[] } = { getQueries: [] }

  await page.route('**/api/v1/community/posts**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const pathname = url.pathname

    if (method === 'GET') {
      state.getQueries.push(url.search)
      const list = state.post && matchesCommunityMockFilter(state.post, url, mockInstructorUserId) ? [state.post] : []
      await fulfill(route, { list })
      return
    }

    if (method === 'POST' && pathname.endsWith('/community/posts')) {
      const body = request.postDataJSON() as { title: string; raw_content: string; domain: string; tags: string[] }
      state.post = {
        id: 'e2e-community-post',
        user_id: 'user-demo',
        author_username: 'demo-user',
        title: body.title,
        raw_content: maskSensitiveContent(body.raw_content),
        domain: body.domain,
        tags: body.tags,
        ai_structured_content: scenarioContent(),
        moderation_summary: {
          status: 'reviewed',
          risk_level: 'medium',
          recommendation: '建议人工重点复核',
          safe_summary: '案例结构基本完整，适合审核者快速浏览核心风险与证据。',
          safe_risk_note: '当前存在中风险项，建议先确认脱敏和证据链完整度。',
          safe_action_hint: '建议讲师确认风险项后，再决定是否进入终审。',
          safe_labels: ['当前状态:pending_review', '风险:medium', '需重点复核'],
          suggested_note: '建议补充审核意见，明确是否需要进一步修改。',
          reasons: ['已整理关键证据，可快速核对异常背景。', '当前仍有风险项，需要人工确认。'],
          flagged: true,
        },
        sensitive_check: body.raw_content.includes('password=')
          ? {
              status: 'risk',
              sanitized: true,
              findings: [
                {
                  type: 'password',
                  field: 'raw_content',
                  excerpt: 'password=********',
                  severity: 'high',
                  suggestion: '来源：原始描述；脱敏建议：删除或替换密码字段后再发布。',
                },
              ],
              checked_at: new Date().toISOString(),
            }
          : { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
        status: 'pending_review',
        created_at: new Date().toISOString(),
      }
      await fulfill(route, state.post)
      return
    }

    if (method === 'POST' && pathname.endsWith('/instructor-review') && state.post) {
      state.post = {
        ...state.post,
        status: 'instructor_approved',
        review_note: '结构清晰，可以提交终审。',
        edited_structured_content: scenarioContent(),
        review_history: [
          reviewHistoryItem(mockInstructorUserId, 'instructor_approve', 'pending_review', 'instructor_approved', '结构清晰，可以提交终审。'),
        ],
      }
      await fulfill(route, state.post)
      return
    }

    if (method === 'POST' && pathname.endsWith('/final-review') && state.post) {
      state.post = {
        ...state.post,
        status: 'published',
        final_note: '终审通过，发布为正式题。',
        converted_question_id: 'e2e-converted-scenario',
        review_history: [
          reviewHistoryItem(mockInstructorUserId, 'instructor_approve', 'pending_review', 'instructor_approved', '结构清晰，可以提交终审。'),
          reviewHistoryItem('admin-user', 'final_publish', 'instructor_approved', 'published', '终审通过，发布为正式题。'),
        ],
      }
      await fulfill(route, { post: state.post, question: convertedScenario(String(state.post.title)) })
      return
    }

    if (method === 'DELETE' && pathname.endsWith('/community/posts/e2e-community-post') && state.post) {
      const currentId = state.post.id
      state.post = undefined
      await fulfill(route, { deleted: true, id: currentId })
      return
    }

    await route.fallback()
  })

  return state
}

function matchesCommunityMockFilter(post: Record<string, unknown>, url: URL, currentUserId: string) {
  const status = url.searchParams.get('status') ?? ''
  if (status && post.status !== status) {
    return false
  }

  const view = url.searchParams.get('view') ?? ''
  if (view === 'instructor_reviewed') {
    return communityMockHistoryHas(post, currentUserId, 'instructor_approve')
  }
  if (view === 'instructor_rejected') {
    return communityMockHistoryHas(post, currentUserId, 'instructor_reject')
  }

  return true
}

function communityMockHistoryHas(post: Record<string, unknown>, actorId: string, action: string) {
  const history = post.review_history
  return Array.isArray(history) && history.some((item) => {
    if (!item || typeof item !== 'object') return false
    const historyItem = item as { actor_id?: unknown; action?: unknown }
    return historyItem.actor_id === actorId && historyItem.action === action
  })
}

test('community shows sensitive information notice', async ({ page }) => {
  await mockCommunityApi(page)
  const fullSecret = 'sk-live-1234567890abcdef'

  await loginAs(page, 'student')
  await page.goto('/community')

  await page.getByLabel('案例标题').fill('E2E 敏感信息案例')
  await page.getByLabel('原始描述').fill(`线上 password=${fullSecret} 导致缓存异常。`)
  await page.getByRole('button', { name: /发布预览|生成中/ }).click()

  const card = page.locator('article').filter({ has: page.getByRole('heading', { name: 'E2E 敏感信息案例' }) })
  await card.getByRole('button', { name: /展开详情/ }).click()
  const notice = card.locator('.sensitive-notice')
  await expect(notice).toContainText('敏感信息提示')
  await expect(notice).toContainText('password')
  await expect(notice).toContainText('来源：原始描述')
  await expect(notice).toContainText('脱敏建议：删除或替换密码字段后再发布。')
  await expect(notice).not.toContainText(fullSecret)
  await expect(page.locator('p, code, span, strong, small').filter({ hasText: fullSecret })).toHaveCount(0)
  await expectNoWhiteScreen(page)
})

test('admin refreshes current empty review queue without resetting filter', async ({ page }) => {
  const statuses: string[] = []

  await page.route('**/api/v1/community/posts**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    const url = new URL(route.request().url())
    statuses.push(url.searchParams.get('status') ?? '')
    await fulfill(route, { list: [] })
  })

  await loginAs(page, 'admin')
  await page.goto('/community?status=pending_review')

  await expect(page.getByText('暂无案例')).toBeVisible()
  await expect(page.getByRole('button', { name: '待初审' })).toBeVisible()
  const pendingReviewQueriesBeforeRefresh = statuses.filter((status) => status === 'pending_review').length
  await page.getByRole('button', { name: '刷新队列' }).click()

  await expect(page).toHaveURL(/\/community\?status=pending_review/)
  await expect(page.getByRole('button', { name: '待初审' })).toHaveClass(/active/)
  await expect.poll(() => statuses.filter((status) => status === 'pending_review').length).toBeGreaterThan(pendingReviewQueriesBeforeRefresh)
})

test('community draft submit persists current edits first', async ({ page }) => {
  const calls: string[] = []
  let post = {
    id: 'e2e-draft-post',
    user_id: 'user-demo',
    title: 'E2E 派生草稿',
    raw_content: '原始派生现象。',
    domain: 'database',
    tags: ['派生', 'AI生成', '娲剧敓'],
    forked_from_scenario_id: 'scenario-db-index',
    ai_structured_content: {
      ...scenarioContent(),
      root_cause: '',
      root_cause_keywords: [],
      key_evidence: [],
      standard_procedure: [],
    },
    edited_structured_content: {
      ...scenarioContent(),
      root_cause: '',
      root_cause_keywords: [],
      key_evidence: [],
      standard_procedure: [],
    },
    sensitive_check: { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
    review_history: [],
    status: 'draft',
    created_at: new Date().toISOString(),
  }

  await page.route('**/api/v1/community/posts**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const pathname = url.pathname

    if (method === 'GET') {
      await fulfill(route, { list: [post] })
      return
    }
    if (method === 'PUT' && pathname.endsWith('/community/posts/e2e-draft-post')) {
      calls.push('update')
      const body = request.postDataJSON() as {
        title: string
        raw_content: string
        tags: string[]
        structured_content: ReturnType<typeof scenarioContent>
      }
      expect(body.title).toBe('E2E 已编辑派生草稿')
      expect(body.raw_content).toBe('提交前编辑过的现象描述。')
      expect(body.tags).toContain('补齐')
      expect(body.tags).not.toContain('娲剧敓')
      expect(body.structured_content.root_cause).toBe('提交前补齐的根因。')
      expect(body.structured_content.key_evidence).toContain('证据一')
      expect(body.structured_content.standard_procedure).toContain('步骤一')
      post = {
        ...post,
        title: body.title,
        raw_content: body.raw_content,
        tags: body.tags,
        edited_structured_content: body.structured_content,
      }
      await fulfill(route, post)
      return
    }
    if (method === 'POST' && pathname.endsWith('/community/posts/e2e-draft-post/submit')) {
      calls.push('submit')
      post = { ...post, status: 'pending_review' }
      await fulfill(route, post)
      return
    }

    await route.fallback()
  })

  await loginAs(page, 'student')
  await page.goto('/community')
  await expect(page.getByRole('heading', { name: 'E2E 派生草稿' })).toBeVisible()

  const tagInput = page.getByLabel('标签')
  await expect(tagInput).toHaveValue('派生,AI生成')
  await expect(tagInput).not.toHaveValue(/娲剧敓/)

  const forkSourceNote = page.locator('.community-fork-note')
  await expect(forkSourceNote).toBeVisible()
  await expect(forkSourceNote).not.toHaveCSS('background-color', 'rgb(244, 247, 245)')
  await expect(forkSourceNote).toHaveCSS('color', 'rgb(220, 228, 238)')

  const rootCauseEditor = page.getByLabel('根因修订')
  await expect(rootCauseEditor).not.toHaveCSS('background-color', 'rgb(255, 255, 255)')
  await expect(rootCauseEditor).toHaveCSS('color', 'rgb(246, 250, 254)')

  await page.getByLabel('草稿标题').fill('E2E 已编辑派生草稿')
  await page.getByLabel('现象描述').fill('提交前编辑过的现象描述。')
  await tagInput.fill('派生,补齐,娲剧敓')
  await page.getByLabel('根因修订').fill('提交前补齐的根因。')
  await page.getByLabel('关键证据').fill('证据一\n证据二')
  await page.getByLabel('标准步骤').fill('步骤一\n步骤二')
  await page.getByRole('button', { name: /提交初审/ }).click()

  await expect.poll(() => calls).toEqual(['update', 'submit'])
  await expect(page.getByRole('heading', { name: 'E2E 已编辑派生草稿' })).toBeVisible()
  const submittedCard = page.locator('article.community-post-card').filter({ has: page.getByRole('heading', { name: 'E2E 已编辑派生草稿' }) })
  await expect(submittedCard.locator('.scenario-meta').getByText('待讲师初审', { exact: true })).toBeVisible()
})

test('profile shows own community post history', async ({ page }) => {
  await page.route('**/api/v1/users/me/history', async (route) => {
    await fulfill(route, {
      scenarios: [],
      interviews: [],
      community_posts: [{
        id: 'profile-community-post',
        user_id: 'user-demo',
        title: '个人档案投稿回显案例',
        raw_content: '发布后缓存 key 规则变化，命中率下降。',
        domain: 'database',
        tags: ['缓存', '投稿'],
        ai_structured_content: scenarioContent(),
        sensitive_check: { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
        review_history: [
          reviewHistoryItem(mockInstructorUserId, 'instructor_approve', 'pending_review', 'instructor_approved', '结构完整。'),
          reviewHistoryItem('admin-user', 'final_publish', 'instructor_approved', 'published', '发布为正式题。'),
        ],
        status: 'published',
        converted_question_id: 'profile-converted-scenario',
        created_at: new Date().toISOString(),
      }],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/profile')

  await expect(page.getByRole('heading', { name: '个人档案' })).toBeVisible()
  const panel = page.locator('.panel').filter({ hasText: '我的案例投稿' })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('个人档案投稿回显案例')
  await expect(panel).toContainText('已发布题库')
  await expect(panel).toContainText('已转题')
  await expect(page.locator('body')).not.toContainText('agent_trace')
  await expectNoWhiteScreen(page)
})

async function mockConvertedScenario(page: Page, title: string) {
  await page.route('**/api/v1/scenarios**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [convertedScenario(title)], total: 1 })
  })
}

async function fulfill(route: Parameters<Parameters<Page['route']>[1]>[0], data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

function scenarioContent() {
  return {
    root_cause: '缓存 key 规则变化导致命中率下降。',
    root_cause_keywords: ['缓存', 'key', '命中率'],
    key_evidence: ['缓存命中率下降', '数据库读请求升高'],
    standard_procedure: ['确认变更窗口', '对比缓存 key 规则', '回滚或兼容旧 key'],
    reveal_strategy: {
      surface_clues: [],
      deep_clues: [],
      distractors: [],
    },
    architecture_diagram: 'graph TD\n  A[应用] --> B[缓存]\n  A --> C[数据库]',
    reference_links: [],
  }
}

function convertedScenario(title: string) {
  return {
    id: 'e2e-converted-scenario',
    title,
    description: '发布后缓存 key 规则变化，命中率下降，数据库读请求升高。',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['缓存', '变更'],
    content: scenarioContent(),
    status: 'active',
    source: 'ugc_structured',
    created_by: 'admin-user',
    version: 1,
    is_sanitized: true,
  }
}

function communityPost(id: string, title: string) {
  return {
    id,
    user_id: 'user-demo',
    author_username: 'demo-user',
    title,
    raw_content: '发布后缓存 key 规则变化，命中率下降，数据库读请求升高。',
    domain: 'database',
    tags: ['缓存', '变更'],
    ai_structured_content: scenarioContent(),
    sensitive_check: { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
    review_history: [],
    status: 'pending_review',
    created_at: new Date().toISOString(),
  }
}

function maskSensitiveContent(content: string) {
  return content.replace(/password=[^\s。；;]+/gi, 'password=********')
}

function reviewHistoryItem(actorId: string, action: string, fromStatus: string, toStatus: string, note: string) {
  return {
    id: `${action}-${Date.now()}`,
    actor_id: actorId,
    action,
    from_status: fromStatus,
    to_status: toStatus,
    note,
    content: scenarioContent(),
    created_at: new Date().toISOString(),
  }
}
