import { expect, test, type Page, type Request } from '@playwright/test'

const agentTypes = [
  'script_rewriter',
  'extractor',
  'storyboard_breaker',
  'voice_assigner',
  'grid_prompt_generator',
] as const

const actor = {
  user: { id: 1, email: 'owner@example.com', display_name: 'Owner' },
  organization: { id: 1, name: 'Demo Studio', slug: 'demo-studio' },
  role: 'owner',
  csrf_token: 'csrf-e2e',
}

async function mockSettingsAPI(page: Page, role: 'owner' | 'viewer' = 'owner', options: { authDisabled?: boolean } = {}) {
  let versions = [
    { id: 42, skill_id: 7, version: 2, main_markdown: '# Extractor v2', references_json: '{"references/rules.md":"Keep JSON stable"}', content_sha256: 'v2', created_by_user_id: 1, created_at: '2026-07-28T10:00:00Z' },
    { id: 41, skill_id: 7, version: 1, main_markdown: '# Extractor v1', references_json: '{}', content_sha256: 'v1', created_by_user_id: 1, created_at: '2026-07-27T10:00:00Z' },
  ]
  let publishedVersionID: number | undefined = 42
  let archivedAt: string | undefined

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let data: unknown = {}

    if (path === '/api/v1/auth/status') data = { enabled: !options.authDisabled, setup_required: false }
    else if (path === '/api/v1/auth/me') data = { ...actor, role }
    else if (path === '/api/v1/auth/organizations') data = [{ ...actor.organization, role, current: true }]
    else if (path === '/api/v1/ai-configs' || path === '/api/v1/ai-providers' || path === '/api/v1/agent-configs' || path === '/api/v1/prompt-templates' || path === '/api/v1/ai-voices' || path === '/api/v1/organization/members' || path === '/api/v1/organization/members/invitations') data = []
    else if (path === '/api/v1/organization/quota') data = { daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 0, active_jobs: 0, daily_budget_cny: 0, budget_warning_percent: 80, budget_used_cny: 0, budget_warning: false }
    else if (path === '/api/v1/organization/cache') data = { objects: 0, references: 0, bytes: 0, orphaned: 0 }
    else if (path === '/api/v1/ai-service-bundles' && request.method() === 'GET') data = [{
      id: 0,
      key: 'standard-cloud-studio',
      name: '标准云端工作室',
      description: '文本、图片、视频与音频的一站式组合',
      is_builtin: true,
      services: agentTypes.slice(0, 4).map((_, index) => ({
        service_type: ['text', 'image', 'video', 'audio'][index],
        provider: index === 3 ? 'minimax' : 'openai',
        name: `bundle-${['text', 'image', 'video', 'audio'][index]}`,
        base_url: index === 3 ? 'https://api.minimax.chat' : 'https://api.openai.com',
        model: ['gpt-4o-mini', 'gpt-image-1', 'sora-2', 'speech-02-turbo'][index],
        is_default: true,
        is_active: true,
      })),
    }, {
      id: 18,
      key: 'organization-studio',
      name: '组织工作室',
      description: '组织保存的服务组合',
      is_builtin: false,
      services: ['text', 'image', 'video', 'audio'].map((service_type, index) => ({
        service_type,
        provider: index === 3 ? 'minimax' : 'openai',
        name: `org-${service_type}`,
        base_url: index === 3 ? 'https://api.minimax.chat' : 'https://api.openai.com',
        model: `org-${service_type}-model`,
        is_default: true,
        is_active: true,
      })),
    }]
    else if (path === '/api/v1/ai-service-bundles/test') data = {
      results: ['text', 'image', 'video', 'audio'].map((service_type) => ({ service_type, status: 'ok', detail: `${service_type} reachable`, latency_ms: 3 })),
    }
    else if (path === '/api/v1/ai-service-bundles/preview') data = {
      items: [
        { service_type: 'text', action: 'reuse', config_id: 10, provider: 'openai', name: 'bundle-text', is_default: true },
        ...['image', 'video', 'audio'].map((service_type) => ({ service_type, action: 'create', provider: 'openai', name: `bundle-${service_type}`, is_default: true })),
      ],
      agents: agentTypes.map((agent_type, index) => ({
        agent_type,
        action: index === 0 ? 'reuse' : index === 1 ? 'update' : 'create',
        config_id: index < 2 ? 30 + index : undefined,
        model: 'gpt-4o-mini',
      })),
      conflicts: [{ service_type: 'text', kind: 'default_replaced', config_id: 2, message: 'the current default will be replaced' }],
      preview_token: 'preview-token-1',
    }
    else if (path === '/api/v1/ai-service-bundles/apply') data = { items: [], conflicts: [] }
    else if (path === '/api/v1/skills' && request.method() === 'GET') data = agentTypes.map((agent_type) => ({
      id: agent_type,
      agent_type,
      source: agent_type === 'extractor' ? 'database' : 'builtin',
      ...(agent_type === 'extractor' ? { registry: { id: 7, agent_type, published_version_id: publishedVersionID, archived_at: archivedAt } } : {}),
    }))
    else if (path === '/api/v1/skills/extractor' && request.method() === 'GET') data = {
      id: 7,
      agent_type: 'extractor',
      source: 'database',
      published_version_id: publishedVersionID,
      archived_at: archivedAt,
      versions,
      publications: [],
      published_version: versions.find((version) => version.id === publishedVersionID),
    }
    else if (/\/api\/v1\/skills\/(script_rewriter|storyboard_breaker|voice_assigner|grid_prompt_generator)$/.test(path)) data = {
      id: path.split('/').pop(), agent_type: path.split('/').pop(), source: 'builtin', content: '# Built-in skill', versions: [], publications: [],
    }
    else if (path === '/api/v1/skills/extractor/versions' && request.method() === 'POST') {
      const payload = request.postDataJSON() as { main_markdown: string; references: Record<string, string> }
      const created = { id: 43, skill_id: 7, version: 3, main_markdown: payload.main_markdown, references_json: JSON.stringify(payload.references), content_sha256: 'v3', created_by_user_id: 1, created_at: '2026-07-28T12:00:00Z' }
      versions = [created, ...versions]
      data = created
    }
    else if (/\/api\/v1\/skills\/extractor\/versions\/(41|42|43)\/publish$/.test(path)) { publishedVersionID = Number(path.split('/').at(-2)); archivedAt = undefined; data = { id: 7, agent_type: 'extractor', published_version_id: publishedVersionID } }
    else if (path === '/api/v1/skills/extractor/versions/41/rollback') { publishedVersionID = 41; archivedAt = undefined; data = { id: 7, agent_type: 'extractor', published_version_id: 41 } }
    else if (path === '/api/v1/skills/extractor/archive') { publishedVersionID = undefined; archivedAt = '2026-07-28T13:00:00Z'; data = { id: 7, agent_type: 'extractor', archived_at: archivedAt } }

    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data, message: 'success' }) })
  })
}

