import { expect, test, type Locator, type Page } from '@playwright/test'

const longProjectTitle = `超长项目-${'FlyAiMovie'.repeat(32)}`

async function mockProjectHome(page: Page, authenticated = false) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = {}

    if (path === '/api/v1/auth/status') {
      data = { enabled: authenticated, setup_required: false }
    } else if (path === '/api/v1/auth/me' && authenticated) {
      data = {
        user: { id: 9, email: 'owner@example.com', display_name: '制片负责人' },
        organization: { id: 1, name: '长名称电影制作工作室', slug: 'studio' },
        role: 'owner',
        csrf_token: 'layout-test',
      }
    } else if (path === '/api/v1/auth/organizations' && authenticated) {
      data = [{ id: 1, name: '长名称电影制作工作室', slug: 'studio', role: 'owner', current: true }]
    } else if (path === '/api/v1/dramas') {
      data = {
        items: [
          {
            id: 2,
            title: longProjectTitle,
            style: 'cinematic',
            updated_at: '2026-07-21T08:00:00Z',
            episodes: [{ id: 20, episode_number: 1, title: '第一集' }],
            characters: [{ id: 30 }],
            scenes: [{ id: 31 }],
          },
        ],
      }
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, data }),
    })
  })
}

async function expectInsideViewport(locator: Locator, viewport: { width: number; height: number }) {
  const box = await locator.boundingBox()
  expect(box, '关键元素应具有可见布局区域').not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height)
}

async function expectNoPageOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    viewportWidth: document.documentElement.clientWidth,
    pageWidth: document.documentElement.scrollWidth,
  }))
  expect(dimensions.pageWidth).toBeLessThanOrEqual(dimensions.viewportWidth)
}

async function expectHorizontallyInsideViewport(locator: Locator, viewportWidth: number) {
  await locator.scrollIntoViewIfNeeded()
  const box = await locator.boundingBox()
  expect(box, '关键控件应具有可见布局区域').not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewportWidth)
}

async function mockWorkbenchLayout(page: Page) {
  const episode = {
    id: 20,
    episode_number: 1,
    title: '第一集',
    content: '故事原文',
    script_content: '第一场：车站',
    image_config_id: 51,
    video_config_id: 52,
    audio_config_id: 53,
  }

  await page.route('**/static/**', route => route.fulfill({ status: 200, contentType: 'image/png', body: '' }))
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = {}

    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/dramas/2') data = {
      id: 2,
      title: '验收短剧',
      episodes: [episode, { id: 21, episode_number: 2, title: '第二集' }],
    }
    else if (path === '/api/v1/episodes/20/characters') data = [{
      id: 30,
      name: '林夏',
      role: '主角',
      appearance: `短发蓝外套-${'appearance'.repeat(24)}`,
      voice_style: 'warm',
    }]
    else if (path === '/api/v1/episodes/20/scenes') data = [{
      id: 31,
      location: '旧车站',
      time: '黄昏',
      prompt: `雨后的站台-${'cinematic'.repeat(24)}`,
    }]
    else if (path === '/api/v1/episodes/20/storyboards') data = [{
      id: 40,
      storyboard_number: 1,
      title: '重逢',
      status: 'pending',
      description: '两人在站台相遇',
    }]
    else if (path === '/api/v1/episodes/20/pipeline-status') data = {
      has_script: true,
      characters: 1,
      scenes: 1,
      storyboards: 1,
      with_video: 0,
      with_tts: 0,
      composed: 0,
    }
    else if (path === '/api/v1/ai-configs') data = [
      { id: 51, name: '图片 Mock', service_type: 'image', provider: 'mock' },
      { id: 52, name: '视频 Mock', service_type: 'video', provider: 'mock' },
      { id: 53, name: '音频 Mock', service_type: 'audio', provider: 'mock' },
    ]
    else if (path === '/api/v1/prompt-templates') data = []
    else if (path === '/api/v1/grid/history') data = []
    else if (path === '/api/v1/assets') data = []
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/productions') data = []
    else if (path === '/api/v1/ai-voices') data = []

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, data }),
    })
  })
}

test('desktop: project home keeps its primary actions and cards inside the viewport', async ({ page }) => {
  await mockProjectHome(page)
  await page.goto('/')

  const viewport = page.viewportSize()!
  await expect(page.getByRole('heading', { name: '短剧项目' })).toBeVisible()
  await expect(page.getByRole('button', { name: '新建项目' })).toBeVisible()
  await expect(page.getByText(longProjectTitle, { exact: true })).toBeVisible()

  await expectInsideViewport(page.getByRole('button', { name: '新建项目' }), viewport)
  await expectInsideViewport(page.getByRole('button', { name: `删除项目 ${longProjectTitle}` }), viewport)
  await expectInsideViewport(page.locator('.project-card').first(), viewport)
  await expectNoPageOverflow(page)

  const projectEntry = page.getByRole('button', { name: `进入项目 ${longProjectTitle}` })
  await projectEntry.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/drama/2')
})

