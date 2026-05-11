import { expect, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('community queue filters keep workshop styling for inactive items', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1024 })

  await loginAs(page, 'admin')
  await page.goto('/community')

  const filterBar = page.locator('.community-feed-toolbar .compact-segmented')
  await expect(filterBar).toBeVisible()

  const inactiveButton = filterBar.getByRole('button', { name: '待初审' })
  await expect(inactiveButton).toBeVisible()
  await expect(inactiveButton).not.toHaveClass(/active/)
  await expect(inactiveButton).toHaveCSS('background-color', 'rgba(19, 35, 48, 0.88)')
  await expect(inactiveButton).toHaveCSS('border-top-color', 'rgba(142, 240, 219, 0.12)')
  await expect(inactiveButton).toHaveCSS('color', 'rgb(214, 224, 233)')
})
