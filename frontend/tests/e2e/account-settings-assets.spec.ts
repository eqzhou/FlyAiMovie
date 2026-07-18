import { expect, test, type Page, type Request } from '@playwright/test'

const actor = {
  user: { id: 1, email: 'owner@example.com', display_name: 'Owner' },
  organization: { id: 1, name: 'Demo Studio', slug: 'demo-studio' },
  role: 'owner',
  csrf_token: 'csrf-e2e',
}

async function mockAccountAPI(page: Page, options: { signedIn?: boolean } = {}) {
  let signedIn = options.signedIn ?? false
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let data: unknown = {}
    let status = 200

    if (path === '/api/v1/auth/status') data = { enabled: true, setup_required: false }
    else if (path === '/api/v1/auth/me' && !signedIn) {
      status = 401
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: 401, message: 'unauthorized' }) })
    } else if (path === '/api/v1/auth/me') data = actor
    else if (path === '/api/v1/auth/login') { signedIn = true; data = actor }
    else if (path === '/api/v1/auth/organizations') data = [{ ...actor.organization, role: 'owner', current: true }]
    else if (path === '/api/v1/auth/invitations/invite-token') data = { email: 'new@example.com', role: 'editor', organization: actor.organization, expires_at: '2099-01-01T00:00:00Z' }
    else if (path === '/api/v1/auth/invitations/invite-token/accept') data = actor
    else if (path === '/api/v1/ai-configs') data = [{ id: 10, name: 'Mock Image', service_type: 'image', provider: 'mock', model: 'mock' }]
    else if (path === '/api/v1/ai-providers') data = [{ provider: 'mock', service_type: 'image' }]
    else if (path === '/api/v1/agent-configs') data = [{ id: 20, agent_type: 'script_rewriter', name: 'Script Rewriter', model: 'mock', is_active: true }]
    else if (path === '/api/v1/organization/quota') data = { daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 2, active_jobs: 0, daily_budget_cny: 10, budget_warning_percent: 80, budget_used_cny: 1.25, budget_warning: false }
    else if (path === '/api/v1/organization/cache') data = { objects: 3, references: 4, bytes: 2048, orphaned: 1 }
    else if (path === '/api/v1/organization/cache/purge') data = { purged: { deleted_objects: 1 }, cleanup: { completed: 1, failed: 0 } }
    else if (path === '/api/v1/organization/members') data = [{ user_id: 1, email: actor.user.email, display_name: actor.user.display_name, role: 'owner' }]
    else if (path === '/api/v1/organization/members/invitations') data = []
    else if (path === '/api/v1/character-library') data = [{ id: 40, name: '跨项目主角', role: '主角', appearance: '短发，黑色外套', voice_style: '沉稳', image_url: '' }]
    else if (path === '/api/v1/dramas') data = { items: [{ id: 2, title: '素材项目', episodes: [{ id: 20, episode_number: 1, title: '第一集' }] }] }
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
  const settingsNavigation = page.getByRole('tablist', { name: '设置分类' })
  await expect(settingsNavigation).toBeVisible()
  await expect(settingsNavigation.getByRole('tab', { name: 'AI 服务' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('Mock Image')).toBeVisible()
  await expect(page.getByText('生成配额')).toHaveCount(0)

  await page.getByRole('button', { name: '添加 AI 服务' }).click()
  const serviceDialog = page.getByRole('dialog', { name: '添加 AI 服务' })
  await serviceDialog.getByLabel('类型').selectOption('video')
  await expect(serviceDialog.getByLabel('厂商')).toContainText('OpenAI Sora')
  await serviceDialog.getByLabel('类型').selectOption('text')
  await expect(serviceDialog.getByLabel('厂商')).toContainText('本地 OpenAI Compatible')
  await serviceDialog.getByRole('button', { name: '取消' }).click()

  await settingsNavigation.getByRole('tab', { name: 'Agent' }).click()
  await page.getByRole('button', { name: '编辑' }).first().click()
  await expect(page.getByRole('dialog', { name: '编辑 Agent' })).toBeVisible()
  await page.getByRole('dialog', { name: '编辑 Agent' }).getByRole('button', { name: '取消' }).click()

  await settingsNavigation.getByRole('tab', { name: '组织与权限' }).click()
  await expect(page.getByText('生成配额')).toBeVisible()
  await expect(page.getByText('本地缓存')).toBeVisible()
  await expect(page.getByText('容量 2.0 KB')).toBeVisible()

  await settingsNavigation.getByRole('tab', { name: '安全与数据' }).click()
  await page.getByRole('button', { name: '修改密码' }).click()
  await expect(page.getByRole('dialog', { name: '修改密码' })).toBeVisible()
  await page.getByRole('dialog', { name: '修改密码' }).getByRole('button', { name: '取消' }).click()

  await page.goto('/drama/2/assets')
  await expect(page.getByRole('heading', { name: '素材项目 · 素材库' })).toBeVisible()
  await expect(page.getByText('车站参考图')).toBeVisible()
})

