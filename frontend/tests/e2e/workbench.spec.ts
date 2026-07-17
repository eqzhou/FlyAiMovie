import { expect, test, type Page, type Request } from '@playwright/test'

const episode = {
  id: 20,
  episode_number: 1,
  title: '第一集',
  content: '原始故事',
  script_content: '第一场：车站',
  video_url: '',
}

const drama = {
  id: 2,
  title: '验收短剧',
  episodes: [episode, { id: 21, episode_number: 2, title: '第二集', content: '' }],
}

const aiConfigs = [
  { id: 51, name: '图片 Mock', service_type: 'image', provider: 'mock' },
  { id: 52, name: '视频 Mock', service_type: 'video', provider: 'mock' },
  { id: 53, name: '音频 Mock', service_type: 'audio', provider: 'mock' },
]

const storyboard = {
  id: 40,
  storyboard_number: 1,
  title: '重逢',
  duration: 4,
  status: 'pending',
  shot_type: '中景',
  angle: '平视',
  movement: '固定',
  description: '两人在站台相遇',
  dialogue: '好久不见',
}

async function mockWorkbenchAPI(
  page: Page,
  options: {
    onEpisodeUpdate?: (request: Request) => void
    onStoryboardCreate?: (request: Request) => void
    onSceneCopy?: (request: Request) => void
    failDramaRequests?: number
    failDramaCalls?: number[]
    nullCollections?: boolean
  } = {},
) {
  let remainingDramaFailures = options.failDramaRequests ?? 0
  let dramaCall = 0
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let data: unknown = {}

    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/dramas/2' && (++dramaCall, remainingDramaFailures > 0 || options.failDramaCalls?.includes(dramaCall))) {
      remainingDramaFailures -= 1
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ code: 503, message: '服务暂时不可用' }),
      })
      return
    } else if (path === '/api/v1/dramas/2') data = drama
    else if (path === '/api/v1/episodes/20/characters') {
      data = options.nullCollections ? null : [{ id: 30, name: '林夏', role: '主角', appearance: '短发，蓝色外套', voice_style: '' }]
    } else if (path === '/api/v1/episodes/20/scenes') {
      data = options.nullCollections ? null : [{ id: 31, location: '旧车站', time: '黄昏', prompt: '雨后的站台' }]
    } else if (path === '/api/v1/episodes/20/storyboards') data = options.nullCollections ? null : [storyboard]
    else if (path === '/api/v1/episodes/20/pipeline-status') {
      data = { has_script: true, characters: 1, scenes: 1, storyboards: 1, with_video: 0, with_tts: 0, composed: 0 }
    } else if (path === '/api/v1/ai-configs') data = aiConfigs
    else if (path === '/api/v1/grid/history') data = []
    else if (path === '/api/v1/assets') data = []
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/ai-voices') data = []
    else if (path === '/api/v1/episodes/20' && request.method() === 'PUT') {
      options.onEpisodeUpdate?.(request)
      data = episode
    } else if (path === '/api/v1/storyboards' && request.method() === 'POST') {
      options.onStoryboardCreate?.(request)
      data = { ...storyboard, id: 41, storyboard_number: 2 }
    } else if (path === '/api/v1/scenes/31/copy' && request.method() === 'POST') {
      options.onSceneCopy?.(request)
      data = { id: 32, location: '旧车站', episode_id: 21 }
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, data }),
    })
  })
}

