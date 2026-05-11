import { expect, test, type Route } from '@playwright/test'
import { loginAs, expectNoWhiteScreen } from './helpers/auth'

test.describe('个人档案页面', () => {
  test('profile page matches the dark workspace visual language', async ({ page }) => {
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
          ai_structured_content: {},
          sensitive_check: { status: 'clear', sanitized: false, findings: [], checked_at: new Date().toISOString() },
          review_history: [],
          status: 'published',
          converted_question_id: 'profile-converted-scenario',
          created_at: new Date().toISOString(),
        }],
      })
    })

    await loginAs(page, 'student')
    await page.goto('/profile')

    await expect(page.locator('.profile-page')).toBeVisible()
    await expect(page.locator('.profile-hero')).toBeVisible()
    await expect(page.locator('.profile-highlight-card')).toHaveCount(4)
    await expect(page.locator('.profile-settings-panel')).toBeVisible()
    await expect(page.locator('.profile-summary-panel')).toBeVisible()
    await expect(page.locator('.profile-community-panel')).toBeVisible()
    await expect(page.getByText('当前关注域')).toBeVisible()
    await expect(page.getByText('个人档案投稿回显案例')).toBeVisible()
    await expect(page.getByText('已发布题库')).toBeVisible()

    const heroBox = await page.locator('.profile-hero').boundingBox()
    const settingsBox = await page.locator('.profile-settings-panel').boundingBox()
    const summaryBox = await page.locator('.profile-summary-panel').boundingBox()
    expect(heroBox).not.toBeNull()
    expect(settingsBox).not.toBeNull()
    expect(summaryBox).not.toBeNull()
    expect(settingsBox!.y).toBeGreaterThan(heroBox!.y + heroBox!.height - 2)
    expect(Math.abs(settingsBox!.y - summaryBox!.y)).toBeLessThanOrEqual(4)

    await expectNoWhiteScreen(page)
  })

  test('profile summary waits for save before updating target level and preferred domains', async ({ page }) => {
    await page.route('**/api/v1/users/me/history', async (route) => {
      await fulfill(route, {
        scenarios: [],
        interviews: [],
        community_posts: [],
      })
    })

    await page.route('**/api/v1/profile', async (route) => {
      if (route.request().method() !== 'PUT') {
        await route.fallback()
        return
      }
      await fulfill(route, {
        id: 'user-demo',
        username: 'demo',
        email: 'demo@example.com',
        role: 'student',
        created_at: new Date().toISOString(),
        profile: {
          target_level: 'architect',
          preferred_domains: ['security', 'dns'],
          capability_radar: {},
          weak_points: [],
          total_stats: {
            scenarios_solved: 0,
            interviews_taken: 0,
            average_score: 0,
            streak_days: 1,
          },
          updated_at: new Date().toISOString(),
        },
      })
    })

    await loginAs(page, 'student')
    await page.goto('/profile')

    const targetCard = page.locator('.profile-highlight-card').filter({ has: page.getByText('目标职级') })
    const domainCard = page.locator('.profile-highlight-card').filter({ has: page.getByText('偏好专业域') })
    const ribbon = page.locator('.profile-domain-ribbon')
    const summaryPanel = page.locator('.profile-summary-panel')

    const originalTargetCardText = (await targetCard.textContent()) ?? ''
    const originalDomainCardText = (await domainCard.textContent()) ?? ''
    const originalRibbonText = (await ribbon.textContent()) ?? ''
    const originalSummaryText = (await summaryPanel.textContent()) ?? ''

    const nextTargetValue = originalTargetCardText.includes('架构师') ? 'intermediate' : 'architect'
    const nextTargetLabel = nextTargetValue === 'architect' ? '架构师' : '中级'
    const useSecurityAndDns = !originalRibbonText.includes('安全') && !originalRibbonText.includes('dns')
    const nextDomainInput = useSecurityAndDns ? 'security,dns' : 'database,network,os'
    const nextDomainDetail = useSecurityAndDns ? '安全 / dns' : '数据库 / 网络 / 操作系统'
    const nextRibbonKeyword = useSecurityAndDns ? '安全' : '数据库'
    const nextSummaryCopy = useSecurityAndDns ? '优先围绕 安全、dns' : '优先围绕 数据库、网络、操作系统'

    await page.locator('.profile-settings-form select').selectOption(nextTargetValue)
    await page.locator('.profile-settings-form input').fill(nextDomainInput)

    await expect.poll(async () => (await targetCard.textContent()) ?? '').toBe(originalTargetCardText)
    await expect.poll(async () => (await domainCard.textContent()) ?? '').toBe(originalDomainCardText)
    await expect.poll(async () => (await ribbon.textContent()) ?? '').toBe(originalRibbonText)
    await expect.poll(async () => (await summaryPanel.textContent()) ?? '').toBe(originalSummaryText)

    await page.getByRole('button', { name: '保存设置' }).click()

    await expect(targetCard).toContainText(nextTargetLabel)
    await expect(domainCard).toContainText(nextDomainDetail)
    await expect(ribbon).toContainText(nextRibbonKeyword)
    await expect(summaryPanel).toContainText(nextSummaryCopy)
  })
})

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}
