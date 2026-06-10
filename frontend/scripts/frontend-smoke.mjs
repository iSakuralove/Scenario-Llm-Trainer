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
    await page.locator('button[type="submit"]').click()
    await page.waitForURL(/\/dashboard$/, { timeout: 30_000 })
    await page.waitForSelector('.app-shell', { state: 'visible', timeout: 30_000 })
    await assertVisible(page, '.app-shell', '登录后主工作区未渲染')
    await page.waitForFunction(() => document.body.innerText.includes('学习仪表盘'), null, { timeout: 30_000 })
    await assertText(page, '学习仪表盘', '仪表盘标题未渲染')
    await page.goto(`${baseURL}/profile`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('个人档案'), null, { timeout: 30_000 })
    await assertText(page, '个人档案', '个人档案页未渲染')
    await assertNoErrorFallback(page)
    await page.goto(`${baseURL}/scenarios`, { waitUntil: 'domcontentloaded', timeout: 30_000 })
    await page.waitForFunction(() => document.body.innerText.includes('排查工坊'), null, { timeout: 30_000 })
    await assertText(page, '排查工坊', '排查工坊页未渲染')
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

async function assertNoErrorFallback(page) {
  const bodyText = await page.locator('body').innerText().catch(() => '')
  if (bodyText.includes('页面渲染失败')) {
    throw new Error(`检测到错误边界: ${bodyText.slice(0, 200)}`)
  }
}

main().catch((error) => {
  console.error(error?.message || error)
  process.exit(1)
})
