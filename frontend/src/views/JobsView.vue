<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { agentAPI, jobsAPI } from '../api'
import { authStore } from '../auth'

const activeTab = ref<'jobs' | 'agents'>('jobs')
const jobs = ref<any[]>([])
const agentRuns = ref<any[]>([])
const selected = ref<number[]>([])
const events = ref<Record<number, any[]>>({})
const expanded = ref<number | null>(null)
const status = ref('')
const kind = ref('')
const agentStatus = ref('')
const agentType = ref('')
const agentDetail = ref<{ run: any; events: any[] } | null>(null)
const detailLoading = ref(false)
const detailRefreshing = ref(false)
const retryingAgentRunID = ref<number | null>(null)
const cancelingAgentRunID = ref<number | null>(null)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const pendingJobActionIDs = ref<number[]>([])
const batchCanceling = ref(false)
let timer: number | null = null
let detailTimer: number | null = null
let noticeTimer: number | null = null
let listRequestToken = 0
let disposed = false

function showNotice(text: string) {
  notice.value = text
  if (noticeTimer) window.clearTimeout(noticeTimer)
  noticeTimer = window.setTimeout(() => {
    if (notice.value === text) notice.value = ''
    noticeTimer = null
  }, 3200)
}

const active = computed(() => activeTab.value === 'jobs'
  ? jobs.value.some(job => !['succeeded', 'failed', 'canceled'].includes(job.status))
  : agentRuns.value.some(run => run.status === 'running'))
const canCancelAgentRun = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const canManageTasks = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const cancellableSelected = computed(() => selected.value.filter(id => {
	if (!canManageTasks.value) return false
  if (pendingJobActionIDs.value.includes(id)) return false
  const job = jobs.value.find(row => row.id === id)
  return job && !['succeeded', 'failed', 'canceled'].includes(job.status)
}))
const jobSummary = computed(() => ({
  running: jobs.value.filter(job => ['queued', 'running', 'waiting_provider'].includes(job.status)).length,
  failed: jobs.value.filter(job => job.status === 'failed').length,
  succeeded: jobs.value.filter(job => job.status === 'succeeded').length,
}))
const agentSummary = computed(() => ({
  running: agentRuns.value.filter(run => run.status === 'running').length,
  failed: agentRuns.value.filter(run => run.status === 'failed').length,
  completed: agentRuns.value.filter(run => ['completed', 'succeeded'].includes(run.status)).length,
}))
const agentDetailEvents = computed(() => {
  if (!agentDetail.value) return []
  if (agentDetail.value.events?.length) return agentDetail.value.events
  const output = parseJSON(agentDetail.value.run.output_json)
  if (!output || typeof output !== 'object') return []
  const record = output as Record<string, unknown>
  const calls = Array.isArray(record.toolCalls) ? record.toolCalls : []
  const results = Array.isArray(record.toolResults) ? record.toolResults : []
  return calls.map((call: any, index: number) => ({
    id: `derived-${index}`,
    event_type: 'tool_call',
    tool_name: call?.toolName || '工具调用',
    payload_json: JSON.stringify({ call, result: results[index] }, null, 2),
    created_at: agentDetail.value?.run.completed_at || agentDetail.value?.run.updated_at,
  }))
})

const statusLabels: Record<string, string> = { queued: '排队中', running: '运行中', waiting_provider: '等待厂商', succeeded: '已完成', failed: '失败', canceled: '已取消' }
const kindLabels: Record<string, string> = {
  'image.generate': '图片生成',
  'video.generate': '视频生成',
  'audio.generate': '语音生成',
  'tts.generate': '语音生成',
  'video.compose': '镜头合成',
  'video.merge': '成片导出',
  episode_compose: '镜头合成',
  episode_merge: '成片导出',
  'media.metadata': '媒体解析',
}
const agentStatusLabels: Record<string, string> = { running: '运行中', started: '已启动', prompt_resolved: '提示词版本', tool_call: '调用工具', tool_result: '工具结果', completed: '已完成', succeeded: '已完成', failed: '失败', canceled: '已取消' }
const agentTypeLabels: Record<string, string> = {
  script_rewriter: '剧本改写',
  extractor: '角色场景提取',
  storyboard_breaker: '分镜拆解',
  voice_assigner: '音色分配',
  grid_prompt_generator: '宫格提示词',
}

