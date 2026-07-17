import { expect, test, type Request } from '@playwright/test'

test('desktop: task center filters, shows events, retries and batch cancels', async ({ page }) => {
  const requests: Request[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    requests.push(request)
    let data: unknown = {}
    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/jobs') data = [
      { id: 1, kind: 'video.generate', status: 'running', progress: 45, provider: 'vidu', attempt: 1, max_attempts: 3, updated_at: '2026-07-17T08:00:00Z' },
      { id: 2, kind: 'image.generate', status: 'failed', progress: 10, provider: 'openai', attempt: 1, max_attempts: 3, last_error: 'provider timeout', updated_at: '2026-07-17T08:01:00Z' },
    ]
    else if (path === '/api/v1/jobs/1/events') data = [{ id: 1, stage: 'running', progress: 45, message: 'provider processing', created_at: '2026-07-17T08:00:00Z' }]
    else if (path === '/api/v1/jobs/2/retry') data = { id: 2, status: 'running' }
    else if (path === '/api/v1/jobs/batch-cancel') data = { canceled: [1], failures: {} }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data }) })
  })

  await page.goto('/jobs')
  await expect(page.getByRole('heading', { name: '任务中心' })).toBeVisible()
  await page.getByRole('button', { name: '查看日志' }).first().click()
  await expect(page.getByText('provider processing')).toBeVisible()
  await page.getByRole('button', { name: '重试' }).click()
  expect(requests.some(request => new URL(request.url()).pathname === '/api/v1/jobs/2/retry')).toBeTruthy()
  await page.getByLabel('选择任务 1').check()
  await page.getByRole('button', { name: '批量取消' }).click()
  const batch = requests.find(request => new URL(request.url()).pathname === '/api/v1/jobs/batch-cancel')
  expect(batch?.postDataJSON()).toEqual({ job_ids: [1] })
})
