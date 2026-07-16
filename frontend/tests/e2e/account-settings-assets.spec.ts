import { expect, test, type Page, type Request } from '@playwright/test'

const actor = {
  user: { id: 1, email: 'owner@example.com', display_name: 'Owner' },
  organization: { id: 1, name: 'Demo Studio', slug: 'demo-studio' },
  role: 'owner',
  csrf_token: 'csrf-e2e',
}

async function mockAccountAPI(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let data: unknown = {}
    let status = 200

    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/auth/me') {
      status = 401
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: 401, message: 'unauthorized' }) })
    } else if (path === '/api/v1/auth/login') data = actor
    else if (path === '/api/v1/auth/organizations') data = [{ ...actor.organization, role: 'owner', current: true }]
    else if (path === '/api/v1/auth/invitations/invite-token') data = { email: 'new@example.com', role: 'editor', organization: actor.organization, expires_at: '2099-01-01T00:00:00Z' }
    else if (path === '/api/v1/auth/invitations/invite-token/accept') data = actor
    else if (path === '/api/v1/ai-configs') data = [{ id: 10, name: 'Mock Image', service_type: 'image', provider: 'mock', model: 'mock' }]
    else if (path === '/api/v1/ai-providers') data = [{ provider: 'mock', service_type: 'image' }]
    else if (path === '/api/v1/agent-configs') data = [{ id: 20, agent_type: 'script_rewriter', name: 'Script Rewriter', model: 'mock', is_active: true }]
    else if (path === '/api/v1/organization/quota') data = { daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 2, active_jobs: 0 }
    else if (path === '/api/v1/organization/members') data = [{ user_id: 1, email: actor.user.email, display_name: actor.user.display_name, role: 'owner' }]
    else if (path === '/api/v1/organization/members/invitations') data = []
    else if (path === '/api/v1/dramas/2') data = { id: 2, title: '素材项目', episodes: [{ id: 20, episode_number: 1, title: '第一集' }], characters: [], scenes: [], props: [] }
    else if (path === '/api/v1/assets') data = [{ id: 30, name: '车站参考图', type: 'image', category: 'reference', url: '/media/station.png', is_favorite: false }]

    return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, data, message: status === 200 ? 'success' : 'error' }) })
  })
}

test('desktop: login, settings and asset library workflows are reachable', async ({ page }) => {
  await mockAccountAPI(page)
  await page.goto('/login')
  await page.getByLabel('邮箱').fill('owner@example.com')
  await page.getByLabel('密码').fill('a-secure-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/$/)

  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
  await expect(page.getByText('Mock Image')).toBeVisible()
  await expect(page.getByText('生成配额')).toBeVisible()

  await page.goto('/drama/2/assets')
  await expect(page.getByRole('heading', { name: '素材项目 · 素材库' })).toBeVisible()
  await expect(page.getByText('车站参考图')).toBeVisible()
})

test('desktop: invitation preview and acceptance submit the expected payload', async ({ page }) => {
  let accepted: Request | undefined
  await mockAccountAPI(page)
  await page.on('request', (request) => {
    if (request.url().endsWith('/auth/invitations/invite-token/accept')) accepted = request
  })
  await page.goto('/invite/invite-token')
  await expect(page.getByRole('heading', { name: '接受组织邀请' })).toBeVisible()
  await expect(page.getByText('加入 Demo Studio，角色：editor')).toBeVisible()
  await page.getByLabel('已有账号当前密码（已有账号必填）').fill('current-password')
  await page.getByRole('button', { name: '接受邀请' }).click()
  await expect(page).toHaveURL(/\/$/)
  expect(accepted?.postDataJSON()).toMatchObject({ email: 'new@example.com', current_password: 'current-password' })
})

test('mobile: login and asset library fit a 390px viewport', async ({ page }) => {
  await mockAccountAPI(page)
  await page.goto('/login')
  await page.getByLabel('邮箱').fill('owner@example.com')
  await page.getByLabel('密码').fill('a-secure-password')
  await page.getByRole('button', { name: '登录' }).click()
  await page.goto('/drama/2/assets')
  await expect(page.getByText('车站参考图')).toBeVisible()
  const layout = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }))
  expect(layout.scrollWidth).toBeLessThanOrEqual(layout.width)
})