test('desktop: project creation marks and validates required fields', async ({ page }) => {
  let created: Request | undefined
  await mockAccountAPI(page, { signedIn: true })
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/dramas' && request.method() === 'POST') created = request
  })
  await page.goto('/')
  await page.getByRole('button', { name: '新建项目' }).click()

  const dialog = page.getByRole('dialog', { name: '新建短剧项目' })
  await expect(dialog.getByText('* 为必填项')).toBeVisible()
  await expect(dialog.getByLabel(/标题.*\*/)).toBeVisible()
  await expect(dialog.getByLabel(/风格.*\*/)).toBeVisible()
  await expect(dialog.getByLabel(/集数.*\*/)).toBeVisible()

  await dialog.getByRole('button', { name: '创建', exact: true }).click()
  await expect(dialog.getByRole('alert')).toHaveText('请填写项目标题')
  await expect(dialog.getByLabel(/标题.*\*/)).toBeFocused()

  await dialog.getByLabel(/标题.*\*/).fill('必填校验项目')
  await dialog.getByLabel(/集数.*\*/).fill('0')
  await dialog.getByRole('button', { name: '创建', exact: true }).click()
  await expect(dialog.getByRole('alert')).toHaveText('集数必须是 1 到 50 之间的整数')

  await dialog.getByLabel(/集数.*\*/).fill('2')
  await dialog.getByRole('button', { name: '创建', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  expect(created?.postDataJSON()).toMatchObject({ title: '必填校验项目', style: 'realistic', total_episodes: 2 })
})

test('desktop: character library keeps forms in focused dialogs', async ({ page }) => {
  let created: Request | undefined
  let imported: Request | undefined
  await mockAccountAPI(page, { signedIn: true })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/character-library' && request.method() === 'POST') created = request
    if (path === '/api/v1/character-library/40/import' && request.method() === 'POST') imported = request
  })
  await page.goto('/character-library')

  await expect(page.getByRole('heading', { name: '角色库' })).toBeVisible()
  await expect(page.getByRole('row', { name: /跨项目主角/ })).toBeVisible()
  await expect(page.getByLabel('角色定位')).toHaveCount(0)

  await page.getByRole('button', { name: '新建角色模板' }).click()
  const createDialog = page.getByRole('dialog', { name: '新建角色模板' })
  await expect(createDialog.getByLabel(/名称.*\*/)).toBeFocused()
  await createDialog.getByLabel(/名称.*\*/).fill('新建模板')
  await createDialog.getByRole('button', { name: '创建模板' }).click()
  await expect(createDialog).toHaveCount(0)
  expect(created?.postDataJSON()).toMatchObject({ name: '新建模板' })

  const characterRow = page.getByRole('row', { name: /跨项目主角/ })
  await characterRow.getByRole('button', { name: '导入项目' }).click()
  const importDialog = page.getByRole('dialog', { name: '导入角色模板' })
  await expect(importDialog.getByText('跨项目主角')).toBeVisible()
  await expect(importDialog.getByLabel('项目')).toHaveValue('2')
  await expect(importDialog.getByLabel('剧集（可选）')).toContainText('第一集')
  await importDialog.getByLabel('剧集（可选）').selectOption('20')
  await importDialog.getByRole('button', { name: '确认导入' }).click()
  await expect(importDialog).toHaveCount(0)
  expect(imported?.postDataJSON()).toMatchObject({ drama_id: 2, episode_id: 20 })
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
