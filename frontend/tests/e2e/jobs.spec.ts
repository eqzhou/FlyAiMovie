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
      { id: 3, kind: 'episode_merge', status: 'succeeded', progress: 100, provider: 'ffmpeg', attempt: 1, max_attempts: 3, updated_at: '2026-07-17T08:02:00Z' },
    ]
    else if (path === '/api/v1/jobs/1/events') data = [{ id: 1, stage: 'running', progress: 45, message: 'provider processing', created_at: '2026-07-17T08:00:00Z' }]
    else if (path === '/api/v1/jobs/2/retry') data = { id: 2, status: 'running' }
    else if (path === '/api/v1/jobs/batch-cancel') data = { canceled: [1], failures: {} }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data }) })
  })

  await page.goto('/jobs')
  await expect(page.getByRole('heading', { name: '任务中心' })).toBeVisible()
  await expect(page.getByText('运行中 1')).toBeVisible()
  await expect(page.getByText('失败 1')).toBeVisible()
  await expect(page.getByText('已完成 1')).toBeVisible()
  await expect(page.getByRole('row', { name: /视频生成.*运行中/ })).toBeVisible()
  await expect(page.getByRole('row', { name: /图片生成.*失败/ })).toBeVisible()
  await expect(page.getByRole('row', { name: /成片导出.*已完成/ })).toBeVisible()
  await page.getByRole('button', { name: '查看日志' }).first().click()
  await expect(page.getByText('provider processing')).toBeVisible()
  await page.getByRole('button', { name: '重试' }).click()
  expect(requests.some(request => new URL(request.url()).pathname === '/api/v1/jobs/2/retry')).toBeTruthy()
  await page.getByLabel('选择任务 1').check()
  await page.getByRole('button', { name: '批量取消' }).click()
  const batch = requests.find(request => new URL(request.url()).pathname === '/api/v1/jobs/batch-cancel')
  expect(batch?.postDataJSON()).toEqual({ job_ids: [1] })
})