test('desktop: admin previews and applies a four-service bundle without retaining credentials', async ({ page }) => {
  let previewRequest: Request | undefined
  let testRequest: Request | undefined
  let applyRequest: Request | undefined
  await mockSettingsAPI(page)
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/ai-service-bundles/preview') previewRequest = request
    if (path === '/api/v1/ai-service-bundles/test') testRequest = request
    if (path === '/api/v1/ai-service-bundles/apply') applyRequest = request
  })

  await page.goto('/settings')
  await page.getByRole('tab', { name: '服务组合' }).click()
  await page.getByRole('button', { name: /标准云端工作室/ }).click()
  const dialog = page.getByRole('dialog', { name: '应用服务组合' })
  await dialog.getByLabel('文本 API Key').fill('text-secret')
  await dialog.getByLabel('图片 API Key').fill('image-secret')
  await dialog.getByLabel('视频 API Key').fill('video-secret')
  await dialog.getByLabel('音频 API Key').fill('audio-secret')
  await expect(dialog.getByLabel('同时同步 5 个 Agent 默认模型')).toBeChecked()
  await dialog.getByRole('button', { name: '测试全部' }).click()
  await expect(dialog.getByText(/测试通过 · text reachable/)).toBeVisible()
  expect(testRequest?.postDataJSON()).toEqual({
    bundle_key: 'standard-cloud-studio',
    apply_agent_defaults: true,
    credentials: { text: 'text-secret', image: 'image-secret', video: 'video-secret', audio: 'audio-secret' },
  })
  await dialog.getByRole('button', { name: '预览变更' }).click()

  await expect(dialog.getByText('复用现有配置')).toBeVisible()
  await expect(dialog.getByText('同步 Agent 默认模型')).toBeVisible()
  await expect(dialog.getByText('更新现有 Agent')).toBeVisible()
  await expect(dialog.getByText('将替换当前默认文本服务')).toBeVisible()
  expect(previewRequest?.postDataJSON()).toEqual({
    bundle_key: 'standard-cloud-studio',
    apply_agent_defaults: true,
    credentials: { text: 'text-secret', image: 'image-secret', video: 'video-secret', audio: 'audio-secret' },
  })

  await dialog.getByRole('button', { name: '确认应用' }).click()
  const confirm = page.getByRole('alertdialog', { name: '应用服务组合' })
  await expect(confirm).toContainText('4 个服务配置和 5 个 Agent 默认模型')
  await confirm.getByRole('button', { name: '应用组合' }).click()
  await expect(page.getByRole('status').filter({ hasText: '服务组合已应用' })).toBeVisible()
  expect(applyRequest?.postDataJSON()).toEqual({
    bundle_key: 'standard-cloud-studio',
    apply_agent_defaults: true,
    credentials: { text: 'text-secret', image: 'image-secret', video: 'video-secret', audio: 'audio-secret' },
    preview_token: 'preview-token-1',
  })
  await page.getByRole('button', { name: /标准云端工作室/ }).click()
  const reopened = page.getByRole('dialog', { name: '应用服务组合' })
  await expect(reopened.getByLabel('文本 API Key')).toHaveValue('')
  await expect(reopened.getByLabel('音频 API Key')).toHaveValue('')
})

