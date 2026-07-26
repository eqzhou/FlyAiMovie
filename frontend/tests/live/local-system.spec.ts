import { expect, request, test } from '@playwright/test'

const ownerEmail = 'live-owner@flyaimovie.test'
const invitedEmail = 'live-editor@flyaimovie.test'
const ownerPassword = process.env.E2E_PASSWORD || ''
const invitedPassword = process.env.E2E_INVITED_PASSWORD || ownerPassword
let organizationCreated = false

test.beforeAll(() => {
  if (process.env.E2E_DISPOSABLE !== '1') {
    throw new Error('Set E2E_DISPOSABLE=1 only for an empty disposable database')
  }
  if (ownerPassword.length < 12 || ownerPassword.length > 128) {
    throw new Error('Set E2E_PASSWORD to a disposable 12-128 character password')
  }
})

test.afterAll(async () => {
  if (!organizationCreated) return
  const api = await request.newContext({ baseURL: process.env.LIVE_BASE_URL || 'http://127.0.0.1:8088' })
  try {
    const login = await api.post('/api/v1/auth/login', { data: { email: ownerEmail, password: ownerPassword } })
    expect(login.ok()).toBeTruthy()
    const actor = await login.json()
    const deleted = await api.delete('/api/v1/organization', {
      headers: { 'X-CSRF-Token': actor.data.csrf_token },
      data: { password: ownerPassword, confirmation: 'live-browser' },
    })
    expect(deleted.status()).toBe(200)
  } finally {
    await api.dispose()
  }
})

