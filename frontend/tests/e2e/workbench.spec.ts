import { expect, test, type Page, type Request } from '@playwright/test'

const episode = {
  id: 20,
  episode_number: 1,
  title: '第一集',
  content: '原始故事',
  script_content: '第一场：车站',
  video_url: '',
  image_config_id: 51,
  video_config_id: 52,
  audio_config_id: 53,
}

const drama = {
  id: 2,
  title: '验收短剧',
  episodes: [episode, { id: 21, episode_number: 2, title: '第二集', content: '' }],
}

const aiConfigs = [
  { id: 50, name: '文本 Mock', service_type: 'text', provider: 'mock', is_default: true },
  { id: 51, name: '图片 Mock', service_type: 'image', provider: 'mock' },
  { id: 54, name: '组织默认图片', service_type: 'image', provider: 'openai', is_default: true },
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
    onFrameGenerate?: (request: Request) => void
    onStoryboardUpdate?: (request: Request) => void
    onSceneCopy?: (request: Request) => void
    onGridAssign?: (request: Request) => void
    onGridGenerate?: (request: Request) => void
    onProductionCreate?: (request: Request) => void
    failDramaRequests?: number
    failDramaCalls?: number[]
    failSceneRequests?: number
    failConfigRequests?: number
    multipleStoryboards?: boolean
    gridHistory?: boolean
    failGridAssign?: boolean
    twoGridHistories?: boolean
    delayGridAssign?: boolean
    legacyGridHistory?: boolean
    nullCollections?: boolean
    viewer?: boolean
  } = {},
) {
  let remainingDramaFailures = options.failDramaRequests ?? 0
  let remainingSceneFailures = options.failSceneRequests ?? 0
	let remainingConfigFailures = options.failConfigRequests ?? 0
  let dramaCall = 0
  let productionRun: any = null
  await page.route('**/static/**', route => route.fulfill({ status: 200, contentType: 'image/png', body: '' }))
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let data: unknown = {}

    if (path === '/api/v1/auth/status') data = { enabled: Boolean(options.viewer), setup_required: false }
    else if (path === '/api/v1/auth/me' && options.viewer) data = { user: { id: 9, email: 'viewer@example.com', display_name: '只读成员' }, organization: { id: 1, name: '制作组', slug: 'studio' }, role: 'viewer', csrf_token: 'test' }
    else if (path === '/api/v1/auth/organizations' && options.viewer) data = [{ id: 1, name: '制作组', slug: 'studio', role: 'viewer', current: true }]
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
    } else if (path === '/api/v1/episodes/20/scenes' && remainingSceneFailures > 0) {
      remainingSceneFailures -= 1
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ code: 503, message: '场景服务暂时不可用' }),
      })
      return
    } else if (path === '/api/v1/episodes/20/scenes') {
      data = options.nullCollections ? null : [{ id: 31, location: '旧车站', time: '黄昏', prompt: '雨后的站台' }]
    } else if (path === '/api/v1/episodes/20/storyboards') data = options.nullCollections ? null : [
      storyboard,
      ...(options.multipleStoryboards ? [{ ...storyboard, id: 41, storyboard_number: 2, title: '目送', description: '林夏目送列车离开', dialogue: '' }] : []),
    ]
    else if (path === '/api/v1/episodes/20/pipeline-status') {
      data = { has_script: true, characters: 1, scenes: 1, storyboards: 1, with_video: 0, with_tts: 0, composed: 0 }
    } else if (path === '/api/v1/ai-configs' && remainingConfigFailures > 0) {
		remainingConfigFailures -= 1
		await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 503, message: '模型配置服务暂时不可用' }) })
		return
	} else if (path === '/api/v1/ai-configs') data = aiConfigs
    else if (path === '/api/v1/prompt-templates') data = [
      { id: 80, key: 'storyboard_image', name: '镜头图片', category: 'image', content: '{{shot_title}} {{shot_description}}', variables_json: '["shot_title","shot_description"]', version: 1, is_active: true },
      { id: 81, key: 'storyboard_video', name: '镜头视频', category: 'video', content: '{{shot_description}} {{video_prompt}}', variables_json: '["shot_description","video_prompt"]', version: 1, is_active: true },
      { id: 82, key: 'grid_composition', name: '宫格构图', category: 'grid', content: '{{grid_rows}}x{{grid_cols}} {{user_instruction}}', variables_json: '["grid_rows","grid_cols","user_instruction"]', version: 1, is_active: true },
    ]
    else if (path === '/api/v1/prompt-templates/80/preview') data = { rendered: '重逢 两人在站台相遇，电影感构图', version: 1 }
    else if (path === '/api/v1/prompt-templates/82/preview') data = { rendered: '2x2 连续的车站重逢镜头', version: 1 }
    else if (path === '/api/v1/grid/history') data = options.gridHistory ? [{
      id: 70, mode: 'first_frame', rows: 1, cols: 2, status: 'split', cells_verified: !options.legacyGridHistory, image_url: '/static/grid.png',
      prompt: '历史宫格提示词',
      cells_json: '["/static/cell-1.png","/static/cell-2.png"]',
      assignments_json: '[{"cell_index":0,"storyboard_id":40,"frame_type":"first_frame"},{"cell_index":1,"storyboard_id":41,"frame_type":"first_frame"}]',
    }, ...(options.twoGridHistories ? [{
      id: 71, mode: 'first_frame', rows: 1, cols: 2, status: 'split', cells_verified: true, image_url: '/static/grid-2.png', prompt: '第二份宫格',
      cells_json: '["/static/cell-3.png","/static/cell-4.png"]', assignments_json: '[]',
    }] : [])] : []
    else if (path === '/api/v1/grid/history/70/assign' && request.method() === 'POST') {
      if (options.failGridAssign) {
        await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 409, message: '目标镜头不属于本集' }) })
        return
      }
      if (options.delayGridAssign) await new Promise(resolve => setTimeout(resolve, 350))
      options.onGridAssign?.(request)
      const assignment = request.postDataJSON() as { cell_index: number; storyboard_id: number; frame_type: string }
      data = {
        ...assignment, cell_url: '/static/cell-2.png',
        assignments: assignment.frame_type === 'first_frame'
          ? [assignment]
          : [{ cell_index: 0, storyboard_id: 40, frame_type: 'first_frame' }, assignment],
      }
    }
    else if (path === '/api/v1/grid/generate' && request.method() === 'POST') {
      options.onGridGenerate?.(request)
      data = {
        image: { id: 91, image_url: '/static/new-grid.png', status: 'completed' },
        history: { id: 72, mode: 'multi_ref', rows: 1, cols: 2, status: 'completed', image_url: '/static/new-grid.png' },
      }
    }
    else if (path === '/api/v1/assets') data = []
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/productions' && request.method() === 'GET') data = productionRun ? [productionRun] : []
    else if (path === '/api/v1/productions' && request.method() === 'POST') {
      options.onProductionCreate?.(request)
      productionRun = { id: 70, drama_id: 2, episode_id: 20, status: 'queued', stage: 'script', progress: 12, status_message: '正在改写剧本', attempt: 1, max_attempts: 3 }
      data = productionRun
    } else if (path === '/api/v1/productions/70/cancel' && request.method() === 'POST') {
      productionRun = { ...productionRun, status: 'canceled', status_message: '已取消' }
      data = productionRun
    } else if (path === '/api/v1/productions/70/retry' && request.method() === 'POST') {
      productionRun = { ...productionRun, status: 'queued', status_message: '等待重试', attempt: 2 }
      data = productionRun
    }
    else if (path === '/api/v1/ai-voices') data = []
    else if (path === '/api/v1/episodes/20' && request.method() === 'PUT') {
      options.onEpisodeUpdate?.(request)
      data = episode
    } else if (path === '/api/v1/storyboards' && request.method() === 'POST') {
      options.onStoryboardCreate?.(request)
      data = { ...storyboard, id: 41, storyboard_number: 2 }
    } else if (path === '/api/v1/storyboards/40/generate-frame' && request.method() === 'POST') {
      options.onFrameGenerate?.(request)
      data = { id: 90, image_url: '/static/storyboard-board.png', frame_type: 'composed' }
    } else if (path === '/api/v1/storyboards/40' && request.method() === 'PUT') {
      options.onStoryboardUpdate?.(request)
      data = { ...storyboard, ...request.postDataJSON() }
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
  let frameGenerateRequest: Request | undefined
  await mockWorkbenchAPI(page, {
    onEpisodeUpdate: (request) => { updateRequest = request },
    onStoryboardCreate: (request) => { storyboardCreateRequest = request },
    onFrameGenerate: (request) => { frameGenerateRequest = request },
  })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await expect(page.getByRole('region', { name: '制作进度' })).toContainText('67%')

  const source = page.getByPlaceholder('粘贴小说、大纲或分场内容…')
  await source.fill('更新后的故事')
  await page.getByRole('button', { name: '保存原文' }).click()
  await expect(page.getByText('已保存原文')).toBeVisible()
  expect(updateRequest?.postDataJSON()).toEqual({ content: '更新后的故事' })

  await page.getByRole('tab', { name: '角色与场景' }).click()
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
  await expect(page.getByText('林夏')).toBeVisible()
  await expect(page.getByText('旧车站 · 黄昏')).toBeVisible()

  await page.getByRole('tab', { name: '宫格帧' }).click()
  await expect(page.getByRole('button', { name: '生成宫格图' })).toBeVisible()
  await expect(page.getByText('#1 重逢')).toBeVisible()

  await page.getByRole('tab', { name: '分镜与视频' }).click()
  await expect(page.getByRole('button', { name: 'AI 拆解分镜' })).toBeVisible()
  await expect(page.getByText('两人在站台相遇')).toBeVisible()
  await page.getByRole('button', { name: '添加分镜' }).click()
  await page.getByLabel('分镜标题').fill('新的镜头')
  await page.getByLabel('镜头描述').fill('人物走向站台出口')
  await page.getByLabel('多参考图 URL（每行一个，最多 8 张）').fill('https://cdn.example/one.png\nhttps://cdn.example/two.png')
  await page.getByRole('button', { name: '保存分镜' }).click()
  await expect(page.getByText('分镜已添加')).toBeVisible()
  expect(storyboardCreateRequest?.postDataJSON()).toMatchObject({
    episode_id: 20,
    title: '新的镜头',
    description: '人物走向站台出口',
    reference_images: '["https://cdn.example/one.png","https://cdn.example/two.png"]',
  })
  await page.getByRole('button', { name: '生成分镜板', exact: true }).click()
  await expect(page.getByText('帧图任务已提交')).toBeVisible()
  expect(frameGenerateRequest?.postDataJSON()).toMatchObject({ frame_type: 'composed', episode_id: 20 })

  await page.getByRole('tab', { name: '合成导出' }).click()
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toBeVisible()
  await expect(page.getByText('尚未导出成片')).toBeVisible()
})