test('desktop: admin manages skill versions and viewer remains read-only', async ({ page, browser }) => {
  let createdVersion: Request | undefined
  await mockSettingsAPI(page)
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/skills/extractor/versions' && request.method() === 'POST') createdVersion = request
  })
  await page.goto('/settings')
  await page.getByRole('tab', { name: 'Skills' }).click()
  const registry = page.getByRole('tabpanel', { name: 'Skills' })
  for (const label of ['剧本改写', '角色场景提取', '分镜拆解', '音色分配', '宫格提示词']) {
    await expect(registry.getByRole('button', { name: new RegExp(label) })).toBeVisible()
  }
  await registry.getByRole('button', { name: /角色场景提取/ }).click()
  await expect(registry.getByText('v2 · 已发布')).toBeVisible()
  await registry.getByLabel('主文档').fill('# Extractor v3')
  await registry.getByLabel('References JSON').fill('{"references/schema.md":"Return strict JSON"}')
  await registry.getByRole('button', { name: '创建版本' }).click()
  await expect(registry.getByText('v3', { exact: true })).toBeVisible()
  expect(createdVersion?.postDataJSON()).toEqual({ main_markdown: '# Extractor v3', references: { 'references/schema.md': 'Return strict JSON' } })

  await registry.getByRole('button', { name: '发布 v3' }).click()
  await page.getByRole('alertdialog', { name: '发布 Skill 版本' }).getByRole('button', { name: '发布版本' }).click()
  await expect(registry.getByText('v3 · 已发布')).toBeVisible()
  await registry.getByRole('button', { name: '回滚到 v1' }).click()
  await page.getByRole('alertdialog', { name: '回滚 Skill' }).getByRole('button', { name: '确认回滚' }).click()
  await expect(registry.getByText('v1 · 已发布')).toBeVisible()
  await registry.getByRole('button', { name: '归档 Skill' }).click()
  await page.getByRole('alertdialog', { name: '归档 Skill' }).getByRole('button', { name: '确认归档' }).click()
  await expect(registry.locator('.job-status').filter({ hasText: '已归档' })).toBeVisible()
  await registry.getByRole('button', { name: '恢复 v1' }).click()
  await page.getByRole('alertdialog', { name: '恢复 Skill' }).getByRole('button', { name: '恢复版本' }).click()
  await expect(registry.getByText('v1 · 已发布')).toBeVisible()

  const viewerContext = await browser.newContext()
  const viewerPage = await viewerContext.newPage()
  await mockSettingsAPI(viewerPage, 'viewer')
  await viewerPage.goto('/settings')
  await viewerPage.getByRole('tab', { name: 'Skills' }).click()
  const viewerRegistry = viewerPage.getByRole('tabpanel', { name: 'Skills' })
  await viewerRegistry.getByRole('button', { name: /角色场景提取/ }).click()
  await expect(viewerRegistry.getByText('# Extractor v2').first()).toBeVisible()
  await expect(viewerRegistry.getByRole('button', { name: /创建版本|发布 v|回滚到|归档 Skill/ })).toHaveCount(0)
  await expect(viewerRegistry.getByLabel('主文档')).toHaveCount(0)
  await viewerContext.close()
})