test('live backend: complete production workflow plus account administration', async ({ page }) => {
    test.setTimeout(120_000)
    await page.goto('/setup')
    await expect(page.getByRole('heading', { name: '初始化制作空间' })).toBeVisible()
    await page.getByLabel('空间名称').fill('Live Browser')
    await page.getByLabel('显示名称').fill('Live Owner')
    await page.getByLabel('邮箱').fill(ownerEmail)
    await page.getByLabel('密码', { exact: true }).fill(ownerPassword)
    await page.getByLabel('确认密码').fill(ownerPassword)
    await page.getByRole('button', { name: '创建空间' }).click()
    await expect(page.getByRole('heading', { name: '短剧项目' })).toBeVisible()
    organizationCreated = true

    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
    await expect(page.getByText('mock-image')).toBeVisible()

    await page.getByRole('button', { name: '添加 AI 服务' }).click()
    const addService = page.getByRole('dialog', { name: '添加 AI 服务' })
    await addService.getByLabel('类型').selectOption('text')
    await addService.getByLabel('厂商').selectOption('openai')
    await addService.getByLabel('名称').fill('Live Text Config')
    await addService.getByLabel('Base URL').fill('https://api.openai.com')
    await addService.getByLabel('API Key').fill('live-test-key')
    await addService.getByLabel('模型').fill('gpt-4o-mini')
    await addService.getByRole('button', { name: '保存配置' }).click()
    const configRow = page.getByRole('row').filter({ hasText: 'Live Text Config' })
    await expect(configRow).toBeVisible()
    // 删除确认已从原生 confirm() 换成统一的 ConfirmDialog（role="alertdialog"）。
    await configRow.getByRole('button', { name: '删' }).click()
    await page.getByRole('alertdialog', { name: '删除 AI 服务' }).getByRole('button', { name: '删除配置' }).click()
    await expect(configRow).toHaveCount(0)

    await page.getByRole('tab', { name: 'Agent' }).click()
    await page.getByRole('button', { name: '编辑' }).first().click()
    const agentDialog = page.getByRole('dialog', { name: '编辑 Agent' })
    await agentDialog.getByRole('button', { name: '保存 Agent' }).click()

    await page.getByRole('tab', { name: '组织与权限' }).click()
    const quotaPanel = page.locator('.panel').filter({ hasText: '生成配额' })
    await quotaPanel.locator('input[type="number"]').nth(0).fill('321')
    await quotaPanel.locator('input[type="number"]').nth(1).fill('7')
    await quotaPanel.locator('input[type="number"]').nth(2).fill('25')
    await quotaPanel.locator('input[type="number"]').nth(3).fill('75')
    await quotaPanel.getByRole('button', { name: '保存配额' }).click()
    await expect(page.getByText('生成配额已保存')).toBeVisible()

    await page.goto('/')
    await page.getByRole('button', { name: '新建项目' }).click()
    await page.getByPlaceholder('例如：雨夜重逢').fill('Live UI 项目')
    await page.getByRole('button', { name: '创建', exact: true }).click()
    await page.getByText('Live UI 项目').click()
    await page.getByRole('button', { name: '素材库' }).click()
    await expect(page.getByRole('heading', { name: 'Live UI 项目 · 素材库' })).toBeVisible()

    await page.getByRole('button', { name: '上传图片' }).click()
		await page.getByLabel('素材文件').setInputFiles({
      name: 'live-pixel.png',
      mimeType: 'image/png',
      buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64'),
    })
    await page.getByRole('button', { name: '上传并绑定' }).click()
    await expect(page.getByText('live-pixel.png')).toBeVisible()

    await page.goto('/')
    await page.getByText('Live UI 项目').click()
    await page.locator('.project-card').filter({ hasText: '第 1 集' }).click()
    await expect(page.getByRole('heading', { name: 'Live UI 项目 · 第1集' })).toBeVisible()
    await page.getByPlaceholder('粘贴小说、大纲或分场内容…').fill('## S01 | 内景 · 客厅 | 夜\n阿宁：你好。')
    await page.getByRole('button', { name: '保存原文' }).click()
    await page.getByRole('button', { name: 'AI 改写剧本' }).click()
    await expect(page.getByText('script_rewriter 完成')).toBeVisible()
    await page.getByRole('button', { name: '2. 角色 / 场景' }).click()
    await page.getByRole('button', { name: 'AI 提取' }).click()
    await expect(page.getByText('extractor 完成')).toBeVisible()
    await page.getByRole('button', { name: '4. 分镜 / 视频' }).click()
    await page.getByRole('button', { name: 'AI 拆解分镜' }).click()
    await expect(page.getByText('storyboard_breaker 完成')).toBeVisible()
    const shot = page.locator('.shot-card').first()
    await expect(shot).toBeVisible()
    await shot.getByRole('button', { name: '首帧', exact: true }).click()
    await shot.getByRole('button', { name: '分镜板', exact: true }).click()
    await expect(shot.locator('img')).toHaveCount(2)
    await shot.getByRole('button', { name: '视频', exact: true }).click()
    await expect(shot.locator('video')).toBeVisible()
    await shot.getByRole('button', { name: '配音', exact: true }).click()
    await expect(shot.locator('audio')).toBeVisible({ timeout: 30_000 })
    await page.getByRole('button', { name: '5. 合成导出' }).click()
    await page.getByRole('button', { name: '批量合成镜头' }).click()
    await expect(page.getByText('已合成')).toBeVisible({ timeout: 45_000 })
    await page.getByRole('button', { name: '拼接导出成片' }).click()
    await expect(page.locator('.wb-main video').first()).toBeVisible({ timeout: 45_000 })

    await page.goto('/settings')
    await page.getByRole('tab', { name: '组织与权限' }).click()
    const cachePanel = page.locator('.panel').filter({ hasText: '本地缓存' })
    await expect(cachePanel).toContainText(/对象 [1-9]/)
    await cachePanel.getByRole('button', { name: '清理过期缓存' }).click()
    await page.getByRole('alertdialog', { name: '清理过期缓存' }).getByRole('button', { name: '清理缓存' }).click()
    await expect(page.getByText('过期缓存已清理')).toBeVisible()
    await page.getByRole('button', { name: '创建邀请' }).click()
    const inviteDialog = page.getByRole('dialog', { name: '创建邀请' })
    await inviteDialog.getByLabel('邀请邮箱').fill(invitedEmail)
    await inviteDialog.getByRole('button', { name: '创建安全邀请' }).click()
    const invitationURL = await inviteDialog.locator('input[readonly]').inputValue()
    await page.goto(invitationURL)
    await page.getByLabel('显示名称').fill('Live Editor')
    await page.getByLabel('新账号初始密码（新账号必填）').fill(invitedPassword)
    await page.getByRole('button', { name: '接受邀请' }).click()
    await expect(page.getByRole('heading', { name: '短剧项目' })).toBeVisible()

    await page.getByRole('button', { name: '退出登录' }).click()
    await page.getByLabel('邮箱').fill(ownerEmail)
    await page.getByLabel('密码').fill(ownerPassword)
    await page.getByRole('button', { name: '登录' }).click()
		await expect(page.getByText('Live UI 项目')).toBeVisible()
})
