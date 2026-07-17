import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/live',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env.LIVE_BASE_URL || 'http://127.0.0.1:8088',
    ...devices['Desktop Chrome'],
    channel: 'chrome',
    viewport: { width: 1440, height: 1000 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
})
