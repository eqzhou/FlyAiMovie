import { expect, test, type Page, type Request } from '@playwright/test'

const actor = {
  user: { id: 1, email: 'owner@example.com', display_name: 'Owner' },
  organization: { id: 1, name: 'Demo Studio', slug: 'demo-studio' },
  role: 'owner',
  csrf_token: 'csrf-e2e',
}

async function mockAccountAPI(page: Page, options: {
  signedIn?: boolean
  role?: 'owner' | 'admin' | 'editor' | 'viewer'
  isPlatformAdmin?: boolean
  platformSettings?: { registration_enabled: boolean; require_email_verification: boolean }
  failCharacterLibraryRequests?: number
  failCharacterLibraryCreates?: number
  failProviderRequests?: number
  delayPromptHistory?: boolean
  delayPromptDraft?: boolean
} = {}) {
  let signedIn = options.signedIn ?? false
  let remainingCharacterLibraryFailures = options.failCharacterLibraryRequests ?? 0
  let remainingCharacterCreateFailures = options.failCharacterLibraryCreates ?? 0
  let remainingProviderFailures = options.failProviderRequests ?? 0
  const platformSettings = {
    registration_enabled: options.platformSettings?.registration_enabled ?? true,
    require_email_verification: options.platformSettings?.require_email_verification ?? false,
  }
  const currentActor = {
    ...actor,
    role: options.role || actor.role,
    user: {
      ...actor.user,
      is_platform_admin: options.isPlatformAdmin ?? false,
    },
  }
  await page.route('**/static/audio/voice-preview.mp3', (route) => route.fulfill({ status: 200, contentType: 'audio/mpeg', body: '' }))
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let data: unknown = {}
    let status = 200

    if (path === '/api/v1/auth/status') data = { enabled: true, setup_required: false, registration_enabled: platformSettings.registration_enabled, require_email_verification: platformSettings.require_email_verification }
    else if (path === '/api/v1/auth/platform-settings' && request.method() === 'GET') data = { ...platformSettings }
    else if (path === '/api/v1/auth/platform-settings' && request.method() === 'PUT') {
      const payload = request.postDataJSON() as { registration_enabled?: boolean; require_email_verification?: boolean }
      if (typeof payload.registration_enabled !== 'boolean' || typeof payload.require_email_verification !== 'boolean') {
        status = 400
        return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, message: 'invalid body' }) })
      }
      platformSettings.registration_enabled = payload.registration_enabled
      platformSettings.require_email_verification = payload.require_email_verification
      data = { ...platformSettings }
    }
    else if (path === '/api/v1/auth/me' && !signedIn) {
      status = 401
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: 401, message: 'unauthorized' }) })
    } else if (path === '/api/v1/auth/me') data = currentActor
    else if (path === '/api/v1/auth/login') { signedIn = true; data = currentActor }
    else if (path === '/api/v1/auth/organizations') data = [{ ...actor.organization, role: 'owner', current: true }]
    else if (path === '/api/v1/auth/invitations/invite-token') data = { email: 'new@example.com', role: 'editor', organization: actor.organization, expires_at: '2099-01-01T00:00:00Z' }
    else if (path === '/api/v1/auth/invitations/invite-token/accept') data = actor
    else if (path === '/api/v1/ai-configs') data = [
      { id: 10, name: 'Mock Image', service_type: 'image', provider: 'mock', model: 'mock', base_url: 'http://localhost', api_key_set: true, is_active: true, is_default: true },
      { id: 11, name: 'Mock Video', service_type: 'video', provider: 'mock', model: 'mock', base_url: 'http://localhost', api_key_set: true, is_active: true },
      { id: 12, name: 'Mock Audio', service_type: 'audio', provider: 'mock', model: 'mock', base_url: 'http://localhost', api_key_set: true, is_active: true },
    ]
    else if (path === '/api/v1/ai-configs/10/test') data = { status: 'ok', provider: 'mock', model: 'mock', latency_ms: 1, detail: 'Mock 服务可用' }
    else if (path === '/api/v1/ai-configs/test') data = { status: 'ok', provider: 'mock', model: 'mock-draft', latency_ms: 2, detail: '当前配置可用' }
    else if (path === '/api/v1/ai-providers' && remainingProviderFailures > 0) {
      remainingProviderFailures -= 1
      status = 503
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, message: '厂商目录暂时不可用' }) })
    }
    else if (path === '/api/v1/ai-providers') data = [{ provider: 'mock', service_type: 'image' }]
    else if (path === '/api/v1/agent-configs') data = [{ id: 20, agent_type: 'script_rewriter', name: 'Script Rewriter', model: 'mock', is_active: true }]
    else if (path === '/api/v1/prompt-templates/preview') {
      const payload = request.postDataJSON() as { content?: string }
      if (payload.content?.includes('{{secret}}')) {
        status = 400
        return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, message: 'unknown prompt variable "secret"' }) })
      }
      if (options.delayPromptDraft && payload.content?.includes('旧内容')) {
        await new Promise(resolve => setTimeout(resolve, 350))
        data = { rendered: '草稿预览：旧内容', variables: [] }
      } else if (options.delayPromptDraft && payload.content?.includes('新内容')) {
        data = { rendered: '草稿预览：新内容', variables: [] }
      } else {
        data = { rendered: '草稿预览：示例短剧', variables: ['drama_title'] }
      }
    }
    else if (path === '/api/v1/prompt-templates') data = [
      { id: 80, key: 'script_rewriter', name: '剧本改写', category: 'agent_system', description: '将故事素材整理为短剧剧本', content: '为 {{drama_title}} 执行 {{user_instruction}}', variables_json: '["drama_title","user_instruction"]', version: 3, is_active: true },
      { id: 81, key: 'grid_prompt_generator', name: '宫格提示词', category: 'agent_system', description: '生成连续画面', content: '{{episode_title}}', variables_json: '["episode_title"]', version: 1, is_active: true },
    ]
    else if (path === '/api/v1/prompt-templates/80/preview') data = { rendered: '为 归途 执行 重写对白', version: 3 }
    else if (path === '/api/v1/prompt-templates/80/revisions') {
      if (options.delayPromptHistory) await new Promise(resolve => setTimeout(resolve, 350))
      data = [
      { id: 803, template_id: 80, version: 3, name: '剧本改写', category: 'agent_system', content: options.delayPromptHistory ? 'A-version' : '为 {{drama_title}} 执行 {{user_instruction}}', is_active: true, created_at: '2026-07-18T10:00:00Z' },
      { id: 802, template_id: 80, version: 2, name: '剧本改写', category: 'agent_system', content: '旧版 {{drama_title}}', is_active: true, created_at: '2026-07-17T10:00:00Z' },
      { id: 801, template_id: 80, version: 1, name: '剧本改写', category: 'agent_system', content: '初始 {{drama_title}}', is_active: true, created_at: '2026-07-16T10:00:00Z' },
      ]
    }
    else if (path === '/api/v1/prompt-templates/81/revisions') data = [
      { id: 811, template_id: 81, version: 1, name: '宫格提示词', category: 'agent_system', content: 'B-version', is_active: true, created_at: '2026-07-18T11:00:00Z' },
    ]
    else if (path === '/api/v1/ai-voices') data = [
      { voice_id: 'female-shaonv', voice_name: '少女', language: '中文', provider: 'mock', capabilities: 'mock', is_active: true },
      { voice_id: 'retired-voice', voice_name: '旧音色', language: '中文', provider: 'mock', capabilities: 'mock', is_active: false },
    ]
    else if (path === '/api/v1/ai-voices/sync') data = { count: 2, message: 'Mock voices seeded' }
    else if (path === '/api/v1/ai-voices/female-shaonv/preview') data = { voice_id: 'female-shaonv', provider: 'mock', audio_url: '/static/audio/voice-preview.mp3' }
    else if (path === '/api/v1/organization/quota') data = { daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 2, active_jobs: 0, daily_budget_cny: 10, budget_warning_percent: 80, budget_used_cny: 1.25, budget_warning: false }
    else if (path === '/api/v1/organization/cache') data = { objects: 3, references: 4, bytes: 2048, orphaned: 1 }
    else if (path === '/api/v1/organization/cache/purge') data = { purged: { deleted_objects: 1 }, cleanup: { completed: 1, failed: 0 } }
    else if (path === '/api/v1/organization/members') data = [{ user_id: 1, email: actor.user.email, display_name: actor.user.display_name, role: 'owner' }]
    else if (path === '/api/v1/organization/members/invitations') data = []
    else if (path === '/api/v1/character-library' && request.method() === 'GET' && remainingCharacterLibraryFailures > 0) {
      remainingCharacterLibraryFailures -= 1
      status = 503
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, message: '角色库暂时不可用' }) })
    }
    else if (path === '/api/v1/character-library' && request.method() === 'POST' && remainingCharacterCreateFailures > 0) {
      remainingCharacterCreateFailures -= 1
      status = 503
      return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, message: '模板保存失败' }) })
    }
    else if (path === '/api/v1/character-library') data = [{ id: 40, name: '跨项目主角', role: '主角', appearance: '短发，黑色外套', voice_style: '沉稳', image_url: '' }]
    else if (path === '/api/v1/dramas') data = { items: [{ id: 2, title: '素材项目', episodes: [{ id: 20, episode_number: 1, title: '第一集' }] }] }
    else if (path === '/api/v1/dramas/2') data = {
      id: 2, title: '素材项目', description: '用于体验项目工作流', style: 'realistic',
      episodes: [{ id: 20, episode_number: 1, title: '第一集', status: 'draft', script_content: '', image_config_id: 10, video_config_id: 11, audio_config_id: 12 }],
      characters: [{ id: 50, name: '阿宁', role: '主角', appearance: '短发', voice_style: 'female-shaonv', voice_provider: 'mock' }],
      scenes: [{ id: 60, location: '旧车站', time: '夜', description: '雨夜站台', prompt: '雨夜站台' }], props: [],
    }
    else if (path === '/api/v1/props') data = [{ id: 70, name: '旧雨伞', description: '黑色长柄伞', image_url: '' }]
    else if (path === '/api/v1/assets') data = [
      { id: 30, name: '车站参考图', type: 'image', category: 'reference', url: '/media/station.png', is_favorite: false },
      { id: 31, name: '危险链接素材', type: 'file', category: 'reference', url: 'javascript:alert(1)', is_favorite: false },
    ]

    return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, data, message: status === 200 ? 'success' : 'error' }) })
  })
}