test('desktop: database service bundle requests use bundle_id', async ({ page }) => {
  let previewRequest: Request | undefined
  await mockSettingsAPI(page)
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/ai-service-bundles/preview') previewRequest = request
  })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '服务组合' }).click()
  await page.getByRole('button', { name: /组织工作室/ }).click()
  const dialog = page.getByRole('dialog', { name: '应用服务组合' })
  await dialog.getByRole('button', { name: '预览变更' }).click()
  await expect(dialog.getByText('变更预览')).toBeVisible()
  expect(previewRequest?.postDataJSON()).toEqual({ bundle_id: 18, apply_agent_defaults: true, credentials: { text: '', image: '', video: '', audio: '' } })
})

test('desktop: auth-disabled mode manages Skills in the local workspace', async ({ page }) => {
  await mockSettingsAPI(page, 'owner', { authDisabled: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: 'Skills' }).click()
  const registry = page.getByRole('tabpanel', { name: 'Skills' })
  await expect(registry.getByText('本地工作区 Skills')).toBeVisible()
  await registry.getByRole('button', { name: /角色场景提取/ }).click()
  await registry.getByLabel('主文档').fill('# Local Extractor v3')
  await registry.getByRole('button', { name: '创建版本' }).click()
  await expect(registry.getByText('v3', { exact: true })).toBeVisible()
})

test('mobile: service bundles and Skill editor stay within the viewport', async ({ page }) => {
  await mockSettingsAPI(page)
  await page.goto('/settings')
  await page.getByRole('tab', { name: '服务组合' }).click()
  await page.getByRole('button', { name: /标准云端工作室/ }).click()
  const bundleDialog = page.getByRole('dialog', { name: '应用服务组合' })
  const dialogBox = await bundleDialog.boundingBox()
  expect(dialogBox).not.toBeNull()
  expect(dialogBox!.width).toBeLessThanOrEqual(390)
  expect(await bundleDialog.locator('.bundle-service-row').first().evaluate((element) => getComputedStyle(element).gridTemplateColumns.trim().split(/\s+/).length)).toBe(1)
  await bundleDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('tab', { name: 'Skills' }).click()
  const registry = page.getByRole('tabpanel', { name: 'Skills' })
  await registry.getByRole('button', { name: /角色场景提取/ }).click()
  const layout = registry.locator('.skill-registry-layout')
  const layoutBox = await layout.boundingBox()
  expect(layoutBox).not.toBeNull()
  expect(layoutBox!.width).toBeLessThanOrEqual(390)
  expect(await layout.evaluate((element) => getComputedStyle(element).gridTemplateColumns.trim().split(/\s+/).length)).toBe(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})