test('desktop: task center shows Agent run audit details and supports cancellation', async ({ page }) => {
  const requests: Request[] = []
  let agentRetried = false
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    requests.push(request)
    let data: unknown = {}
    let status = 200
    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/agent-runs') data = [
      ...(agentRetried ? [{ id: 104, retry_of_id: 103, agent_type: 'voice_assigner', drama_id: 3, episode_id: 9, status: 'running', input: '分配音色', started_at: '2026-07-18T09:00:00Z', updated_at: '2026-07-18T09:00:00Z' }] : []),
      { id: 101, agent_type: 'extractor', drama_id: 2, episode_id: 8, status: 'running', input: '提取角色和场景', started_at: '2026-07-18T08:00:00Z', updated_at: '2026-07-18T08:01:00Z' },
      { id: 102, agent_type: 'script_rewriter', drama_id: 2, episode_id: 7, status: 'succeeded', input: '改写第一集', started_at: '2026-07-18T07:00:00Z', completed_at: '2026-07-18T07:01:00Z', updated_at: '2026-07-18T07:01:00Z' },
      { id: 103, agent_type: 'voice_assigner', drama_id: 3, episode_id: 9, status: 'failed', input: '分配音色', last_error: '未配置可用音色', started_at: '2026-07-18T06:00:00Z', completed_at: '2026-07-18T06:01:00Z', updated_at: '2026-07-18T06:01:00Z' },
    ]
    else if (path === '/api/v1/agent-runs/102') data = {
      run: {
        id: 102, agent_type: 'script_rewriter', drama_id: 2, episode_id: 7, status: 'completed', input: '改写第一集',
        output_json: JSON.stringify({ type: 'final', text: '改写后的完整剧本', toolCalls: [{ toolName: 'update_script', arguments: { episode_id: 7 } }], toolResults: [{ updated: true }] }),
        started_at: '2026-07-18T07:00:00Z', completed_at: '2026-07-18T07:01:00Z', updated_at: '2026-07-18T07:01:00Z',
      },
      events: [
        { id: 1, sequence: 1, event_type: 'prompt_resolved', payload_json: JSON.stringify({ source: 'organization_template', template_id: 80, key: 'script_rewriter', version: 7 }), created_at: '2026-07-18T07:00:00Z' },
        { id: 2, sequence: 2, event_type: 'tool_call', tool_name: 'update_script', payload_json: JSON.stringify({ call: { toolName: 'update_script', arguments: { episode_id: 7 } }, result: { updated: true } }), created_at: '2026-07-18T07:01:00Z' },
        { id: 3, sequence: 3, event_type: 'completed', payload_json: JSON.stringify({ status: 'completed', error: '' }), created_at: '2026-07-18T07:01:00Z' },
      ],
    }
    else if (path === '/api/v1/agent-runs/101/cancel') data = { id: 101, cancel_requested: true }
    else if (path === '/api/v1/agent-runs/103/retry') {
      agentRetried = true
      data = { id: 104, retry_of_id: 103, agent_type: 'voice_assigner', drama_id: 3, episode_id: 9, status: 'running' }
      status = 202
    }
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status, data }) })
  })

  await page.goto('/jobs')
  await page.getByRole('tab', { name: 'Agent 运行' }).click()
  await expect(page.getByRole('row', { name: /角色场景提取.*运行中.*项目 #2.*剧集 #8/ })).toBeVisible()
  await expect(page.getByRole('row', { name: /剧本改写.*已完成/ })).toBeVisible()
  await expect(page.getByRole('row', { name: /音色分配.*失败.*未配置可用音色/ })).toBeVisible()

  await page.getByRole('row', { name: /剧本改写.*已完成/ }).getByRole('button', { name: '查看详情' }).click()
  const dialog = page.getByRole('dialog', { name: 'Agent 运行详情' })
  await expect(dialog.getByText('改写第一集')).toBeVisible()
  await expect(dialog.getByText('改写后的完整剧本', { exact: true })).toBeVisible()
  await expect(dialog.getByText('提示词：script_rewriter · v7', { exact: true })).toBeVisible()
  const toolEvent = dialog.locator('.agent-event').filter({ hasText: 'update_script' }).first()
  await expect(toolEvent.getByText('update_script', { exact: true })).toBeVisible()
  await expect(toolEvent.getByText(/"episode_id": 7/)).toBeVisible()
  await dialog.getByRole('button', { name: '关闭' }).click()

  await page.getByRole('row', { name: /角色场景提取.*运行中/ }).getByRole('button', { name: '取消' }).click()
  expect(requests.some(request => new URL(request.url()).pathname === '/api/v1/agent-runs/101/cancel')).toBeTruthy()

  await page.getByRole('row', { name: /音色分配.*失败/ }).getByRole('button', { name: '重试' }).click()
  expect(requests.some(request => new URL(request.url()).pathname === '/api/v1/agent-runs/103/retry')).toBeTruthy()
  await expect(page.getByRole('row', { name: /#104.*重试自 #103.*音色分配.*运行中/ })).toBeVisible()
})

test('desktop: viewer can inspect Agent runs but cannot cancel them', async ({ page }) => {
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = {}
    if (path === '/api/v1/auth/status') data = { enabled: true, setup_required: false }
    else if (path === '/api/v1/auth/me') data = { user: { id: 9, email: 'viewer@example.com', display_name: '只读成员' }, organization: { id: 1, name: '制作组', slug: 'studio' }, role: 'viewer', csrf_token: 'test' }
    else if (path === '/api/v1/auth/organizations') data = [{ id: 1, name: '制作组', slug: 'studio', role: 'viewer', current: true }]
    else if (path === '/api/v1/jobs') data = [
      { id: 301, kind: 'image.generate', status: 'running', progress: 30, attempt: 1, max_attempts: 3, updated_at: '2026-07-18T09:01:00Z' },
      { id: 302, kind: 'video.generate', status: 'failed', progress: 10, attempt: 1, max_attempts: 3, updated_at: '2026-07-18T09:02:00Z' },
    ]
    else if (path === '/api/v1/agent-runs') data = [{ id: 201, agent_type: 'storyboard_breaker', drama_id: 4, episode_id: 12, status: 'running', input: '拆解分镜', started_at: '2026-07-18T09:00:00Z', updated_at: '2026-07-18T09:01:00Z' }]
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data }) })
  })

  await page.goto('/jobs')
  await expect(page.getByRole('button', { name: '批量取消' })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: /选择任务/ })).toHaveCount(0)
  await expect(page.getByRole('row', { name: /#301.*图片生成/ }).getByRole('button', { name: '取消' })).toHaveCount(0)
  await expect(page.getByRole('row', { name: /#302.*视频生成/ }).getByRole('button', { name: '重试' })).toHaveCount(0)
  await page.getByRole('tab', { name: 'Agent 运行' }).click()
  const row = page.getByRole('row', { name: /分镜拆解.*运行中/ })
  await expect(row.getByRole('button', { name: '查看详情' })).toBeVisible()
  await expect(row.getByRole('button', { name: '取消' })).toHaveCount(0)
  await expect(row.getByRole('button', { name: '重试' })).toHaveCount(0)
})

test('desktop: running Agent detail refreshes streamed tool events to completion', async ({ page }) => {
  let detailReads = 0
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = {}
    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/agent-runs') data = [{ id: 401, agent_type: 'script_rewriter', drama_id: 6, episode_id: 14, status: detailReads >= 3 ? 'completed' : 'running', input: '改写剧本', started_at: '2026-07-19T00:00:00Z', completed_at: detailReads >= 3 ? '2026-07-19T00:00:03Z' : undefined }]
    else if (path === '/api/v1/agent-runs/401') {
      detailReads++
      const completed = detailReads >= 3
      data = {
        run: { id: 401, agent_type: 'script_rewriter', drama_id: 6, episode_id: 14, status: completed ? 'completed' : 'running', input: '改写剧本', output_json: completed ? JSON.stringify({ type: 'done', text: '剧本已写回' }) : '', started_at: '2026-07-19T00:00:00Z', completed_at: completed ? '2026-07-19T00:00:03Z' : undefined },
        events: [
          { id: 1, sequence: 1, event_type: 'started', payload_json: '{"status":"running"}', created_at: '2026-07-19T00:00:00Z' },
          ...(detailReads >= 2 ? [{ id: 2, sequence: 2, event_type: 'tool_call', tool_name: 'save_script', payload_json: '{"toolName":"save_script"}', created_at: '2026-07-19T00:00:01Z' }] : []),
          ...(completed ? [
            { id: 3, sequence: 3, event_type: 'tool_result', tool_name: 'save_script', payload_json: '{"result":"ok"}', created_at: '2026-07-19T00:00:02Z' },
            { id: 4, sequence: 4, event_type: 'completed', payload_json: '{"status":"completed"}', created_at: '2026-07-19T00:00:03Z' },
          ] : []),
        ],
      }
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data }) })
  })

  await page.goto('/jobs')
  await page.getByRole('tab', { name: 'Agent 运行' }).click()
  await page.getByRole('button', { name: '查看详情' }).click()
  const dialog = page.getByRole('dialog', { name: 'Agent 运行详情' })
  await expect(dialog.getByText('已启动', { exact: true })).toBeVisible()
  await expect(dialog.getByText('save_script', { exact: true })).toBeVisible({ timeout: 5000 })
  await expect(dialog.getByText('剧本已写回', { exact: true })).toBeVisible({ timeout: 5000 })
  await expect(dialog.locator('.agent-detail-head .job-status')).toHaveText('已完成')
  expect(detailReads).toBeGreaterThanOrEqual(3)
})