test('desktop: login, settings and asset library workflows are reachable', async ({ page }) => {
  // This workflow covers login + multiple settings tabs + assets. On CI WebKit the
  // shared runner is slow enough that the default 30s budget is routinely exceeded
  // mid-flow (especially around animated/composite-heavy voice preview clicks).
  test.setTimeout(90_000)
  let updatedConfig: Request | undefined
  let updatedPrompt: Request | undefined
  let restoredPromptVersion: Request | undefined
  let voicePreview: Request | undefined
  let draftConfigTest: Request | undefined
  let updatedAgent: Request | undefined
  await mockAccountAPI(page)
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/ai-configs/10' && request.method() === 'PUT') updatedConfig = request
    if (new URL(request.url()).pathname === '/api/v1/prompt-templates/80' && request.method() === 'PUT') updatedPrompt = request
    if (new URL(request.url()).pathname === '/api/v1/prompt-templates/80/revisions/1/restore' && request.method() === 'POST') restoredPromptVersion = request
    if (new URL(request.url()).pathname === '/api/v1/ai-voices/female-shaonv/preview' && request.method() === 'POST') voicePreview = request
    if (new URL(request.url()).pathname === '/api/v1/ai-configs/test' && request.method() === 'POST') draftConfigTest = request
    if (new URL(request.url()).pathname === '/api/v1/agent-configs' && request.method() === 'POST') updatedAgent = request
  })
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

  await page.setViewportSize({ width: 1172, height: 650 })
  const mockConfigRow = page.getByRole('row', { name: /Mock Image/ })
  await mockConfigRow.getByRole('button', { name: '编辑' }).click()
  const editServiceDialog = page.getByRole('dialog', { name: '编辑 AI 服务' })
  await expect(editServiceDialog.getByLabel('模型')).toHaveValue('mock')
  await expect(editServiceDialog.getByLabel('启用服务')).toBeChecked()
  await expect(editServiceDialog.getByLabel('设为默认')).toBeChecked()
  await editServiceDialog.getByLabel('Base URL').fill('http://changed.localhost')
  await editServiceDialog.getByRole('button', { name: '测试当前配置' }).click()
  await expect(editServiceDialog.getByRole('alert')).toContainText('更改类型、厂商或 Base URL 后，请重新填写 API Key')
  expect(draftConfigTest).toBeUndefined()
  await editServiceDialog.getByLabel('Base URL').fill('http://localhost')
  await editServiceDialog.getByLabel('名称').fill('Mock Image Updated')
  await editServiceDialog.getByLabel('模型').fill('mock-draft')
  await editServiceDialog.getByRole('button', { name: '测试当前配置' }).click()
  await expect(editServiceDialog).toContainText('当前配置可用 · 2 ms')
  expect(draftConfigTest?.postDataJSON()).toMatchObject({ id: 10, name: 'Mock Image Updated', model: 'mock-draft', api_key: '' })
  await editServiceDialog.evaluate((dialog) => { dialog.scrollTop = dialog.scrollHeight })
  const serviceDialogBox = await editServiceDialog.boundingBox()
  const serviceDialogTitleBox = await editServiceDialog.getByRole('heading', { name: '编辑 AI 服务' }).boundingBox()
  expect(serviceDialogBox).not.toBeNull()
  expect(serviceDialogTitleBox).not.toBeNull()
  expect(serviceDialogTitleBox!.y).toBeGreaterThanOrEqual(0)
  await editServiceDialog.getByLabel('设为默认').uncheck()
  await editServiceDialog.getByRole('button', { name: '保存修改' }).click()
  expect(updatedConfig?.postDataJSON()).toMatchObject({ name: 'Mock Image Updated', model: 'mock-draft', is_default: false, is_active: true })
  await page.setViewportSize({ width: 1440, height: 1000 })

  await mockConfigRow.getByRole('button', { name: '测试连接' }).click()
  await expect(page.getByText('Mock 服务可用 · 1 ms')).toBeVisible()

  await page.getByRole('button', { name: '添加 AI 服务' }).click()
  const serviceDialog = page.getByRole('dialog', { name: '添加 AI 服务' })
  await expect(serviceDialog.getByLabel('启用服务')).toBeChecked()
  await expect(serviceDialog.getByLabel('设为默认')).toBeChecked()
  await serviceDialog.getByLabel('类型').selectOption('video')
  await expect(serviceDialog.getByLabel('厂商')).toContainText('OpenAI Sora')
  await serviceDialog.getByLabel('类型').selectOption('text')
  await expect(serviceDialog.getByLabel('厂商')).toContainText('本地 OpenAI Compatible')
  await serviceDialog.getByRole('button', { name: '取消' }).click()

  await settingsNavigation.getByRole('tab', { name: 'Agent' }).click()
  await page.getByRole('button', { name: '编辑' }).first().click()
  const agentDialog = page.getByRole('dialog', { name: '编辑 Agent' })
  await expect(agentDialog).toBeVisible()
  await agentDialog.getByLabel('说明').fill('保持人物与叙事一致性')
  await agentDialog.getByLabel('系统提示词').fill('输出结构化短剧脚本')
  await agentDialog.getByRole('button', { name: '保存 Agent' }).click()
  expect(updatedAgent?.postDataJSON()).toMatchObject({
    description: '保持人物与叙事一致性',
    system_prompt: '输出结构化短剧脚本',
  })

  await settingsNavigation.getByRole('tab', { name: '提示词' }).click()
  await expect(page.getByRole('row', { name: /剧本改写/ })).toContainText('v3')
  const promptRow = page.getByRole('row', { name: /剧本改写/ })
  await promptRow.getByRole('button', { name: '预览' }).click()
  const previewDialog = page.getByRole('dialog', { name: '预览提示词' })
  await previewDialog.getByLabel('项目标题').fill('归途')
  await previewDialog.getByLabel('用户要求').fill('重写对白')
  await previewDialog.getByRole('button', { name: '生成预览' }).click()
  await expect(previewDialog.getByText('为 归途 执行 重写对白')).toBeVisible()
  await previewDialog.getByRole('button', { name: '关闭' }).click()
  await promptRow.getByRole('button', { name: '编辑' }).click()
  const promptDialog = page.getByRole('dialog', { name: '编辑提示词' })
  await promptDialog.getByLabel('模板内容').fill('新模板 {{drama_title}}')
  await promptDialog.getByRole('button', { name: '保存修改' }).click()
  expect(updatedPrompt?.postDataJSON()).toMatchObject({ content: '新模板 {{drama_title}}' })

  await promptRow.getByRole('button', { name: '版本历史' }).click()
  const historyDialog = page.getByRole('dialog', { name: '提示词版本历史' })
  await expect(historyDialog.getByText('v3', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('初始 {{drama_title}}', { exact: true })).toBeVisible()
  await historyDialog.getByRole('button', { name: '恢复 v1' }).click()
  expect(restoredPromptVersion).toBeDefined()
  await expect(page.locator('.toast').filter({ hasText: '已恢复为新版本' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: '提示词列表已刷新' })).toBeVisible()

  await settingsNavigation.getByRole('tab', { name: '音色库' }).click()
  await expect(page.getByRole('row', { name: /少女/ })).toContainText('启用')
  await expect(page.getByRole('row', { name: /旧音色/ })).toContainText('失效')
  await page.getByRole('searchbox', { name: '搜索音色' }).fill('少女')
  await expect(page.getByRole('row', { name: /旧音色/ })).toHaveCount(0)
  await page.getByLabel('试听文本').fill('这是音色试听文本')
  const previewButton = page.getByRole('row', { name: /少女/ }).getByRole('button', { name: '试听少女' })
  await expect(previewButton).toBeEnabled()
  await previewButton.scrollIntoViewIfNeeded()
  // WebKit on CI can keep this button in a perpetual "unstable" composite state.
  // Force click still exercises the click handler and request path without waiting
  // for animation frames that never settle under software rendering.
  await previewButton.click({ force: true })
  await expect.poll(() => voicePreview?.postDataJSON() ?? null).toEqual({ text: '这是音色试听文本' })
  await expect(page.getByLabel('少女试听音频')).toHaveAttribute('src', '/static/audio/voice-preview.mp3')

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
  const unsafeAsset = page.locator('.asset-card').filter({ hasText: '危险链接素材' })
  await expect(unsafeAsset.getByText('素材地址无效')).toBeVisible()
  await expect(unsafeAsset.getByRole('link')).toHaveCount(0)
})

