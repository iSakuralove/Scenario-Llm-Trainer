import { expect, type Page } from '@playwright/test'

export type DemoRole = 'student' | 'instructor' | 'admin'

const accounts: Record<DemoRole, { username: string; password: string }> = {
  student: { username: 'demo', password: 'demo123' },
  instructor: { username: 'instructor', password: 'instructor123' },
  admin: { username: 'admin', password: 'admin123' },
}

export async function loginAs(page: Page, role: DemoRole) {
  const account = accounts[role]
  await page.goto('/')
  await page.evaluate(() => window.localStorage.clear())
  await page.reload()
  await page.getByLabel('用户名或邮箱').fill(account.username)
  await page.getByLabel('密码').fill(account.password)
  await page.getByRole('button', { name: '进入系统' }).click()
  await expect(page.getByRole('link', { name: /仪表盘/ })).toBeVisible()
}

export async function resetSession(page: Page) {
  await page.goto('/')
  await page.evaluate(() => window.localStorage.clear())
}

export async function expectNoWhiteScreen(page: Page) {
  await expect(page.locator('body')).not.toContainText('页面渲染失败')
  await expect(page.locator('body')).not.toHaveText(/^\s*$/)
  await expect(page.locator('.app-shell, .auth-layout').first()).toBeVisible()
}