test('desktop: complete workbench stages remain usable and script can be saved', async ({ page }) => {
  let updateRequest: Request | undefined
  let storyboardCreateRequest: Request | undefined
  await mockWorkbenchAPI(page, {
    onEpisodeUpdate: (request) => { updateRequest = request },
    onStoryboardCreate: (request) => { storyboardCreateRequest = request },
  })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await expect(page.getByText('流水线 67%')).toBeVisible()

  const source = page.getByPlaceholder('粘贴小说、大纲或分场内容…')
  await source.fill('更新后的故事')
  await page.getByRole('button', { name: '保存原文' }).click()
  await expect(page.getByText('已保存原文')).toBeVisible()
  expect(updateRequest?.postDataJSON()).toEqual({ content: '更新后的故事' })

  await page.getByRole('button', { name: '2. 角色 / 场景' }).click()
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
  await expect(page.getByText('林夏')).toBeVisible()
  await expect(page.getByText('旧车站 · 黄昏')).toBeVisible()

  await page.getByRole('button', { name: '3. 宫格帧' }).click()
  await expect(page.getByRole('button', { name: '生成宫格图' })).toBeVisible()
  await expect(page.getByText('#1 重逢')).toBeVisible()

  await page.getByRole('button', { name: '4. 分镜 / 视频' }).click()
  await expect(page.getByRole('button', { name: 'AI 拆解分镜' })).toBeVisible()
  await expect(page.getByText('两人在站台相遇')).toBeVisible()
  await page.getByRole('button', { name: '添加分镜' }).click()
  await page.getByLabel('分镜标题').fill('新的镜头')
  await page.getByLabel('镜头描述').fill('人物走向站台出口')
  await page.getByRole('button', { name: '保存分镜' }).click()
  await expect(page.getByText('分镜已添加')).toBeVisible()
  expect(storyboardCreateRequest?.postDataJSON()).toMatchObject({
    episode_id: 20,
    title: '新的镜头',
    description: '人物走向站台出口',
  })

  await page.getByRole('button', { name: '5. 合成导出' }).click()
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toBeVisible()
  await expect(page.getByText('尚未导出成片')).toBeVisible()
})

test('desktop: failed initial load is actionable and can be retried', async ({ page }) => {
  await mockWorkbenchAPI(page, { failDramaRequests: 1 })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('alert')).toContainText('服务暂时不可用')
  await page.getByRole('button', { name: '重新加载' }).click()
  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
})

test('desktop: a failed background refresh does not blank the loaded workbench', async ({ page }) => {
  await mockWorkbenchAPI(page, { failDramaCalls: [2] })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await page.getByRole('button', { name: '2. 角色 / 场景' }).click()
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
  await expect(page.getByText('林夏')).toBeVisible()
})

test('desktop: null collection responses are treated as empty lists', async ({ page }) => {
  await mockWorkbenchAPI(page, { nullCollections: true })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await page.getByRole('button', { name: '2. 角色 / 场景' }).click()
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '场景' })).toBeVisible()
})

test('desktop: an existing episode can bind newly added generation configs', async ({ page }) => {
  let updateRequest: Request | undefined
  await mockWorkbenchAPI(page, { onEpisodeUpdate: request => { updateRequest = request } })
  await page.goto('/drama/2/episode/1')

  await page.getByRole('button', { name: '生成配置' }).click()
  await page.getByLabel('图片配置').selectOption('51')
  await page.getByLabel('视频配置').selectOption('52')
  await page.getByLabel('音频配置').selectOption('53')
  await page.getByRole('button', { name: '保存配置' }).click()

  await expect(page.getByText('生成配置已更新')).toBeVisible()
  expect(updateRequest?.postDataJSON()).toEqual({
    image_config_id: 51,
    video_config_id: 52,
    audio_config_id: 53,
  })
})

test('desktop: scene can be copied to another episode', async ({ page }) => {
  let copyRequest: Request | undefined
  await mockWorkbenchAPI(page, { onSceneCopy: request => { copyRequest = request } })
  await page.goto('/drama/2/episode/1')
  await page.getByRole('button', { name: '2. 角色 / 场景' }).click()
  await page.getByRole('button', { name: '复制' }).click()
  await expect(page.getByRole('dialog')).toContainText('复制场景')
  await page.getByLabel('目标剧集').selectOption('21')
  await page.getByRole('button', { name: '确认' }).click()
  await expect(page.getByText('场景已复制')).toBeVisible()
  expect(copyRequest?.postDataJSON()).toEqual({ episode_id: 21, allow_cross_drama: false })
})

test('mobile: workbench navigation and export fit a 390px viewport', async ({ page }) => {
  await mockWorkbenchAPI(page)
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await expect(page.getByRole('button', { name: '1. 剧本' })).toBeVisible()
  await page.getByRole('button', { name: '5. 合成导出' }).click()
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toBeVisible()

  const layout = await page.evaluate(() => ({
    viewportWidth: document.documentElement.clientWidth,
    documentWidth: document.documentElement.scrollWidth,
  }))
  expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth)
})