test('desktop: one settings source can fail without blanking other sections', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, failProviderRequests: 1 })
  await page.goto('/settings')

  await expect(page.getByText('Mock Image')).toBeVisible()
  const warning = page.getByRole('alert')
  await expect(warning).toContainText('厂商目录暂时不可用')
  await page.getByRole('tab', { name: '提示词' }).click()
  await expect(page.getByRole('row', { name: /剧本改写/ })).toBeVisible()
  await warning.getByRole('button', { name: '重试加载' }).click()
  await expect(warning).toHaveCount(0)
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

test('desktop: viewers can preview prompts without mutation controls', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, role: 'viewer' })
  await page.goto('/')
  await expect(page.getByRole('button', { name: '新建项目' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /删除项目/ })).toHaveCount(0)
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()
  await expect(page.getByRole('button', { name: '新建提示词' })).toHaveCount(0)
  const promptRow = page.getByRole('row', { name: /剧本改写/ })
  await expect(promptRow.getByRole('button', { name: '预览' })).toBeVisible()
  await promptRow.getByRole('button', { name: '版本历史' }).click()
  const historyDialog = page.getByRole('dialog', { name: '提示词版本历史' })
  await expect(historyDialog.getByText('v3', { exact: true })).toBeVisible()
  await expect(historyDialog.getByRole('button', { name: /恢复 v/ })).toHaveCount(0)
  await historyDialog.getByRole('button', { name: '关闭' }).click()
  await expect(promptRow.getByRole('button', { name: '复制' })).toHaveCount(0)
  await expect(promptRow.getByRole('button', { name: '编辑' })).toHaveCount(0)
  await expect(promptRow.getByRole('button', { name: '恢复默认' })).toHaveCount(0)

  await page.getByRole('tab', { name: '音色库' }).click()
  await expect(page.getByRole('row', { name: /少女/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /试听少女/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '同步音色' })).toHaveCount(0)
})

