import { chromium } from 'playwright'

const baseURL = process.env.FRONTEND_BASE_URL || 'http://localhost:5173'

async function main() {
  const browser = await chromium.launch({ headless: true, channel: process.platform === 'win32' ? 'chrome' : undefined })
  const page = await browser.newPage()
  const runtimeErrors = []

  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('Failed to load resource')) {
      runtimeErrors.push(`console:${message.text()}`)
    }
  })
  page.on('pageerror', (error) => {
    runtimeErrors.push(`pageerror:${error?.message || String(error)}`)
  })

  try {
    await page.goto(`${baseURL}/`, { waitUntil: 'networkidle', timeout: 30_000 })
	await assertVisible(page, '.auth-layout', '登录页容器未渲染')
	await assertNoErrorFallback(page)
	await fillLoginForm(page)
	await page.locator('input[type="password"]').press('Enter')
    await page.waitForURL(/\/dashboard$/, { timeout: 30_000 })
    await page.waitForSelector('.app-shell', { state: 'visible', timeout: 30_000 })
    await assertVisible(page, '.app-shell', '登录后主工作区未渲染')
    await page.waitForFunction(() => document.body.innerText.includes('学习仪表盘'), null, { timeout: 30_000 })
    await assertText(page, '学习仪表盘', '仪表盘标题未渲染')
    await page.goto(`${baseURL}/profile`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('个人档案'), null, { timeout: 30_000 })
    await assertText(page, '个人档案', '个人档案页未渲染')
    await assertText(page, '导入简历文本', '个人档案页未渲染简历导入入口')
    await assertNoErrorFallback(page)
    await page.goto(`${baseURL}/scenarios`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('排查工坊'), null, { timeout: 30_000 })
    await assertText(page, '排查工坊', '排查工坊页未渲染')
    await assertNoErrorFallback(page)
    await page.route('**/api/v1/interviews/launchpad', async (route) => {
      await delay(800)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 200,
          message: 'ok',
          data: buildMockInterviewLaunchpadPayload(),
        }),
      })
    })
    await page.route('**/api/v1/users/me/history', async (route) => {
      await delay(800)
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 500,
          message: 'history failed',
        }),
      })
    })
    await page.route('**/api/v1/users/me/mentor', async (route) => {
      await delay(10)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 200,
          message: 'ok',
          data: {
            generated_at: new Date().toISOString(),
            overview: '目标职级为 中级，建议先围绕 dns、安全、操作系统 建立训练样本。',
            strengths: ['数据库 当前表现较稳，画像分 72。', '网络 当前表现较稳，画像分 64。'],
            weaknesses: ['dns 仍需补强，画像分 50。', '安全 仍需补强，画像分 50。'],
            risks: [
              { level: 'danger', title: '覆盖率偏低', message: '当前开放轨道覆盖率仅 0% ，建议优先补齐待补方向。' },
              { level: 'info', title: '档案信息不足', message: '补充简历摘要或项目摘要后，Mentor 建议和面试追问会更贴近你的真实背景。' },
            ],
            actions: [
              { title: 'DNS解析返回错误IP地址的排障', detail: 'AI 推荐：dns 当前画像分为 50，适合通过情景排查补强。', action_label: '进入排查工坊', action_path: '/scenarios' },
              { title: 'dns专项面试追问', detail: 'AI 推荐：围绕 dns 做一次面试模拟，验证能否把排查过程讲清楚。', action_label: '进入面试舱', action_path: '/interviews' },
            ],
            coverage: {
              coverage_percent: 0,
              completed_sessions: 0,
              subject_count: 0,
              top_subjects: [],
              uncovered_tracks: ['Java / L3'],
            },
            profile: {
              target_level: 'intermediate',
              target_role: '',
              preferred_domains: ['database', 'network', 'os'],
              has_resume_summary: false,
              has_project_summary: false,
            },
            sample_ready: false,
          },
        }),
      })
    })
    await page.route('**/api/v1/interviews/sessions', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 200,
          message: 'ok',
          data: {
            session_id: 'mock-interview-session',
            status: 'question_presented',
            question: {
              id: 'mock-question-1',
              title: '数据库 L3',
              description: '数据库排障训练',
              domain: 'database',
              difficulty: 'L3',
              question_type: 'scenario_analysis',
              reference_answer: '',
              reference_keywords: [],
              evaluation_dimensions: [],
              follow_up_strategies: [],
              created_at: new Date().toISOString(),
            },
            session: {
              id: 'mock-interview-session',
              user_id: 'demo',
              question_id: 'mock-question-1',
              status: 'question_presented',
              current_round: 0,
              max_rounds: 3,
              started_at: new Date().toISOString(),
              evaluations: [],
              submissions: [],
              selected_atom_snapshots: [],
              setup_notes: '',
              focus_areas: [],
            },
          },
        }),
      })
	})
	await page.goto(`${baseURL}/interviews`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
	await page.waitForSelector('[data-testid="launchpad-track-loading-skeleton"]', { state: 'visible', timeout: 5_000 })
	await page.waitForFunction(() => document.body.innerText.includes('load average 高但 CPU 不高怎么排查'), null, { timeout: 30_000 })
	await assertText(page, '技术面试舱', '面试舱页面未渲染')
	await assertText(page, 'load average 高但 CPU 不高怎么排查', '面试舱未展示操作系统题目')
	await assertText(page, '历史面试', '面试舱历史折叠区未渲染')
	const questionCards = page.locator('[data-testid="interview-track-grid"] [role="radio"]')
	if (await questionCards.count() !== 5) throw new Error('面试舱未展示五张题目卡片')
	if (await page.locator('#interview-shared-setup').count() !== 0) throw new Error('未选题时不应展示面试设置')
	await assertNoText(page, '兼容模式', '面试舱仍暴露兼容模式说明')
	await assertNoText(page, '可启动组合', '面试舱仍展示内部启动组合统计')
	await assertNoText(page, '五维评分维度', '面试舱仍展示评分说明')
	await assertNoText(page, 'ANSWER PIPELINE', '面试舱仍包含说明型英文流程面板')
	await assertNoText(page, '评分与报告产物', '面试舱仍包含说明型报告产物面板')
	await assertNoText(page, '岗位级别模型', '面试舱仍包含说明型岗位模型面板')
	await assertNoText(page, '面试流程', '面试舱仍包含说明型流程面板')
	await page.locator('select[aria-label="按领域筛选面试题"]').selectOption('os')
	await page.waitForFunction(() => document.querySelectorAll('[data-testid="interview-track-grid"] [role="radio"]').length === 1, null, { timeout: 30_000 })
	await page.locator('select[aria-label="按领域筛选面试题"]').selectOption('')
	await page.locator('[data-testid="interview-track-grid"] [role="radio"]').filter({ hasText: '数据库' }).first().click()
	await page.waitForSelector('#interview-shared-setup', { state: 'visible', timeout: 5_000 })
	if (await page.locator('#interview-shared-setup input[type="checkbox"]').count() !== 5) throw new Error('共享设置未展示可勾选的追问重点')
	await page.locator('.interview-start-button').click()
    await page.waitForURL(/\/interviews\/session\/mock-interview-session$/, { timeout: 30_000 })
    await assertNoErrorFallback(page)
    await page.goto(`${baseURL}/mentor`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('AI Mentor'), null, { timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('综合诊断'), null, { timeout: 30_000 })
    await assertText(page, '风险预警', 'Mentor 页未渲染风险预警')
    await assertText(page, '建议行动', 'Mentor 页未渲染建议行动')
    await assertText(page, '知识覆盖', 'Mentor 页未渲染知识覆盖')
    await assertNoErrorFallback(page)
    await page.goto(`${baseURL}/system`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForURL(/\/dashboard$/, { timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('学习仪表盘'), null, { timeout: 30_000 })
    await assertText(page, '学习仪表盘', '学生访问系统页后未回到仪表盘')
    await assertNoErrorFallback(page)
    if (runtimeErrors.length > 0) {
      throw new Error(`发现前端运行时错误: ${runtimeErrors.join(' | ')}`)
    }
    console.log(`frontend smoke passed: ${baseURL}`)
  } finally {
    await browser.close()
  }
}

async function fillLoginForm(page) {
  const inputs = page.locator('input')
  const count = await inputs.count()
  if (count < 2) {
    throw new Error(`登录页输入框数量不足，当前为 ${count}`)
  }
  await inputs.nth(0).fill('demo')
  await inputs.nth(1).fill('demo123')
}

async function assertVisible(page, selector, message) {
  const visible = await page.locator(selector).isVisible().catch(() => false)
  if (!visible) {
    throw new Error(message)
  }
}

async function assertText(page, text, message) {
  const bodyText = await page.locator('body').innerText().catch(() => '')
  if (!bodyText.includes(text)) {
    throw new Error(message)
  }
}

async function assertNoText(page, text, message) {
  const bodyText = await page.locator('body').innerText().catch(() => '')
  if (bodyText.includes(text)) {
    throw new Error(message)
  }
}

async function assertNoErrorFallback(page) {
  const bodyText = await page.locator('body').innerText().catch(() => '')
  if (bodyText.includes('页面渲染失败')) {
    throw new Error(`检测到错误边界: ${bodyText.slice(0, 200)}`)
  }
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function buildMockInterviewLaunchpadPayload() {
  const trackSeeds = [
    { id: 'mock-database-l3', domain: 'database', domainLabel: '数据库', difficulty: 'L3', questionType: 'scenario_analysis', summary: '如何定位 MySQL 慢查询' },
    { id: 'mock-network-l3', domain: 'network', domainLabel: '网络', difficulty: 'L3', questionType: 'scenario_analysis', summary: '如何排查跨服务调用超时' },
    { id: 'mock-os-l3', domain: 'os', domainLabel: '操作系统', difficulty: 'L3', questionType: 'principle', summary: 'load average 高但 CPU 不高怎么排查' },
    { id: 'mock-security-l4', domain: 'security', domainLabel: '安全', difficulty: 'L4', questionType: 'scenario_analysis', summary: '访问密钥泄露后如何遏制风险' },
    { id: 'mock-devops-l4', domain: 'devops', domainLabel: 'DevOps', difficulty: 'L4', questionType: 'scenario_analysis', summary: '发布失败后如何回滚并恢复流水线' },
  ]
  const openTracks = trackSeeds.map((track) => ({
    id: track.id,
    title: track.summary,
    domain: track.domain,
    domain_label: track.domainLabel,
    category: track.domain,
    difficulty: track.difficulty,
    question_type: track.questionType,
    question_role: 'opening',
    tags: [],
    summary: track.summary,
    details: [track.questionType === 'principle' ? '原理问答' : '情景分析'],
    published_count: 1,
    indexed_count: 1,
    availability_state: 'available',
    vector_status_summary: 'indexed',
  }))
  return {
    summary: {
      open_track_count: 5,
      published_atom_count: 5,
      indexed_atom_count: 5,
      fallback_mode: false,
      state: 'ready',
      message: 'mock launchpad',
    },
    domains: trackSeeds.map((track) => ({ value: track.domain, label: track.domainLabel, group: '面试题', note: '1 道题', open_track_count: 1 })),
    open_tracks: openTracks,
    recommended_tracks: [
      {
        ...openTracks[0],
        reason: '你最近最常练的是数据库。',
        source_kind: 'habitual_track',
      },
    ],
    recent_sessions: [],
    coverage: {
      domains: trackSeeds.map((track) => track.domain),
      difficulties: ['L3', 'L4'],
      question_types: ['scenario_analysis', 'principle'],
      question_roles: ['opening'],
      vector_status_summary: ['indexed'],
    },
    coverage_stats: {
      total_open_tracks: 5,
      practiced_open_tracks: 1,
      coverage_percent: 20,
      completed_sessions: 1,
      practiced_domains: ['database'],
      practiced_difficulties: ['L3'],
      subject_count: 1,
      top_subjects: ['慢查询定位'],
      uncovered_track_ids: openTracks.slice(1).map((track) => track.id),
    },
    fallback_mode: false,
  }
}

main().catch((error) => {
  console.error(error?.message || error)
  process.exit(1)
})
