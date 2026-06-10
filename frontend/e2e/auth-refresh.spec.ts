import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('dashboard request refreshes expired access token and retries once', async ({ page }) => {
  let dashboardAttempts = 0
  let refreshAttempts = 0

  await page.route('**/api/v1/users/me/dashboard', async (route) => {
    dashboardAttempts += 1
    if (dashboardAttempts === 1) {
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ code: 401, message: 'token expired' }),
      })
      return
    }
    await fulfill(route, dashboardPayload())
  })

  await page.route('**/api/v1/auth/refresh', async (route) => {
    refreshAttempts += 1
    await fulfill(route, {
      user: demoUser(),
      access_token: 'e2e-refreshed-access-token',
      refresh_token: 'e2e-refreshed-refresh-token',
    })
  })

  await loginAs(page, 'student')

  await expect.poll(() => dashboardAttempts).toBeGreaterThanOrEqual(2)
  await expect.poll(() => refreshAttempts).toBe(1)
  await expect(page.getByRole('heading', { name: '学习仪表盘' })).toBeVisible()
  await expect(page.getByText('继续数据库排障训练并完成今日复盘。').first()).toBeVisible()

  const storedTokens = await page.evaluate(() => ({
    access: window.localStorage.getItem('teaching_mvp_access'),
    refresh: window.localStorage.getItem('teaching_mvp_refresh'),
  }))
  expect(storedTokens.access).toBe('e2e-refreshed-access-token')
  expect(storedTokens.refresh).toBe('e2e-refreshed-refresh-token')
})

test('refresh failure sends the user back to the login page', async ({ page }) => {
  let historyAttempts = 0

  await loginAs(page, 'student')

  await page.route('**/api/v1/users/me/history', async (route) => {
    historyAttempts += 1
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ code: 401, message: 'token expired' }),
    })
  })

  await page.route('**/api/v1/auth/refresh', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ code: 401, message: 'invalid refresh token' }),
    })
  })

  await page.goto('/profile')

  await expect.poll(() => historyAttempts).toBeGreaterThan(0)
  await expect(page.locator('.auth-layout')).toBeVisible()
  await expect(page.getByRole('button', { name: '进入系统' })).toBeVisible()
})

function demoUser() {
  return {
    id: 'user-demo',
    username: 'demo',
    email: 'demo@example.com',
    role: 'student',
    created_at: new Date('2026-06-10T00:00:00Z').toISOString(),
    profile: {
      target_level: 'intermediate',
      preferred_domains: ['database'],
      capability_radar: {
        database: 82,
      },
      weak_points: [],
      total_stats: {
        scenarios_solved: 3,
        interviews_taken: 1,
        average_score: 88,
        streak_days: 2,
      },
      checkin_dates: [],
      updated_at: new Date('2026-06-10T00:00:00Z').toISOString(),
    },
  }
}

function dashboardPayload() {
  return {
    user: demoUser(),
    stats: demoUser().profile.total_stats,
    capability_radar: {
      database: 82,
    },
    weak_points: [],
    recommendations: [],
    learning_plan: {
      generated_at: new Date('2026-06-10T00:00:00Z').toISOString(),
      summary: '继续数据库排障训练并完成今日复盘。',
      target_level: 'intermediate',
      focus_domains: ['database'],
      domain_insights: [],
      recommendations: [],
      review_plan: [],
    },
    review_calendar: {
      generated_at: new Date('2026-06-10T00:00:00Z').toISOString(),
      checkin_dates: [],
      streak_days: 2,
      today_checked: false,
      today: '2026-06-10',
      review_plan: [],
      focus_domains: ['database'],
      next_action: '继续数据库排障训练',
    },
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}