test('desktop: prompt workspace supports complete variables and template duplication', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()
  const promptRow = page.getByRole('row', { name: /剧本改写/ })

  await promptRow.getByRole('button', { name: '预览' }).click()
  const previewDialog = page.getByRole('dialog', { name: '预览提示词' })
  await expect(previewDialog.getByLabel('项目标题')).toHaveValue('示例短剧')
  await expect(previewDialog.getByLabel('用户要求')).toHaveValue('保持人物与画面连续')
  await previewDialog.getByRole('button', { name: '关闭' }).click()

  await promptRow.getByRole('button', { name: '复制' }).click()
  const duplicateDialog = page.getByRole('dialog', { name: '新建提示词' })
  await expect(duplicateDialog.getByRole('textbox', { name: '名称 *', exact: true })).toHaveValue('剧本改写副本')
  await expect(duplicateDialog.getByRole('textbox', { name: '标识 *', exact: true })).toHaveValue('script_rewriter_copy')
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 镜头标题', exact: true })).toBeVisible()
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 宫格行数', exact: true })).toBeVisible()
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 角色列表', exact: true })).toBeVisible()
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 角色姓名', exact: true })).toBeVisible()
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 场景地点', exact: true })).toBeVisible()
  await expect(duplicateDialog.getByRole('button', { name: '插入变量 道具名称', exact: true })).toBeVisible()
  await duplicateDialog.getByRole('textbox', { name: '模板内容 *', exact: true }).fill('镜头：')
  await duplicateDialog.getByRole('button', { name: '插入变量 镜头标题' }).click()
  await expect(duplicateDialog.getByRole('textbox', { name: '模板内容 *', exact: true })).toHaveValue('镜头：{{shot_title}}')
  await duplicateDialog.getByRole('textbox', { name: '模板内容 *', exact: true }).fill('为 {{drama_title}} 生成画面')
  await duplicateDialog.getByRole('button', { name: '检查并预览' }).click()
  await expect(duplicateDialog.getByText('草稿预览：示例短剧', { exact: true })).toBeVisible()
  await duplicateDialog.getByRole('textbox', { name: '模板内容 *', exact: true }).fill('{{secret}}')
  await duplicateDialog.getByRole('button', { name: '检查并预览' }).click()
  await expect(duplicateDialog.getByRole('alert')).toContainText('unknown prompt variable')
  await duplicateDialog.getByRole('button', { name: '取消' }).click()
})

