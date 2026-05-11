import { expect, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test.describe('学习仪表盘展示', () => {
  test('dashboard presents the competition-first hero workspace', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/dashboard')

    await expect(page.getByRole('heading', { name: '学习仪表盘' })).toBeVisible()
    await expect(page.getByText('汇总排查、面试和能力画像')).toBeVisible()
    await expect(page.locator('.sidebar-status-card')).toBeVisible()
    await expect(page.locator('.dashboard-hero')).toBeVisible()
    await expect(page.locator('.dashboard-hero-metrics .metric')).toHaveCount(4)
    await expect(page.getByText('今日学习闭环')).toBeVisible()
    await expect(page.getByText('今日推荐')).toBeVisible()
    await expect(page.getByText('能力雷达')).toBeVisible()
    await expect(page.getByText('三天复习计划')).toBeVisible()
    await expect(page.getByText('薄弱点')).toBeVisible()
  })
})
