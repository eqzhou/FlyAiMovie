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

test('live backend: account, settings, invitation and asset workflows', async ({ page }) => {
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

    const addService = page.locator('.panel').filter({ hasText: '添加 AI 服务' })
    await addService.locator('.field').filter({ hasText: '类型' }).locator('select').selectOption('text')
    await addService.locator('.field').filter({ hasText: '厂商' }).locator('select').selectOption('openai')
    await addService.locator('.field').filter({ hasText: '名称' }).locator('input').fill('Live Text Config')
    await addService.locator('.field').filter({ hasText: 'Base URL' }).locator('input').fill('https://api.openai.com')
    await addService.locator('.field').filter({ hasText: 'API Key' }).locator('input').fill('live-test-key')
    await addService.locator('.field').filter({ hasText: '模型' }).locator('input').fill('gpt-4o-mini')
    await addService.getByRole('button', { name: '保存配置' }).click()
    const configRow = page.getByRole('row').filter({ hasText: 'Live Text Config' })
    await expect(configRow).toBeVisible()
    page.once('dialog', (dialog) => dialog.accept())
    await configRow.getByRole('button', { name: '删' }).click()
    await expect(configRow).toHaveCount(0)

    const agentPanel = page.locator('.panel').filter({ hasText: 'Agent 预设' })
    await agentPanel.getByRole('button', { name: '编辑' }).first().click()
    await agentPanel.getByRole('button', { name: '保存 Agent' }).click()

    const quotaPanel = page.locator('.panel').filter({ hasText: '生成配额' })
    await quotaPanel.locator('input[type="number"]').nth(0).fill('321')
    await quotaPanel.locator('input[type="number"]').nth(1).fill('7')
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

    await page.goto('/settings')
    await page.locator('.field').filter({ hasText: '已有账号邀请邮箱' }).locator('input').fill(invitedEmail)
    await page.getByRole('button', { name: '创建安全邀请' }).click()
    const invitationURL = await page.locator('input[readonly]').inputValue()
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