test('desktop: prompt templates can be searched and filtered', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()

  await page.getByRole('searchbox', { name: '搜索提示词' }).fill('宫格')
  await expect(page.getByRole('row', { name: /宫格提示词/ })).toBeVisible()
  await expect(page.getByRole('row', { name: /剧本改写/ })).toHaveCount(0)
  await page.getByLabel('提示词分类').selectOption('image')
  await expect(page.getByText('没有匹配的提示词模板')).toBeVisible()
  await page.getByLabel('提示词分类').selectOption('all')
  await page.getByRole('searchbox', { name: '搜索提示词' }).fill('')
  await expect(page.getByRole('row', { name: /剧本改写/ })).toBeVisible()
})

test('desktop: prompt history ignores a stale response from a previously closed template', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, delayPromptHistory: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()

  await page.getByRole('row', { name: /剧本改写/ }).getByRole('button', { name: '版本历史' }).click()
  await page.getByRole('dialog', { name: '提示词版本历史' }).getByRole('button', { name: '关闭' }).click()
  await page.getByRole('row', { name: /宫格提示词/ }).getByRole('button', { name: '版本历史' }).click()
  const history = page.getByRole('dialog', { name: '提示词版本历史' })
  await expect(history.getByText('B-version', { exact: true })).toBeVisible()
  await page.waitForTimeout(450)
  await expect(history.getByText('A-version', { exact: true })).toHaveCount(0)
  await expect(history).toContainText('宫格提示词')
})

