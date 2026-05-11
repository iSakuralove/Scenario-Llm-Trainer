import { expect, type Page, type Route, test } from '@playwright/test'

test('interview launchpad explains roles, domains, available tracks, and request payload', async ({ page }) => {
  let requestedPayload: Record<string, string> | undefined
  await mockShellAPI(page)
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    requestedPayload = route.request().postDataJSON() as Record<string, string>
    const question = interviewQuestionFromTrack(requestedPayload)
    await fulfillJSON(route, {
      session_id: 'launchpad-session',
      status: 'active',
      question,
      session: interviewSession('launchpad-session', question.id),
    })
  })

  await openInterviewsAsStudent(page)

  await expect(page.getByRole('heading', { name: '技术面试舱' })).toBeVisible()
  await expect(page.locator('.interview-launchpad')).toContainText('INTERVIEW')
  await expect(page.getByTestId('interview-launch-summary')).toContainText('五维评分维度')
  await expect(page.getByTestId('interview-launch-summary')).toContainText('技术准确性')
  await expect(page.locator('.interview-poster-meta strong')).toHaveCount(0)
  await expect.poll(async () =>
    page.getByTestId('interview-launch-summary').evaluate((item) => getComputedStyle(item, '::after').content),
  ).toBe('none')
  await expect(page.locator('.interview-launchpad')).not.toContainText('选择真实可启动训练轨道，进入结构化问答、追问和五维评分。')
  await expect(page.locator('.interview-launchpad')).not.toContainText('只展示真实可启动组合')
  await expect(page.locator('.interview-launchpad')).not.toContainText('后续题库建设范围')
  await expect(page.locator('.interview-launchpad')).not.toContainText('题库待补齐')
  await expect(page.getByTestId('interview-level-table')).toContainText('L3')
  await expect(page.getByTestId('interview-level-table')).toContainText('初级工程师')
  await expect(page.getByTestId('interview-level-table')).toContainText('L4')
  await expect(page.getByTestId('interview-level-table')).toContainText('中级工程师')
  await expect(page.getByTestId('interview-level-table')).toContainText('L5')
  await expect(page.getByTestId('interview-level-table')).toContainText('高级工程师')

  await expect(page.getByTestId('interview-domain-cloud-native')).toContainText('deepseek-v4-flash 基线题库')
  await expect(page.getByRole('button', { name: /云原生/ })).toHaveCount(1)
  await expect(page.getByTestId('interview-domain-mq-cache')).toBeVisible()
  await expect(page.getByTestId('interview-domain-architecture')).toBeVisible()
  await expect(page.getByRole('radio', { name: /云原生 L4/ })).toBeVisible()
  await expect(page.getByRole('radio', { name: /架构设计 L5/ })).toBeVisible()
  await expect(page.getByRole('radio', { name: /数据库 L3/ })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByRole('radio', { name: /网络 L3/ })).toHaveAttribute('aria-checked', 'false')

  await page.getByRole('radio', { name: /数据库 L3/ }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('radio', { name: /网络 L3/ })).toHaveAttribute('aria-checked', 'true')
  await page.keyboard.press('Home')
  await expect(page.getByRole('radio', { name: /数据库 L3/ })).toHaveAttribute('aria-checked', 'true')
  await page.keyboard.press('End')
  await expect(page.getByRole('radio', { name: /架构设计 L5/ })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('interview-launch-summary')).toContainText('架构设计')
  await expect(page.getByTestId('interview-launch-summary')).toContainText('L5')
  await page.getByTestId('interview-track-section').getByRole('button', { name: /开始面试/ }).click()

  await expect.poll(() => requestedPayload).toEqual({
    domain: 'architecture',
    difficulty: 'L5',
    question_type: 'principle',
  })
  await expect(page).toHaveURL(/\/interviews\/session\/launchpad-session$/)
})

test('interview launchpad blocks mismatched question responses', async ({ page }) => {
  await mockShellAPI(page)
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, {
      session_id: 'mismatched-session',
      status: 'active',
      question: { ...interviewQuestion(), domain: 'database' },
      session: interviewSession('mismatched-session'),
    })
  })

  await openInterviewsAsStudent(page)

  await page.getByRole('radio', { name: /网络 L3/ }).click()
  await page.getByTestId('interview-track-section').getByRole('button', { name: /开始面试/ }).click()

  await expect(page.locator('.launch-error').first()).toContainText('题目与所选训练轨道不一致')
  await expect(page).toHaveURL(/\/interviews$/)
})

