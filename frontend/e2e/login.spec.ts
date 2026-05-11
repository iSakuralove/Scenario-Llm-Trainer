import { expect, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs, type DemoRole } from './helpers/auth'

const roles: DemoRole[] = ['student', 'instructor', 'admin']

test.describe('演示账号登录', () => {
  test('login page presents the AI teaching workbench poster', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => window.localStorage.clear())
    await page.reload()

    await expect(page.locator('.auth-layout')).toBeVisible()
    await expect(page.getByText('基于Agent的')).toBeVisible()
    await expect(page.getByText('IT技能排障与面')).toBeVisible()
    await expect(page.getByText('试情景式训练')).toBeVisible()
    await expect(page.getByText('AGENT-DRIVEN')).toBeVisible()
    await expect(page.getByText('TRAINING SYSTEM')).toBeVisible()
    await expect(page.getByText('技术面试追问')).toBeVisible()
    await expect(page.getByText('UGC 转题库')).toBeVisible()
    await expect(page.getByText('AI Router')).toBeVisible()
    await expect(page.locator('.auth-collage-preview')).toBeVisible()
    await expect(page.locator('.auth-flow-card')).toHaveCount(3)
    await expect(page.locator('.auth-panel-demo')).toBeVisible()
  })

  test('login page keeps the form reachable on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await page.goto('/')
    await page.evaluate(() => window.localStorage.clear())
    await page.reload()

    await expect(page.locator('.auth-panel')).toBeVisible()
    await expect(page.locator('.auth-collage-preview')).toBeVisible()

    const layout = await page.evaluate(() => {
      const root = document.documentElement
      const panel = document.querySelector('.auth-panel')?.getBoundingClientRect()
      const preview = document.querySelector('.auth-workbench-preview')?.getBoundingClientRect()
      const submit = document.querySelector('.auth-panel button[type="submit"]')?.getBoundingClientRect()

      return {
        hasHorizontalOverflow: root.scrollWidth > root.clientWidth + 1,
        panelTop: panel?.top ?? Number.POSITIVE_INFINITY,
        previewTop: preview?.top ?? Number.NEGATIVE_INFINITY,
        submitTop: submit?.top ?? Number.POSITIVE_INFINITY,
        viewportHeight: root.clientHeight,
      }
    })

    expect(layout.hasHorizontalOverflow).toBe(false)
    expect(layout.panelTop).toBeLessThan(layout.previewTop)
    expect(layout.submitTop).toBeLessThan(layout.viewportHeight)
  })

  test('login workbench preview keeps all left labels readable on compact desktop', async ({ page }) => {
    await page.setViewportSize({ width: 1267, height: 715 })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await page.evaluate(() => {
      window.localStorage.clear()
      window.sessionStorage.clear()
    })
    await page.reload({ waitUntil: 'domcontentloaded' })

    await expect(page.locator('.auth-workbench-preview')).toBeVisible()
    await expect(page.locator('.auth-ribbon')).toHaveCount(3)

    const previewLayout = await page.evaluate(() => {
      const preview = document.querySelector('.auth-workbench-preview')?.getBoundingClientRect()
      const surface = document.querySelector('.auth-collage-surface')?.getBoundingClientRect()
      const ribbons = Array.from(document.querySelectorAll('.auth-ribbon')).map((ribbon) => {
        const box = ribbon.getBoundingClientRect()
        return {
          left: box.left,
          right: box.right,
          top: box.top,
          bottom: box.bottom,
          text: ribbon.textContent?.trim() ?? '',
        }
      })

      return {
        preview,
        surface,
        ribbons,
        viewportWidth: document.documentElement.clientWidth,
      }
    })

    expect(previewLayout.preview).not.toBeUndefined()
    expect(previewLayout.surface).not.toBeUndefined()
    for (const ribbon of previewLayout.ribbons) {
      expect(ribbon.left).toBeGreaterThanOrEqual(previewLayout.preview!.left)
      expect(ribbon.right).toBeLessThan(previewLayout.surface!.left - 8)
      expect(ribbon.top).toBeGreaterThanOrEqual(previewLayout.preview!.top)
      expect(ribbon.bottom).toBeLessThanOrEqual(previewLayout.preview!.bottom)
      expect(ribbon.text.length).toBeGreaterThan(0)
    }
  })

  for (const role of roles) {
    test(`${role} can enter the app`, async ({ page }) => {
      await loginAs(page, role)
      await expectNoWhiteScreen(page)
      await expect(page.getByRole('link', { name: /排查工坊/ })).toBeVisible()
      await expect(page.getByRole('link', { name: /面试舱/ })).toBeVisible()
      await expect(page.getByRole('link', { name: /案例工坊/ })).toBeVisible()

      if (role === 'admin') {
        await expect(page.getByRole('link', { name: /系统状态/ })).toBeVisible()
      } else {
        await expect(page.getByRole('link', { name: /系统状态/ })).toHaveCount(0)
      }
    })
  }

  test('student login after admin logout does not stay on system page', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/system')
    await expect(page.getByRole('heading', { name: '系统状态' })).toBeVisible()

    await page.getByTitle('退出登录').click()
    await expect(page.locator('.auth-layout')).toBeVisible()

    await page.getByLabel('用户名或邮箱').fill('demo')
    await page.getByLabel('密码').fill('demo123')
    await page.getByRole('button', { name: '进入系统' }).click()

    await expectNoWhiteScreen(page)
    await expect(page).toHaveURL(/\/dashboard$/)
    await expect(page.locator('body')).not.toContainText('admin role required')
    await expect(page.getByRole('link', { name: /系统状态/ })).toHaveCount(0)
  })

  test('student is redirected away from direct system URL', async ({ page }) => {
    await loginAs(page, 'student')
    await page.goto('/system')

    await expectNoWhiteScreen(page)
    await expect(page).toHaveURL(/\/dashboard$/)
    await expect(page.locator('body')).not.toContainText('admin role required')
    await expect(page.getByRole('heading', { name: '系统状态' })).toHaveCount(0)
  })
})