test('desktop: prompt draft preview ignores a stale response after content changes', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, delayPromptDraft: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()
  await page.getByRole('row', { name: /剧本改写/ }).getByRole('button', { name: '复制' }).click()
  const dialog = page.getByRole('dialog', { name: '新建提示词' })
  const content = dialog.getByRole('textbox', { name: '模板内容 *', exact: true })

  await content.fill('旧内容')
  await dialog.getByRole('button', { name: '检查并预览' }).click()
  await content.fill('新内容')
  await dialog.getByRole('button', { name: '检查并预览' }).click()
  await expect(dialog.getByText('草稿预览：新内容', { exact: true })).toBeVisible()
  await page.waitForTimeout(450)
  await expect(dialog.getByText('草稿预览：旧内容', { exact: true })).toHaveCount(0)
  await expect(dialog.getByText('草稿预览：新内容', { exact: true })).toBeVisible()
})

test('mobile: prompt editor variable palette stays usable', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()
  await page.getByRole('row', { name: /剧本改写/ }).getByRole('button', { name: '复制' }).click()
  const dialog = page.getByRole('dialog', { name: '新建提示词' })
  await expect(dialog.getByRole('button', { name: '插入变量 视频提示词' })).toBeVisible()
  const box = await dialog.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height).toBeLessThanOrEqual(844)
})

test('desktop: editors can preview voices without managing the catalog', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, role: 'editor' })
  await page.goto('/settings')
  await page.getByRole('tab', { name: '音色库' }).click()
  await expect(page.getByLabel('试听文本')).toBeVisible()
  await expect(page.getByRole('button', { name: '试听少女' })).toBeVisible()
  await expect(page.getByRole('button', { name: '同步音色' })).toHaveCount(0)
})