test('interview launchpad shows backend not found errors without leaving launchpad', async ({ page }) => {
  await mockShellAPI(page)
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ code: 404, message: 'interview question not found' }),
    })
  })

  await openInterviewsAsStudent(page)

  await page.getByTestId('interview-track-section').getByRole('button', { name: /开始面试/ }).click()

  await expect(page.locator('.launch-error').first()).toContainText('interview question not found')
  await expect(page).toHaveURL(/\/interviews$/)
})

test('interview launchpad keeps role table readable on tablet and mobile widths', async ({ page }) => {
  await mockShellAPI(page)
  await page.setViewportSize({ width: 1024, height: 768 })
  await openInterviewsAsStudent(page)

  await expectNoHorizontalOverflow(page)
  await expectLevelRowsReadable(page)
  await expectLaunchGridsSingleColumn(page)

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoHorizontalOverflow(page)
  await expectLevelRowsReadable(page)
})

test('interview launchpad gives available tracks more width than level guidance on desktop', async ({ page }) => {
  await mockShellAPI(page)
  await page.setViewportSize({ width: 1440, height: 955 })
  await openInterviewsAsStudent(page)

  await expect.poll(async () =>
    page.evaluate(() => {
      const trackSection = document.querySelector<HTMLElement>('[data-testid="interview-track-section"]')
      const levelSection = document.querySelector<HTMLElement>('[data-testid="interview-level-section"]')
      if (!trackSection || !levelSection) return false
      const tracks = trackSection.getBoundingClientRect()
      const levels = levelSection.getBoundingClientRect()
      const levelTitle = levelSection.querySelector<HTMLElement>('.panel-title')
      const levelFontSize = levelTitle ? Number.parseFloat(getComputedStyle(levelTitle).fontSize) : 0
      return tracks.width > levels.width && levelFontSize <= 15
    }),
  ).toBe(true)
})

test('interview launchpad avoids oversized empty bands on desktop', async ({ page }) => {
  await mockShellAPI(page)
  await page.setViewportSize({ width: 1267, height: 715 })
  await openInterviewsAsStudent(page)

  await expectNoHorizontalOverflow(page)
  await expectWorkspaceClearOfSidebar(page)
  await expect.poll(async () =>
    page.evaluate(() => {
      const grid = document.querySelector<HTMLElement>('.interview-command-grid')
      const level = document.querySelector<HTMLElement>('[data-testid="interview-level-section"]')
      if (!grid || !level) return false
      const levelBox = level.getBoundingClientRect()
      const gridStyle = getComputedStyle(grid)
      const levelOverflow = level.scrollWidth - level.clientWidth
      const panels = Array.from(grid.children).map((item) => item.getBoundingClientRect()).sort((a, b) => a.top - b.top)
      const maxPanelGap = panels.slice(1).reduce((maxGap, panel, index) => Math.max(maxGap, panel.top - panels[index].bottom), 0)
      return gridStyle.gridTemplateColumns.split(' ').length === 1 && levelBox.width >= 300 && levelOverflow <= 2 && maxPanelGap <= 30
    }),
  ).toBe(true)
})

test('interview launchpad keeps the left navigation visible on zoomed desktop widths', async ({ page }) => {
  await mockShellAPI(page)
  await page.setViewportSize({ width: 1241, height: 628 })
  await openInterviewsAsStudent(page)

  await expectNoHorizontalOverflow(page)
  await expectLaunchGridsSingleColumn(page)
  await expect.poll(async () =>
    page.evaluate(() => {
      const shell = document.querySelector<HTMLElement>('.app-shell')
      const sidebar = document.querySelector<HTMLElement>('[data-testid="global-sidebar"]')
      const workspace = document.querySelector<HTMLElement>('.workspace')
      const launchpad = document.querySelector<HTMLElement>('.interview-launchpad')
      if (!shell || !sidebar || !workspace || !launchpad) return false
      const shellColumns = getComputedStyle(shell).gridTemplateColumns.split(' ').filter(Boolean)
      const sidebarBox = sidebar.getBoundingClientRect()
      const workspaceBox = workspace.getBoundingClientRect()
      const launchpadBox = launchpad.getBoundingClientRect()
      const firstNavItem = sidebar.querySelector<HTMLElement>('.nav-list a')
      const firstNavBox = firstNavItem?.getBoundingClientRect()
      return (
        shellColumns.length === 2
        && getComputedStyle(sidebar).position === 'sticky'
        && sidebarBox.left <= 1
        && sidebarBox.right <= workspaceBox.left + 1
        && launchpadBox.left >= sidebarBox.right
        && Boolean(firstNavBox && firstNavBox.width >= 120 && firstNavBox.height >= 40)
      )
    }),
  ).toBe(true)
})