test('desktop: production stages expose progress and a continuous next-step action', async ({ page }) => {
  await mockWorkbenchAPI(page)
  await page.goto('/drama/2/episode/1')

  const stages = page.getByRole('tablist', { name: '制作阶段' })
  await expect(stages).toBeVisible()
  await expect(stages.getByRole('tab')).toHaveCount(5)
  await expect(stages.getByRole('tab', { name: '剧本' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('region', { name: '制作进度' })).toContainText('制作进度')
  await expect(page.getByRole('region', { name: '制作进度' })).toContainText('67%')

  await page.getByRole('button', { name: '下一步：角色与场景' }).click()
  await expect(stages.getByRole('tab', { name: '角色与场景' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
})

test('desktop: stage navigation is URL-backed and survives reload', async ({ page }) => {
  await mockWorkbenchAPI(page)
  await page.goto('/drama/2/episode/1?stage=boards')

  const stages = page.getByRole('tablist', { name: '制作阶段' })
  await expect(stages.getByRole('tab', { name: '分镜与视频' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('region', { name: '当前镜头' })).toBeVisible()

  await stages.getByRole('tab', { name: '合成导出' }).click()
  await expect(page).toHaveURL(/stage=export/)
  await page.reload()
  await expect(stages.getByRole('tab', { name: '合成导出' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toBeVisible()
})

test('desktop: storyboard workspace keeps a compact rail and one active inspector', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true })
  await page.goto('/drama/2/episode/1?stage=boards')

  const rail = page.getByRole('list', { name: '镜头列表' })
  await expect(rail.getByRole('button', { name: /镜头 1/ })).toHaveAttribute('aria-current', 'true')
  const inspector = page.getByRole('region', { name: '当前镜头' })
  await expect(inspector).toContainText('两人在站台相遇')
  await expect(inspector.getByRole('button', { name: '生成首帧' })).toHaveCount(1)

  await rail.getByRole('button', { name: /镜头 2/ }).click()
  await expect(inspector).toContainText('林夏目送列车离开')
  await expect(rail.getByRole('button', { name: /镜头 2/ })).toHaveAttribute('aria-current', 'true')
  await expect(page.locator('.storyboard-workspace')).toHaveCount(1)
})

test('desktop: partial resource failure keeps the workbench usable and retryable', async ({ page }) => {
  await mockWorkbenchAPI(page, { failSceneRequests: 1 })
  await page.goto('/drama/2/episode/1?stage=cast')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  const warning = page.getByRole('alert')
  await expect(warning).toContainText('场景加载失败')
  await expect(page.getByText('林夏')).toBeVisible()
  await warning.getByRole('button', { name: '重试加载' }).click()
  await expect(page.getByText('旧车站 · 黄昏')).toBeVisible()
  await expect(warning).toHaveCount(0)
})

test('desktop: AI config load failure is distinct from missing bindings', async ({ page }) => {
	await mockWorkbenchAPI(page, { failConfigRequests: 1 })
	await page.goto('/drama/2/episode/1')

	const warning = page.getByRole('alert')
	await expect(warning).toContainText('AI 配置加载失败')
	await expect(warning).toContainText('模型配置服务暂时不可用')
})

test('desktop: automatic production starts from confirmation and remains controllable', async ({ page }) => {
  let createRequest: Request | undefined
  await mockWorkbenchAPI(page, { onProductionCreate: request => { createRequest = request } })
  await page.goto('/drama/2/episode/1')

  await page.getByRole('button', { name: '自动制作' }).click()
  const dialog = page.getByRole('dialog', { name: '自动制作本集' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('文本 Mock')
  await expect(dialog).toContainText('图片 Mock')
  await expect(dialog).not.toContainText('组织默认图片')
  await dialog.getByRole('button', { name: '开始制作' }).click()

  expect(createRequest?.postDataJSON()).toEqual({ drama_id: 2, episode_id: 20 })
  const panel = page.getByRole('region', { name: '自动制作状态' })
  await expect(panel).toContainText('剧本生成')
  await expect(panel).toContainText('12%')
  await panel.getByRole('button', { name: '取消自动制作' }).click()
  await expect(panel).toContainText('已取消')
  await panel.getByRole('button', { name: '重试自动制作' }).click()
  await expect(panel).toContainText('等待重试')
})

test('desktop: clearing every shot disables batch generation', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true })
  await page.goto('/drama/2/episode/1?stage=boards')

  await page.getByLabel('选择镜头 1').uncheck()
  await page.getByLabel('选择镜头 2').uncheck()
  await expect(page.getByRole('button', { name: '批量首帧' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '批量视频' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '批量配音' })).toBeDisabled()
})

test('desktop: viewer can inspect the workbench without mutation controls', async ({ page }) => {
  await mockWorkbenchAPI(page, { viewer: true, gridHistory: true, multipleStoryboards: true })
  await page.goto('/drama/2/episode/1?stage=boards')

  await expect(page.getByRole('region', { name: '当前镜头' })).toBeVisible()
  await expect(page.getByRole('button', { name: '自动制作' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '生成首帧' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除镜头' })).toHaveCount(0)
  await page.getByRole('tab', { name: '剧本' }).click()
  await expect(page.getByRole('button', { name: '保存原文' })).toHaveCount(0)
  await page.getByRole('tab', { name: '合成导出' }).click()
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toHaveCount(0)
  await page.getByRole('tab', { name: '宫格帧' }).click()
  await page.getByRole('button', { name: '载入宫格 #70' }).click()
  await expect(page.getByRole('img', { name: '宫格切片 1' })).toBeVisible()
  await expect(page.getByRole('button', { name: '重新分配' })).toHaveCount(0)
})

test('desktop: prompt templates apply through focused shot and grid editors', async ({ page }) => {
  // Multi-dialog prompt flow; CI WebKit often stalls on composite-unstable buttons.
  test.setTimeout(90_000)
  let storyboardUpdate: Request | undefined
  await mockWorkbenchAPI(page, { onStoryboardUpdate: (request) => { storyboardUpdate = request } })
  await page.goto('/drama/2/episode/1')

  await page.getByRole('tab', { name: '分镜与视频' }).click()
  await page.getByRole('button', { name: '改图词' }).click()
  const shotDialog = page.getByRole('dialog', { name: '编辑图片提示词' })
  await expect(shotDialog.getByLabel('提示词模板')).toContainText('镜头图片')
  await shotDialog.getByRole('button', { name: '套用模板' }).click()
  await expect(shotDialog.getByLabel('图片提示词')).toHaveValue('重逢 两人在站台相遇，电影感构图')
  await shotDialog.getByRole('button', { name: '保存' }).click()
  expect(storyboardUpdate?.postDataJSON()).toEqual({ image_prompt: '重逢 两人在站台相遇，电影感构图' })

  await page.getByRole('button', { name: '改视频词' }).click()
  const videoDialog = page.getByRole('dialog', { name: '编辑视频提示词' })
  await expect(videoDialog.getByLabel('提示词模板')).toContainText('镜头视频')
  await videoDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('button', { name: '改对白' }).click()
  const dialogueDialog = page.getByRole('dialog', { name: '编辑对白' })
  await expect(dialogueDialog.getByLabel('提示词模板')).toHaveCount(0)
  await expect(dialogueDialog.getByLabel('对白')).toHaveValue('好久不见')
  await dialogueDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('tab', { name: '宫格帧' }).click()
  const applyTemplateButton = page.getByRole('button', { name: '套用提示词模板' })
  await expect(applyTemplateButton).toBeEnabled()
  await applyTemplateButton.click({ force: true })
  const gridDialog = page.getByRole('dialog', { name: '编辑宫格提示词' })
  await expect(gridDialog).toBeVisible()
  await gridDialog.getByRole('button', { name: '套用模板' }).click({ force: true })
  await expect(gridDialog.getByLabel('宫格提示词')).toHaveValue('2x2 连续的车站重逢镜头')
  await gridDialog.getByRole('button', { name: '应用' }).click({ force: true })
  await expect(gridDialog).toHaveCount(0)
  await expect(page.getByLabel('宫格提示词')).toHaveValue('2x2 连续的车站重逢镜头')
})

test('desktop: a split grid cell can be reassigned to a selected storyboard slot', async ({ page }) => {
  let assignRequest: Request | undefined
  await mockWorkbenchAPI(page, {
    multipleStoryboards: true,
    gridHistory: true,
    onGridAssign: request => { assignRequest = request },
  })
  await page.goto('/drama/2/episode/1?stage=grid')

  await page.getByRole('button', { name: '载入宫格 #70' }).click()
  await expect(page.getByRole('img', { name: '宫格切片 2' })).toBeVisible()
  const cell = page.getByRole('group', { name: '宫格切片 2' })
  await cell.getByLabel('目标镜头').selectOption('40')
  await cell.getByLabel('写入位置').selectOption('first_frame')
  await cell.getByRole('button', { name: '重新分配' }).click()

  await expect(page.getByText('切片 2 已写入镜头 #1 首帧')).toBeVisible()
  await expect(page.getByRole('group', { name: '宫格切片 1' })).toContainText('尚未分配')
  expect(assignRequest?.postDataJSON()).toEqual({ cell_index: 1, storyboard_id: 40, frame_type: 'first_frame' })
})

test('desktop: a failed grid cell assignment stays actionable in its cell', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true, gridHistory: true, failGridAssign: true })
  await page.goto('/drama/2/episode/1?stage=grid')
  await page.getByRole('button', { name: '载入宫格 #70' }).click()

  const cell = page.getByRole('group', { name: '宫格切片 1' })
  await cell.getByRole('button', { name: '重新分配' }).click()
  await expect(cell.getByRole('alert')).toContainText('目标镜头不属于本集')
  await expect(cell.getByRole('button', { name: '重新分配' })).toBeEnabled()
})

test('desktop: changing a loaded grid starts a clean generation context', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true, gridHistory: true })
  await page.goto('/drama/2/episode/1?stage=grid')
  await page.getByRole('button', { name: '载入宫格 #70' }).click()
  await expect(page.getByRole('img', { name: '宫格切片 1' })).toBeVisible()

  await page.getByRole('button', { name: '多参一致' }).click()
  await expect(page.getByRole('img', { name: '宫格切片 1' })).toHaveCount(0)
  await expect(page.getByRole('img', { name: '宫格图预览' })).toHaveCount(0)
  await page.getByRole('button', { name: '生成宫格图' }).click()
  await expect(page.getByRole('img', { name: '宫格图预览' })).toHaveAttribute('src', '/static/new-grid.png')
  await expect(page.getByRole('img', { name: /宫格切片/ })).toHaveCount(0)
})

test('desktop: grid history switching is locked while a cell assignment is pending', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true, gridHistory: true, twoGridHistories: true, delayGridAssign: true })
  await page.goto('/drama/2/episode/1?stage=grid')
  await page.getByRole('button', { name: '载入宫格 #70' }).click()
  const cell = page.getByRole('group', { name: '宫格切片 2' })
  await cell.getByLabel('目标镜头').selectOption('40')
  await cell.getByLabel('写入位置').selectOption('last_frame')
  await cell.getByRole('button', { name: '重新分配' }).click()

  await expect(page.getByRole('button', { name: '载入宫格 #71' })).toBeDisabled()
  await expect(page.getByText('切片 2 已写入镜头 #1 尾帧')).toBeVisible()
  await expect(page.getByRole('button', { name: '载入宫格 #71' })).toBeEnabled()
})