test('desktop: project detail uses focused content views and creation dialogs', async ({ page }) => {
  // Long multi-dialog project flow; CI WebKit regularly exceeds 30s mid-flow.
  test.setTimeout(90_000)
  let createdCharacter: Request | undefined
  let updatedCharacter: Request | undefined
  let deletedCharacter: Request | undefined
  let createdScene: Request | undefined
  let updatedScene: Request | undefined
  let copiedScene: Request | undefined
  let updatedEpisode: Request | undefined
  let characterImage: Request | undefined
  let characterVoice: Request | undefined
  let characterLibrary: Request | undefined
  let sceneImage: Request | undefined
  await mockAccountAPI(page, { signedIn: true })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/characters' && request.method() === 'POST') createdCharacter = request
    if (path === '/api/v1/characters/50' && request.method() === 'PUT') updatedCharacter = request
    if (path === '/api/v1/characters/50' && request.method() === 'DELETE') deletedCharacter = request
    if (path === '/api/v1/characters/50/generate-image' && request.method() === 'POST') characterImage = request
    if (path === '/api/v1/characters/50/generate-voice-sample' && request.method() === 'POST') characterVoice = request
    if (path === '/api/v1/characters/50/save-to-library' && request.method() === 'POST') characterLibrary = request
    if (path === '/api/v1/scenes' && request.method() === 'POST') createdScene = request
    if (path === '/api/v1/scenes/60' && request.method() === 'PUT') updatedScene = request
    if (path === '/api/v1/scenes/60/copy' && request.method() === 'POST') copiedScene = request
    if (path === '/api/v1/scenes/60/generate-image' && request.method() === 'POST') sceneImage = request
    if (path === '/api/v1/episodes/20' && request.method() === 'PUT') updatedEpisode = request
  })
  await page.goto('/drama/2')

  await expect(page.getByRole('heading', { name: '素材项目' })).toBeVisible()
  const projectNavigation = page.getByRole('tablist', { name: '项目内容' })
  await expect(projectNavigation.getByRole('tab', { name: '剧集' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('第一集', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '新增剧集' }).click()
  const episodeDialog = page.getByRole('dialog', { name: '新增剧集' })
  await expect(episodeDialog.getByLabel('图片服务')).toHaveValue('10')
  await expect(episodeDialog.getByLabel('视频服务')).toHaveValue('11')
  await expect(episodeDialog.getByLabel('音频服务')).toHaveValue('12')
  await episodeDialog.getByRole('button', { name: '取消' }).click()

  await page.locator('.episode-actions').getByRole('button', { name: '编辑剧集 第一集' }).click()
  const editEpisodeDialog = page.getByRole('dialog', { name: '编辑剧集' })
  await expect(editEpisodeDialog.getByLabel('标题（选填）')).toHaveValue('第一集')
  await editEpisodeDialog.getByLabel('标题（选填）').fill('第一集·修订')
  await editEpisodeDialog.getByRole('button', { name: '保存剧集' }).click()
  expect(updatedEpisode?.postDataJSON()).toMatchObject({
    title: '第一集·修订',
    image_config_id: 10,
    video_config_id: 11,
    audio_config_id: 12,
  })

  await projectNavigation.getByRole('tab', { name: '项目资产' }).click()
  const assetNavigation = page.getByRole('tablist', { name: '资产类型' })
  await expect(assetNavigation.getByRole('tab', { name: '角色 1' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('阿宁', { exact: true })).toBeVisible()

  const characterRow = page.getByRole('row', { name: /阿宁/ })
  await characterRow.getByRole('button', { name: '生成形象' }).click()
  expect(characterImage?.postDataJSON()).toMatchObject({ episode_id: 20 })
  await characterRow.getByRole('button', { name: '试听' }).click()
  expect(characterVoice?.postDataJSON()).toMatchObject({ episode_id: 20 })
  await characterRow.getByRole('button', { name: '存入角色库' }).click()
  expect(characterLibrary).toBeTruthy()
  await characterRow.getByRole('button', { name: '编辑' }).click()
  const editCharacterDialog = page.getByRole('dialog', { name: '编辑角色' })
  await expect(editCharacterDialog.getByLabel(/名称.*\*/)).toHaveValue('阿宁')
  await editCharacterDialog.getByLabel('定位').fill('核心主角')
  await editCharacterDialog.getByRole('button', { name: '保存角色' }).click()
  await expect(editCharacterDialog).toHaveCount(0)
  expect(updatedCharacter?.postDataJSON()).toMatchObject({ name: '阿宁', role: '核心主角' })

  const addCharacterButton = page.getByRole('button', { name: '添加角色' })
  await expect(addCharacterButton).toBeEnabled()
  await addCharacterButton.click({ force: true })
  const createCharacterDialog = page.getByRole('dialog', { name: '添加角色' })
  await createCharacterDialog.getByLabel(/名称.*\*/).fill('阿澈')
  await createCharacterDialog.getByLabel('定位').fill('配角')
  await createCharacterDialog.getByRole('button', { name: '添加角色' }).click()
  expect(createdCharacter?.postDataJSON()).toMatchObject({ drama_id: 2, name: '阿澈', role: '配角' })

  // 删除确认已从原生 confirm() 换成统一的 ConfirmDialog（role="alertdialog"）。
  await characterRow.getByRole('button', { name: '删除' }).click()
  const deleteCharacterConfirm = page.getByRole('alertdialog', { name: '删除角色' })
  await expect(deleteCharacterConfirm).toBeVisible()
  await deleteCharacterConfirm.getByRole('button', { name: '删除角色' }).click()
  await expect(deleteCharacterConfirm).toHaveCount(0)
  expect(deletedCharacter).toBeTruthy()

  await assetNavigation.getByRole('tab', { name: '场景 1' }).click()
  await expect(page.getByText('旧车站', { exact: true })).toBeVisible()
  const sceneRow = page.getByRole('row', { name: /旧车站/ })
  await sceneRow.getByRole('button', { name: '生成场景' }).click()
  expect(sceneImage?.postDataJSON()).toMatchObject({ episode_id: 20 })
  await sceneRow.getByRole('button', { name: '复制' }).click()
  const copySceneDialog = page.getByRole('dialog', { name: '复制场景' })
  await expect(copySceneDialog.getByLabel('目标剧集')).toHaveValue('20')
  await copySceneDialog.getByRole('button', { name: '确认复制' }).click()
  expect(copiedScene?.postDataJSON()).toMatchObject({ episode_id: 20 })
  await sceneRow.getByRole('button', { name: '编辑' }).click()
  const editSceneDialog = page.getByRole('dialog', { name: '编辑场景' })
  await editSceneDialog.getByLabel(/地点.*\*/).fill('旧车站·站台')
  await editSceneDialog.getByRole('button', { name: '保存场景' }).click()
  await expect(editSceneDialog).toHaveCount(0)
  expect(updatedScene?.postDataJSON()).toMatchObject({ location: '旧车站·站台' })
  const addSceneButton = page.getByRole('button', { name: '添加场景' })
  await expect(addSceneButton).toBeEnabled()
  await addSceneButton.scrollIntoViewIfNeeded()
  // WebKit on CI can leave primary action buttons in perpetual composite instability.
  await addSceneButton.click({ force: true })
  const createSceneDialog = page.getByRole('dialog', { name: '添加场景' })
  await expect(createSceneDialog).toBeVisible()
  await createSceneDialog.getByLabel(/地点.*\*/).fill('码头')
  await createSceneDialog.getByRole('button', { name: '添加场景' }).click()
  await expect(createSceneDialog).toHaveCount(0)
  expect(createdScene?.postDataJSON()).toMatchObject({ drama_id: 2, location: '码头' })

  await assetNavigation.getByRole('tab', { name: '道具 1' }).click()
  await expect(page.getByText('旧雨伞', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '添加道具' }).click()
  await expect(page.getByRole('dialog', { name: '添加道具' })).toBeVisible()
})

test('desktop: character library keeps forms in focused dialogs', async ({ page }) => {
  let created: Request | undefined
  let updated: Request | undefined
  let imported: Request | undefined
  await mockAccountAPI(page, { signedIn: true })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/character-library' && request.method() === 'POST') created = request
    if (path === '/api/v1/character-library/40' && request.method() === 'PUT') updated = request
    if (path === '/api/v1/character-library/40/import' && request.method() === 'POST') imported = request
  })
  await page.goto('/character-library')

  await expect(page.getByRole('heading', { name: '角色库' })).toBeVisible()
  await expect(page.getByRole('row', { name: /跨项目主角/ })).toBeVisible()
  await expect(page.getByLabel('角色定位')).toHaveCount(0)

  await page.getByRole('searchbox', { name: '搜索角色模板' }).fill('不存在')
  await expect(page.getByText('没有匹配的角色模板')).toBeVisible()
  await page.getByRole('searchbox', { name: '搜索角色模板' }).fill('主角')

  const editableRow = page.getByRole('row', { name: /跨项目主角/ })
  await editableRow.getByRole('button', { name: '编辑' }).click()
  const editDialog = page.getByRole('dialog', { name: '编辑角色模板' })
  await expect(editDialog.getByLabel(/名称.*\*/)).toHaveValue('跨项目主角')
  await editDialog.getByLabel('角色定位').fill('核心主角')
  await editDialog.getByRole('button', { name: '保存修改' }).click()
  expect(updated?.postDataJSON()).toMatchObject({ name: '跨项目主角', role: '核心主角' })

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

test('desktop: character library reports load and submit failures in context', async ({ page }) => {
  await mockAccountAPI(page, { signedIn: true, failCharacterLibraryRequests: 1, failCharacterLibraryCreates: 1 })
  await page.goto('/character-library')

  const loadError = page.getByRole('alert')
  await expect(loadError).toContainText('角色库暂时不可用')
  await loadError.getByRole('button', { name: '重试加载' }).click()
  await expect(page.getByRole('row', { name: /跨项目主角/ })).toBeVisible()

  await page.getByRole('button', { name: '新建角色模板' }).click()
  const dialog = page.getByRole('dialog', { name: '新建角色模板' })
  await dialog.getByLabel(/名称.*\*/).fill('失败保留模板')
  await dialog.getByRole('button', { name: '创建模板' }).click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toContainText('模板保存失败')
  await expect(dialog.getByLabel(/名称.*\*/)).toHaveValue('失败保留模板')
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

  await page.goto('/settings')
  await page.getByRole('tab', { name: '提示词' }).click()
  await expect(page.getByText('剧本改写', { exact: true })).toBeVisible()
  const settingsLayout = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }))
  expect(settingsLayout.scrollWidth).toBeLessThanOrEqual(settingsLayout.width)
})