test('interview launchpad keeps compact top navigation readable on narrow screens', async ({ page }) => {
  await mockShellAPI(page)
  await page.setViewportSize({ width: 685, height: 955 })
  await openInterviewsAsStudent(page)

  await expectNoHorizontalOverflow(page)
  await expect.poll(async () =>
    page.evaluate(() => {
      const shell = document.querySelector<HTMLElement>('.app-shell')
      const sidebar = document.querySelector<HTMLElement>('[data-testid="global-sidebar"]')
      const workspace = document.querySelector<HTMLElement>('.workspace')
      if (!shell || !sidebar || !workspace) return false
      const firstNavItem = sidebar.querySelector<HTMLElement>('.nav-list a')
      const firstIcon = firstNavItem?.querySelector<SVGElement>('svg')
      if (!firstNavItem || !firstIcon) return false
      const sidebarBox = sidebar.getBoundingClientRect()
      const workspaceBox = workspace.getBoundingClientRect()
      const navBox = firstNavItem.getBoundingClientRect()
      const navStyle = getComputedStyle(firstNavItem)
      const iconStyle = getComputedStyle(firstIcon)
      return (
        getComputedStyle(shell).gridTemplateColumns.split(' ').filter(Boolean).length === 1
        && getComputedStyle(sidebar).position === 'static'
        && workspaceBox.top >= sidebarBox.bottom
        && firstNavItem.innerText.includes('\u4eea\u8868\u76d8')
        && navBox.width >= 120
        && navBox.height >= 40
        && navStyle.visibility === 'visible'
        && navStyle.opacity !== '0'
        && navStyle.color !== 'rgba(0, 0, 0, 0)'
        && iconStyle.visibility === 'visible'
        && iconStyle.opacity !== '0'
      )
    }),
  ).toBe(true)
})

test('interview launchpad keeps expanded track lists in a bounded scroll region', async ({ page }) => {
  await mockShellAPI(page)
  await mockExpandedTracks(page)
  await openInterviewsAsStudent(page)

  const trackGrid = page.getByTestId('interview-track-grid')
  await expect(trackGrid.getByRole('radio')).toHaveCount(24)
  await expect.poll(async () => trackGrid.evaluate((item) => item.scrollHeight > item.clientHeight)).toBe(true)
  await expect.poll(async () => trackGrid.evaluate((item) => item.getBoundingClientRect().height <= 520)).toBe(true)
  await expectNoHorizontalOverflow(page)

  await page.getByRole('radio', { name: /数据库 L3/ }).focus()
  await page.keyboard.press('End')
  await expect(page.getByRole('radio', { name: /架构设计 L5/ })).toHaveAttribute('aria-checked', 'true')
  await expect
    .poll(async () => {
      return await trackGrid.evaluate((item) => {
        const active = item.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')
        if (!active) return false
        const activeRect = active.getBoundingClientRect()
        const gridRect = item.getBoundingClientRect()
        return activeRect.top >= gridRect.top && activeRect.bottom <= gridRect.bottom
      })
    })
    .toBe(true)
})