test('desktop: legacy grid cells remain viewable with a safe regeneration path', async ({ page }) => {
  await mockWorkbenchAPI(page, { multipleStoryboards: true, gridHistory: true, legacyGridHistory: true })
  await page.goto('/drama/2/episode/1?stage=grid')
  await page.getByRole('button', { name: '载入宫格 #70' }).click()

  await expect(page.getByText('历史切片仅供查看')).toBeVisible()
  const legacyCell = page.getByRole('group', { name: '宫格切片 1' })
  await expect(legacyCell.getByLabel('目标镜头')).toBeDisabled()
  await expect(legacyCell.getByLabel('写入位置')).toBeDisabled()
  await expect(legacyCell.getByRole('button', { name: '重新分配' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '生成宫格图' })).toBeEnabled()
})

test('desktop: first-last grid blocks an underfilled selection before generation', async ({ page }) => {
  let generationCalls = 0
  await mockWorkbenchAPI(page, { onGridGenerate: () => { generationCalls += 1 } })
  await page.goto('/drama/2/episode/1?stage=grid')

  await page.getByRole('button', { name: '首尾参考' }).click()
  await expect(page.getByRole('status')).toContainText('首尾参考需选择 2 个镜头，当前已选 1 个')
  await expect(page.getByRole('button', { name: '生成宫格图' })).toBeDisabled()
  expect(generationCalls).toBe(0)
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
  await page.getByRole('tab', { name: '角色与场景' }).click()
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()
  await expect(page.getByText('林夏')).toBeVisible()
})

