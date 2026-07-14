import { defineConfig, devices } from '@playwright/test'

const baseURL = 'http://localhost:5173'
const isCI = Boolean(process.env.CI)
const browserChannel = process.env.PLAYWRIGHT_BROWSER_CHANNEL ?? (process.platform === 'win32' ? 'chrome' : undefined)
const recordArtifacts = process.env.PLAYWRIGHT_RECORD_ARTIFACTS !== 'off'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: {
    timeout: 10_000,
  },
  retries: isCI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL,
    trace: recordArtifacts ? 'retain-on-failure' : 'off',
    screenshot: recordArtifacts ? 'only-on-failure' : 'off',
    video: recordArtifacts ? 'retain-on-failure' : 'off',
  },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1',
    url: baseURL,
    reuseExistingServer: !isCI,
    timeout: 120_000,
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        ...(browserChannel ? { channel: browserChannel } : {}),
      },
    },
  ],
})