function statusLabel(value: string) { return statusLabels[value] || value }
function kindLabel(value: string) { return kindLabels[value] || value.replaceAll('.', ' / ') }
function agentStatusLabel(value: string) { return agentStatusLabels[value] || value }
function agentTypeLabel(value: string) { return agentTypeLabels[value] || value }

function agentEventTitle(event: any) {
  if (event.event_type !== 'prompt_resolved') return event.tool_name || agentStatusLabel(event.event_type)
  const payload = parseJSON(event.payload_json)
  if (!payload || typeof payload !== 'object') return '提示词版本'
  const record = payload as Record<string, unknown>
  const key = String(record.key || '未命名')
  const version = Number(record.version || 0)
  if (version > 0) return `提示词：${key} · v${version}`
  if (record.source === 'agent_config') return `提示词：${key} · Agent 配置`
  return `提示词：${key} · 内置`
}

async function load() {
  const token = ++listRequestToken
  loading.value = true
  error.value = ''
  try {
    const rows = await jobsAPI.list({ status: status.value || undefined, kind: kind.value || undefined, limit: 100 })
    if (token !== listRequestToken) return
    jobs.value = rows
    selected.value = selected.value.filter(id => jobs.value.some(row => row.id === id))
  } catch (reason) {
    if (token !== listRequestToken) return
    error.value = reason instanceof Error ? reason.message : '加载任务失败'
  } finally {
    if (token === listRequestToken) loading.value = false
  }
}

async function loadAgentRuns() {
  const token = ++listRequestToken
  loading.value = true
  error.value = ''
  try {
    const rows = await agentAPI.runs({ status: agentStatus.value || undefined, agent_type: agentType.value || undefined })
    if (token !== listRequestToken) return
    agentRuns.value = rows
  } catch (reason) {
    if (token !== listRequestToken) return
    error.value = reason instanceof Error ? reason.message : '加载 Agent 运行记录失败'
  } finally {
    if (token === listRequestToken) loading.value = false
  }
}

async function switchTab(tab: 'jobs' | 'agents') {
  activeTab.value = tab
  error.value = ''
  if (tab === 'agents') await loadAgentRuns()
  else await load()
}

async function refresh() {
  if (activeTab.value === 'agents') await loadAgentRuns()
  else await load()
}

async function toggleEvents(job: any) {
  if (expanded.value === job.id) {
    expanded.value = null
    return
  }
  try {
    events.value = { ...events.value, [job.id]: await jobsAPI.events(job.id) }
    expanded.value = job.id
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载任务事件失败'
  }
}

async function cancel(job: any) {
  if (pendingJobActionIDs.value.includes(job.id)) return
  pendingJobActionIDs.value = [...pendingJobActionIDs.value, job.id]
  try {
    await jobsAPI.cancel(job.id)
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '取消任务失败'
  } finally {
    pendingJobActionIDs.value = pendingJobActionIDs.value.filter((id) => id !== job.id)
  }
}

async function retry(job: any) {
  if (pendingJobActionIDs.value.includes(job.id)) return
  pendingJobActionIDs.value = [...pendingJobActionIDs.value, job.id]
  try {
    await jobsAPI.retry(job.id)
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '重试任务失败'
  } finally {
    pendingJobActionIDs.value = pendingJobActionIDs.value.filter((id) => id !== job.id)
  }
}

async function batchCancel() {
  if (batchCanceling.value || !cancellableSelected.value.length) return
  const targetIDs = [...cancellableSelected.value]
  batchCanceling.value = true
  pendingJobActionIDs.value = [...new Set([...pendingJobActionIDs.value, ...targetIDs])]
  try {
    await jobsAPI.batchCancel(targetIDs)
    selected.value = []
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '批量取消失败'
  } finally {
    pendingJobActionIDs.value = pendingJobActionIDs.value.filter((id) => !targetIDs.includes(id))
    batchCanceling.value = false
  }
}

async function openAgentDetail(run: any) {
  detailLoading.value = true
  error.value = ''
  try {
    agentDetail.value = await agentAPI.run(run.id)
    startAgentDetailRefresh()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载 Agent 详情失败'
  } finally {
    detailLoading.value = false
  }
}

function stopAgentDetailRefresh() {
  if (detailTimer) window.clearInterval(detailTimer)
  detailTimer = null
}

function startAgentDetailRefresh() {
  stopAgentDetailRefresh()
  if (agentDetail.value?.run.status !== 'running') return
  detailTimer = window.setInterval(refreshAgentDetail, 2000)
}