test('desktop: platform admin can see and save registration settings', async ({ page }) => {
  let updatedSettings: Request | undefined
  await mockAccountAPI(page, {
    signedIn: true,
    isPlatformAdmin: true,
    platformSettings: { registration_enabled: true, require_email_verification: false },
  })
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/auth/platform-settings' && request.method() === 'PUT') {
      updatedSettings = request
    }
  })

  await page.goto('/settings')
  await page.getByRole('tab', { name: '安全与数据' }).click()
  await expect(page.getByText('注册设置', { exact: true })).toBeVisible()
  await expect(page.getByText('开放公开注册')).toBeVisible()
  await expect(page.getByText('要求邮箱校验')).toBeVisible()
  await expect(page.getByText('不会自动发送验证邮件')).toBeVisible()
  await expect(page.getByRole('button', { name: '修改密码' })).toBeVisible()

  const registrationToggle = page.getByLabel('开放公开注册')
  const verificationToggle = page.getByLabel('要求邮箱校验')
  await expect(registrationToggle).toBeChecked()
  await expect(verificationToggle).not.toBeChecked()
  await registrationToggle.uncheck()
  await verificationToggle.check()
  await page.getByRole('button', { name: '保存注册设置' }).click()

  await expect.poll(() => updatedSettings?.postDataJSON()).toMatchObject({
    registration_enabled: false,
    require_email_verification: true,
  })
  await expect(page.locator('.toast').filter({ hasText: '注册设置已保存' })).toBeVisible()
})

test('desktop: non-platform owner does not see registration settings', async ({ page }) => {
  await mockAccountAPI(page, {
    signedIn: true,
    role: 'owner',
    isPlatformAdmin: false,
  })

  await page.goto('/settings')
  await page.getByRole('tab', { name: '安全与数据' }).click()
  await expect(page.getByRole('button', { name: '修改密码' })).toBeVisible()
  await expect(page.getByText('注册设置', { exact: true })).toHaveCount(0)
  await expect(page.getByText('开放公开注册')).toHaveCount(0)
  await expect(page.getByText('要求邮箱校验')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '保存注册设置' })).toHaveCount(0)
})