test('interview launchpad keeps track card detail lines readable after domain expansion', async ({ page }) => {
  await mockShellAPI(page)
  await openInterviewsAsStudent(page)

  const targetCard = page.getByRole('radio', { name: /缓存与消息队列 L4/ })
  await expect(targetCard).toBeVisible()

  const detailMetrics = await targetCard.evaluate((item) => {
    const detail = item.querySelector('small')
    if (!detail) return null
    const itemRect = item.getBoundingClientRect()
    const detailRect = detail.getBoundingClientRect()
    return {
      itemBottom: itemRect.bottom,
      detailBottom: detailRect.bottom,
      detailHeight: detailRect.height,
      detailTop: detailRect.top - itemRect.top,
    }
  })

  expect(detailMetrics).not.toBeNull()
  expect(detailMetrics!.detailHeight).toBeGreaterThan(12)
  expect(detailMetrics!.detailTop).toBeGreaterThan(50)
  expect(detailMetrics!.detailBottom).toBeLessThanOrEqual(detailMetrics!.itemBottom + 1)
})

test('interview launchpad can delete history records', async ({ page }) => {
  await mockShellAPI(page)
  const historySessions = [
    {
      id: 'e2e-history-delete',
      user_id: 'demo-user',
      question_id: 'interview-history-question',
      status: 'question_presented',
      current_round: 1,
      max_rounds: 3,
      evaluations: [],
      submissions: [],
      started_at: new Date().toISOString(),
    },
  ]

  await page.route('**/api/v1/users/me/history', async (route) => {
    await fulfillJSON(route, {
      scenarios: [],
      interviews: historySessions,
      community_posts: [],
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-history-delete', async (route) => {
    if (route.request().method() === 'DELETE') {
      historySessions.splice(0, historySessions.length)
      await fulfillJSON(route, { deleted: true, id: 'e2e-history-delete' })
      return
    }
    await route.fallback()
  })

  await openInterviewsAsStudent(page)

  const historyPanel = page.getByTestId('interview-history-panel')
  await expect(historyPanel).toContainText('1 条记录')
  await expect(historyPanel).toContainText('面试 #e2e-hist')
  await historyPanel.getByRole('button', { name: /删除记录/ }).click()
  await expect(historyPanel).toContainText('0 条记录')
  await expect(historyPanel).toContainText('暂无历史面试记录')
})

async function fulfillJSON(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

async function openInterviewsAsStudent(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('teaching_mvp_access', 'e2e-token')
    window.localStorage.setItem('teaching_mvp_refresh', 'e2e-refresh')
  })
  await page.goto('/interviews')
  await expect(page.getByTestId('interview-track-grid')).toBeVisible()
}

async function mockShellAPI(page: Page) {
  await page.route('**/api/v1/auth/login', async (route) => {
    await fulfillJSON(route, { user: demoUser(), access_token: 'e2e-token', refresh_token: 'e2e-refresh' })
  })
  await page.route('**/api/v1/users/me', async (route) => {
    await fulfillJSON(route, demoUser())
  })
  await page.route('**/api/v1/users/me/dashboard', async (route) => {
    await fulfillJSON(route, {
      capability_radar: {},
      weak_points: [],
      recommendations: [],
      learning_plan: { generated_at: new Date().toISOString(), focus_domains: [], insights: [], recommendations: [] },
      review_calendar: { generated_at: new Date().toISOString(), items: [] },
      stats: { scenarios_solved: 0, interviews_taken: 0, average_score: 0, streak_days: 0 },
    })
  })
  await page.route('**/api/v1/ai/status', async (route) => {
    await fulfillJSON(route, { mode: 'mock', provider: 'mock', model: 'mock' })
  })
}

async function mockExpandedTracks(page: Page) {
  await page.route('**/src/features/interviews/launchpadConfig.ts*', async (route) => {
    const response = await route.fetch()
    const source = await response.text()
    await route.fulfill({
      response,
      body: buildExpandedLaunchpadConfig(source),
    })
  })
}

function demoUser() {
  return {
    id: 'demo-user',
    username: 'demo',
    email: 'demo@example.com',
    role: 'student',
    profile: {
      target_level: 'intermediate',
      preferred_domains: ['database', 'network'],
      capability_radar: {},
      weak_points: [],
      total_stats: { scenarios_solved: 0, interviews_taken: 0, average_score: 0, streak_days: 0 },
      updated_at: new Date().toISOString(),
    },
    created_at: new Date().toISOString(),
  }
}

function buildExpandedLaunchpadConfig(source: string) {
  const domains = ['数据库', '网络', '操作系统', '安全', 'DevOps', '后端工程', '分布式系统', '云原生', '缓存与消息队列', '可观测性', '性能优化', '架构设计']
  const values = ['database', 'network', 'os', 'security', 'devops', 'backend', 'distributed', 'cloud-native', 'mq-cache', 'observability', 'performance', 'architecture']
  const tracks = domains.flatMap((label, domainIndex) =>
    (domainIndex === domains.length - 1 ? ['L3', 'L5'] : ['L3', 'L4']).map((difficulty) => ({
      id: `${values[domainIndex]}-${difficulty.toLowerCase()}-scenario`,
      title: `${label} ${difficulty}`,
      domain: values[domainIndex],
      domainLabel: label,
      difficulty,
      questionType: 'scenario_analysis',
      summary: `${label} ${difficulty} 轨道的工程情景分析训练，覆盖定位、方案和复盘。`,
      details: ['情景分析', '最多 3 轮追问', `${label} 专项题库`],
    })),
  )
  const replacement = `export const interviewLaunchTracks = ${JSON.stringify(tracks, null, 2)}`
  return source.replace(/export const interviewLaunchTracks[^=]*= \[[\s\S]*?\]\s*;?\s*export const interviewFlowSteps/, `${replacement}\n\nexport const interviewFlowSteps`)
}

function interviewQuestion() {
  return {
    id: 'interview-network-timeout',
    title: '如何排查跨服务调用超时',
    description: '微服务之间出现间歇性超时，重试后成功。请给出从应用到网络基础设施的排查路径。',
    domain: 'network',
    difficulty: 'L3',
    question_type: 'scenario_analysis',
    evaluation_dimensions: [],
    follow_up_strategies: [],
  }
}

function interviewQuestionFromTrack(track: Record<string, string> | undefined) {
  if (track?.domain === 'architecture') {
    return {
      id: 'interview-architecture-multi-active',
      title: '多活架构下如何处理跨地域一致性',
      description: '公司计划把核心交易系统升级为双活架构。请说明你会如何划分一致性等级、处理流量切换和设计演进路径。',
      domain: 'architecture',
      difficulty: 'L5',
      question_type: 'principle',
      evaluation_dimensions: [],
      follow_up_strategies: [],
    }
  }
  return interviewQuestion()
}

function interviewSession(sessionId: string, questionId = 'interview-network-timeout') {
  return {
    id: sessionId,
    user_id: 'demo-user',
    question_id: questionId,
    status: 'question_presented',
    current_round: 1,
    max_rounds: 3,
    submissions: [],
    evaluations: [],
  }
}

async function expectNoHorizontalOverflow(page: Page) {
  await expect.poll(async () => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
}

async function expectWorkspaceClearOfSidebar(page: Page) {
  await expect.poll(async () =>
    page.evaluate(() => {
      const sidebar = document.querySelector<HTMLElement>('[data-testid="global-sidebar"]')
      const workspace = document.querySelector<HTMLElement>('.workspace')
      const launchpad = document.querySelector<HTMLElement>('.interview-launchpad')
      if (!workspace || !launchpad) return false
      if (!sidebar || sidebar.hidden || getComputedStyle(sidebar).display === 'none') {
        return workspace.getBoundingClientRect().left >= 0 && launchpad.getBoundingClientRect().left >= 0
      }
      const sidebarBox = sidebar.getBoundingClientRect()
      const workspaceBox = workspace.getBoundingClientRect()
      const launchpadBox = launchpad.getBoundingClientRect()
      if (getComputedStyle(sidebar).position === 'static') {
        return workspaceBox.top >= sidebarBox.bottom && launchpadBox.top >= sidebarBox.bottom
      }
      return workspaceBox.left >= sidebarBox.right && launchpadBox.left >= sidebarBox.right
    }),
  ).toBe(true)
}

async function expectLevelRowsReadable(page: Page) {
  await expect.poll(async () => page.locator('.level-row p').evaluateAll((items) => items.every((item) => item.getBoundingClientRect().width >= 180))).toBe(true)
}

async function expectLaunchGridsSingleColumn(page: Page) {
  await expect.poll(async () =>
    page.locator('.interview-command-grid').evaluateAll((items) => items.length > 0 && items.every((item) => getComputedStyle(item).gridTemplateColumns.split(' ').length === 1)),
  ).toBe(true)
}