async function refreshAgentDetail() {
  const runID = agentDetail.value?.run.id
  if (!runID || detailRefreshing.value || document.visibilityState !== 'visible') return
  detailRefreshing.value = true
  try {
    const detail = await agentAPI.run(runID) as any
    if (agentDetail.value?.run.id !== runID) return
    agentDetail.value = detail
    if (detail.run.status !== 'running') {
      stopAgentDetailRefresh()
      await loadAgentRuns()
    }
  } catch (reason) {
    stopAgentDetailRefresh()
    error.value = reason instanceof Error ? reason.message : '刷新 Agent 详情失败'
  } finally {
    detailRefreshing.value = false
  }
}

function closeAgentDetail() {
  stopAgentDetailRefresh()
  agentDetail.value = null
}

async function cancelAgentRun(run: any) {
  if (cancelingAgentRunID.value !== null) return
  cancelingAgentRunID.value = run.id
  error.value = ''
  try {
    await agentAPI.cancelRun(run.id)
    await loadAgentRuns()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '取消 Agent 运行失败'
  } finally {
    cancelingAgentRunID.value = null
  }
}

async function retryAgentRun(run: any) {
  retryingAgentRunID.value = run.id
  error.value = ''
  notice.value = ''
  try {
    const retry = await agentAPI.retryRun(run.id) as any
    showNotice(`Agent 重试 #${retry.id} 已启动`)
    await loadAgentRuns()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '重试 Agent 运行失败'
  } finally {
    retryingAgentRunID.value = null
  }
}

function parseJSON(value: unknown) {
  if (typeof value !== 'string' || !value.trim()) return value
  try { return JSON.parse(value) } catch { return value }
}

function prettyJSON(value: unknown) {
  const parsed = parseJSON(value)
  return typeof parsed === 'string' ? parsed : JSON.stringify(parsed, null, 2)
}