test('mobile: project home contains long content and keeps navigation destinations reachable', async ({ page }) => {
  await mockProjectHome(page, true)
  await page.goto('/')

  const viewport = page.viewportSize()!
  const menuButton = page.getByRole('button', { name: '打开导航' })
  await expect(menuButton).toHaveCount(1)
  await expect(menuButton).toBeVisible()
  await expect(menuButton).toHaveAttribute('aria-controls', 'primary-navigation')
  const navigation = page.getByRole('navigation', { name: '主导航' })
  await expect(navigation).toBeHidden()
  await expect(page.getByRole('button', { name: '新建项目' })).toBeVisible()
  await expect(page.getByText(longProjectTitle, { exact: true })).toBeVisible()

  await expectInsideViewport(page.getByRole('button', { name: '新建项目' }), viewport)
  await expectInsideViewport(page.getByRole('button', { name: `删除项目 ${longProjectTitle}` }), viewport)
  await expectInsideViewport(page.locator('.project-card').first(), viewport)
  await expectNoPageOverflow(page)

  await menuButton.click()
  await expect(navigation.getByRole('link', { name: '项目' })).toBeVisible()
  await expect(navigation.getByRole('link', { name: '角色库' })).toBeVisible()
  await expect(navigation.getByRole('link', { name: '任务' })).toBeVisible()
  await expect(navigation.getByRole('link', { name: '设置' })).toBeVisible()
  await expect(navigation.getByRole('button', { name: '退出登录' })).toBeVisible()
  await expectInsideViewport(navigation, viewport)
  await expectNoPageOverflow(page)

  await page.keyboard.press('Escape')
  await expect(navigation).toBeHidden()
})

test('desktop: workbench actions and stage navigation do not overflow at 1024px', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 900 })
  await mockWorkbenchLayout(page)
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  for (const name of ['自动制作', '生成配置', '刷新', '返回项目']) {
    const button = page.getByRole('button', { name, exact: true })
    await expect(button).toBeVisible()
    await expectHorizontallyInsideViewport(button, 1024)
  }

  const stages = page.getByRole('tablist', { name: '制作阶段' })
  await expect(stages).toBeVisible()
  const stageDimensions = await stages.evaluate(element => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }))
  expect(stageDimensions.scrollWidth).toBe(stageDimensions.clientWidth)
  await expectHorizontallyInsideViewport(stages, 1024)

  await page.getByRole('tab', { name: '角色与场景' }).click()
  const castPanels = page.locator('.cast-layout > .panel')
  await expect(castPanels).toHaveCount(2)
  const panelTops = await castPanels.evaluateAll(elements => elements.map(element => element.getBoundingClientRect().top))
  expect(Math.abs(panelTops[0] - panelTops[1])).toBeLessThanOrEqual(1)
  await expectNoPageOverflow(page)
})

test('mobile: workbench header and cast actions stay within the 390px page', async ({ page }) => {
  await mockWorkbenchLayout(page)
  await page.goto('/drama/2/episode/1')

  for (const name of ['自动制作', '生成配置', '刷新', '返回项目']) {
    const button = page.getByRole('button', { name, exact: true })
    await expect(button).toBeVisible()
    await expectHorizontallyInsideViewport(button, 390)
  }

  const stages = page.getByRole('tablist', { name: '制作阶段' })
  const stageDimensions = await stages.evaluate(element => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }))
  expect(stageDimensions.scrollWidth).toBeGreaterThan(stageDimensions.clientWidth)
  const finalStage = stages.getByRole('tab', { name: '合成导出' })
  await finalStage.scrollIntoViewIfNeeded()
  await expectHorizontallyInsideViewport(finalStage, 390)

  await page.getByRole('tab', { name: '角色与场景' }).click()
  const castPanels = page.locator('.cast-layout > .panel')
  await expect(castPanels).toHaveCount(2)
  for (const name of ['形象', '音色', '试听', '编辑', '删除', '生成场景', '复制', '迁移']) {
    const button = page.getByRole('button', { name, exact: true }).first()
    await expect(button).toBeVisible()
    await expectHorizontallyInsideViewport(button, 390)
  }
  await expectNoPageOverflow(page)
})