test('desktop: null collection responses are treated as empty lists', async ({ page }) => {
  await mockWorkbenchAPI(page, { nullCollections: true })
  await page.goto('/drama/2/episode/1')

  await expect(page.getByRole('heading', { name: '验收短剧 · 第一集' })).toBeVisible()
  await page.getByRole('tab', { name: '角色与场景' }).click()
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
  await page.getByRole('tab', { name: '角色与场景' }).click()
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
  await expect(page.getByRole('tab', { name: '剧本' })).toBeVisible()
  await page.getByRole('tab', { name: '合成导出' }).click()
  await expect(page.getByRole('button', { name: '拼接导出成片' })).toBeVisible()
  await page.getByRole('tab', { name: '分镜与视频' }).click()
  await page.getByRole('button', { name: '改图词' }).click()
  await expect(page.getByRole('dialog', { name: '编辑图片提示词' })).toBeVisible()

  const layout = await page.evaluate(() => ({
    viewportWidth: document.documentElement.clientWidth,
    documentWidth: document.documentElement.scrollWidth,
  }))
  expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth)
})

test('mobile: global navigation collapses without hiding destinations', async ({ page }) => {
  await mockWorkbenchAPI(page)
  await page.goto('/drama/2/episode/1')

  const menuButton = page.getByRole('button', { name: '打开导航' })
  await expect(menuButton).toBeVisible()
  await menuButton.click()
  const projectLink = page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '项目' })
  await expect(projectLink).toBeVisible()
  const hitTest = await projectLink.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2)
    return Boolean(hit && (hit === element || element.contains(hit)))
  })
  expect(hitTest).toBeTruthy()

  const layout = await page.evaluate(() => ({
    viewportWidth: document.documentElement.clientWidth,
    documentWidth: document.documentElement.scrollWidth,
  }))
  expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth)
})

test('desktop: character and scene dialogs autofocus their first field', async ({ page }) => {
  await mockWorkbenchAPI(page)
  await page.goto('/drama/2/episode/1?stage=cast')
  await expect(page.getByRole('heading', { name: '角色' })).toBeVisible()

  await page.getByRole('button', { name: '添加角色' }).click()
  const characterDialog = page.getByRole('dialog', { name: '添加角色' })
  await expect(characterDialog).toBeVisible()
  await expect(characterDialog.getByLabel(/角色名.*\*/)).toBeFocused()
  await characterDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('button', { name: '添加场景' }).click()
  const sceneDialog = page.getByRole('dialog', { name: '添加场景' })
  await expect(sceneDialog).toBeVisible()
  await expect(sceneDialog.getByLabel(/地点.*\*/)).toBeFocused()
})