function outputText(value: unknown) {
  const parsed = parseJSON(value)
  return parsed && typeof parsed === 'object' && 'text' in parsed ? String((parsed as Record<string, unknown>).text || '') : ''
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

onMounted(async () => {
  disposed = false
  await load()
  if (disposed) return
  timer = window.setInterval(() => { if (active.value && document.visibilityState === 'visible') refresh() }, 4000)
})
onUnmounted(() => {
  disposed = true
  listRequestToken += 1
  if (timer) window.clearInterval(timer)
  if (noticeTimer) window.clearTimeout(noticeTimer)
  stopAgentDetailRefresh()
})
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div><h1 class="page-title">任务中心</h1><p class="page-desc">生成任务与 Agent 执行审计</p></div>
      <button class="btn" :disabled="loading" @click="refresh">刷新</button>
    </div>
    <div class="settings-tabs" role="tablist" aria-label="任务类型">
      <button role="tab" :aria-selected="activeTab === 'jobs'" :class="{ active: activeTab === 'jobs' }" @click="switchTab('jobs')">生成任务</button>
      <button role="tab" :aria-selected="activeTab === 'agents'" :class="{ active: activeTab === 'agents' }" @click="switchTab('agents')">Agent 运行</button>
    </div>
    <div v-if="activeTab === 'jobs'" class="job-summary" aria-label="任务概览">
      <span><i class="status-dot run"></i>运行中 <strong>{{ jobSummary.running }}</strong></span>
      <span><i class="status-dot fail"></i>失败 <strong>{{ jobSummary.failed }}</strong></span>
      <span><i class="status-dot ok"></i>已完成 <strong>{{ jobSummary.succeeded }}</strong></span>
    </div>
    <div v-else class="job-summary" aria-label="Agent 运行概览">
      <span><i class="status-dot run"></i>运行中 <strong>{{ agentSummary.running }}</strong></span>
      <span><i class="status-dot fail"></i>失败 <strong>{{ agentSummary.failed }}</strong></span>
      <span><i class="status-dot ok"></i>已完成 <strong>{{ agentSummary.completed }}</strong></span>
    </div>
    <div v-if="activeTab === 'jobs'" class="audit-toolbar">
      <select v-model="status" aria-label="任务状态" @change="load">
        <option value="">全部状态</option><option value="queued">排队</option><option value="running">运行中</option>
        <option value="waiting_provider">等待厂商</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="canceled">已取消</option>
      </select>
      <input v-model.trim="kind" aria-label="任务类型" placeholder="按任务类型过滤" @keyup.enter="load" />
      <button v-if="canManageTasks" class="btn btn-danger" :disabled="batchCanceling || !cancellableSelected.length" @click="batchCancel">{{ batchCanceling ? '取消中…' : '批量取消' }}</button>
    </div>
    <div v-else class="audit-toolbar">
      <select v-model="agentStatus" aria-label="Agent 状态" @change="loadAgentRuns">
        <option value="">全部状态</option><option value="running">运行中</option><option value="completed">已完成</option><option value="failed">失败</option><option value="canceled">已取消</option>
      </select>
      <select v-model="agentType" aria-label="Agent 类型" @change="loadAgentRuns">
        <option value="">全部 Agent</option><option v-for="(label, value) in agentTypeLabels" :key="value" :value="value">{{ label }}</option>
      </select>
    </div>
    <div v-if="error" class="inline-alert" role="alert">
      <div><strong>{{ activeTab === 'jobs' ? '任务操作失败' : 'Agent 操作失败' }}</strong><span>{{ error }}</span></div>
      <button class="btn" type="button" :disabled="loading" @click="refresh">重试</button>
    </div>
    <div v-if="notice" class="toast" role="status">{{ notice }}</div>
    <div v-if="activeTab === 'jobs'" class="panel audit-table-wrap">
      <table v-if="jobs.length" class="table jobs-table">
        <thead><tr><th v-if="canManageTasks"></th><th>任务</th><th>状态</th><th>进度</th><th>厂商</th><th>尝试</th><th>更新时间</th><th>操作</th></tr></thead>
        <tbody>
          <template v-for="job in jobs" :key="job.id">
            <tr>
              <td v-if="canManageTasks"><input v-model="selected" type="checkbox" :value="job.id" :aria-label="`选择任务 ${job.id}`" /></td>
              <td>#{{ job.id }}<br><span class="muted">{{ kindLabel(job.kind) }}</span></td>
              <td><span class="job-status" :class="job.status">{{ statusLabel(job.status) }}</span><div v-if="job.last_error" class="job-error">{{ job.last_error }}</div></td>
              <td><div class="progress-bar" :class="{ active: ['queued', 'running', 'waiting_provider'].includes(job.status) }"><i :style="`width: ${job.progress || 0}%`"></i></div><span class="muted">{{ job.progress || 0 }}%</span></td>
              <td>{{ job.provider || '-' }}<br><span class="muted">{{ job.provider_task_id || '' }}</span></td>
              <td>{{ job.attempt }} / {{ job.max_attempts }}</td>
              <td>{{ formatTime(job.updated_at) }}</td>
              <td><div class="mini-actions">
                <button class="btn" @click="toggleEvents(job)">{{ expanded === job.id ? '收起日志' : '查看日志' }}</button>
                <button v-if="canManageTasks && !['succeeded','failed','canceled'].includes(job.status)" class="btn btn-danger" :disabled="pendingJobActionIDs.includes(job.id)" @click="cancel(job)">{{ pendingJobActionIDs.includes(job.id) ? '取消中…' : '取消' }}</button>
                <button v-if="canManageTasks && ['failed','canceled'].includes(job.status)" class="btn" :disabled="job.attempt >= job.max_attempts || pendingJobActionIDs.includes(job.id)" @click="retry(job)">{{ pendingJobActionIDs.includes(job.id) ? '重试中…' : '重试' }}</button>
              </div></td>
            </tr>
            <tr v-if="expanded === job.id"><td :colspan="canManageTasks ? 8 : 7"><div class="job-events">
              <div v-for="event in events[job.id] || []" :key="event.id"><span class="muted">{{ formatTime(event.created_at) }}</span> · {{ event.stage }} · {{ event.message }}</div>
              <div v-if="!(events[job.id] || []).length" class="muted">暂无阶段日志</div>
            </div></td></tr>
          </template>
        </tbody>
      </table>
      <div v-if="loading && !jobs.length" class="page-loading" role="status" aria-live="polite">
        <div class="page-loading-mark" aria-hidden="true"></div>
        <div><strong>加载任务中</strong><p class="muted">同步生成任务与阶段日志…</p></div>
      </div>
      <div v-else-if="!loading && !jobs.length" class="surface-empty empty jobs-empty">
        <strong>暂无任务</strong>
        <span class="muted">在工作台发起图片、视频、配音或合成后，任务会显示在这里。</span>
      </div>
    </div>
    <div v-else class="panel audit-table-wrap">
      <table v-if="agentRuns.length" class="table agent-runs-table">
        <thead><tr><th>运行</th><th>Agent</th><th>状态</th><th>关联范围</th><th>开始时间</th><th>完成时间</th><th>操作</th></tr></thead>
        <tbody><tr v-for="run in agentRuns" :key="run.id">
          <td>#{{ run.id }}<div v-if="run.retry_of_id" class="agent-retry-origin">重试自 #{{ run.retry_of_id }}</div></td>
          <td><strong>{{ agentTypeLabel(run.agent_type) }}</strong><div class="agent-input-preview">{{ run.input || '无输入内容' }}</div></td>
          <td><span class="job-status" :class="run.status">{{ agentStatusLabel(run.status) }}</span><div v-if="run.last_error" class="job-error">{{ run.last_error }}</div></td>
          <td>项目 #{{ run.drama_id }}<br><span class="muted">剧集 #{{ run.episode_id }}</span></td>
          <td>{{ formatTime(run.started_at) }}</td>
          <td>{{ run.completed_at ? formatTime(run.completed_at) : '-' }}</td>
          <td><div class="mini-actions">
            <button class="btn" :disabled="detailLoading" @click="openAgentDetail(run)">查看详情</button>
            <button v-if="run.status === 'running' && canCancelAgentRun" class="btn btn-danger" :disabled="cancelingAgentRunID !== null" @click="cancelAgentRun(run)">{{ cancelingAgentRunID === run.id ? '取消中…' : '取消' }}</button>
            <button v-if="['failed', 'canceled'].includes(run.status) && canCancelAgentRun" class="btn" :disabled="retryingAgentRunID !== null" @click="retryAgentRun(run)">{{ retryingAgentRunID === run.id ? '重试中…' : '重试' }}</button>
          </div></td>
        </tr></tbody>
      </table>
      <div v-if="loading && !agentRuns.length" class="page-loading" role="status" aria-live="polite">
        <div class="page-loading-mark" aria-hidden="true"></div>
        <div><strong>加载 Agent 运行记录</strong><p class="muted">同步工具调用与执行结果…</p></div>
      </div>
      <div v-else-if="!loading && !agentRuns.length" class="surface-empty empty jobs-empty">
        <strong>暂无 Agent 运行记录</strong>
        <span class="muted">剧本改写、提取、分镜拆解等 Agent 执行后会出现在此列表。</span>
      </div>
    </div>

    <div v-if="agentDetail" class="modal-mask" @click.self="closeAgentDetail">
      <div class="modal settings-modal settings-modal-wide agent-run-modal" role="dialog" aria-modal="true" aria-labelledby="agent-run-detail-title" @keydown.esc="closeAgentDetail">
        <div class="agent-detail-head">
          <div><h3 id="agent-run-detail-title">Agent 运行详情</h3><p>#{{ agentDetail.run.id }} · {{ agentTypeLabel(agentDetail.run.agent_type) }}</p></div>
          <span class="job-status" :class="agentDetail.run.status">{{ agentStatusLabel(agentDetail.run.status) }}</span>
        </div>
        <div v-if="agentDetail.run.last_error" class="form-error" role="alert">{{ agentDetail.run.last_error }}</div>
        <section class="agent-detail-section"><h4>输入</h4><pre>{{ agentDetail.run.input || '无输入内容' }}</pre></section>
        <section class="agent-detail-section"><h4>结构化输出</h4><p v-if="outputText(agentDetail.run.output_json)" class="agent-output-text">{{ outputText(agentDetail.run.output_json) }}</p><pre>{{ prettyJSON(agentDetail.run.output_json) || '暂无输出' }}</pre></section>
        <section class="agent-detail-section"><h4>工具调用与事件</h4>
          <div v-for="event in agentDetailEvents" :key="event.id" class="agent-event">
            <div><strong>{{ agentEventTitle(event) }}</strong><span>{{ formatTime(event.created_at) }}</span></div>
            <pre>{{ prettyJSON(event.payload_json) }}</pre>
          </div>
          <div v-if="!agentDetailEvents.length" class="empty">暂无执行事件</div>
        </section>
        <div class="modal-actions"><span v-if="detailRefreshing" class="muted agent-detail-refreshing">正在更新…</span><button type="button" class="btn" @click="closeAgentDetail">关闭</button></div>
      </div>
    </div>
  </main>
</template>