test('mobile: Agent run list and detail stay within the viewport', async ({ page }) => {
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = {}
    if (path === '/api/v1/auth/status') data = { enabled: false, setup_required: false }
    else if (path === '/api/v1/jobs') data = []
    else if (path === '/api/v1/agent-runs') data = [{ id: 301, agent_type: 'grid_prompt_generator', drama_id: 5, episode_id: 13, status: 'completed', input: '生成九宫格提示词', started_at: '2026-07-18T10:00:00Z', completed_at: '2026-07-18T10:01:00Z', updated_at: '2026-07-18T10:01:00Z' }]
    else if (path === '/api/v1/agent-runs/301') data = { run: { id: 301, agent_type: 'grid_prompt_generator', drama_id: 5, episode_id: 13, status: 'completed', input: '生成九宫格提示词', output_json: JSON.stringify({ text: '九宫格结果', toolCalls: [{ toolName: 'save_grid_prompt', arguments: { episode_id: 13 } }], toolResults: [{ saved: true }] }), started_at: '2026-07-18T10:00:00Z', completed_at: '2026-07-18T10:01:00Z' }, events: [] }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data }) })
  })

  await page.goto('/jobs')
  await page.getByRole('tab', { name: 'Agent 运行' }).click()
  await page.getByRole('button', { name: '查看详情' }).click()
  const dialog = page.getByRole('dialog', { name: 'Agent 运行详情' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('save_grid_prompt', { exact: true })).toBeVisible()
  const box = await dialog.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height).toBeLessThanOrEqual(844)
})
