<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { jobsAPI } from '../api'

const jobs = ref<any[]>([])
const selected = ref<number[]>([])
const events = ref<Record<number, any[]>>({})
const expanded = ref<number | null>(null)
const status = ref('')
const kind = ref('')
const loading = ref(false)
const error = ref('')
let timer: number | null = null

const active = computed(() => jobs.value.some(job => !['succeeded', 'failed', 'canceled'].includes(job.status)))
const cancellableSelected = computed(() => selected.value.filter(id => {
  const job = jobs.value.find(row => row.id === id)
  return job && !['succeeded', 'failed', 'canceled'].includes(job.status)
}))

async function load() {
  loading.value = true
  error.value = ''
  try {
    jobs.value = await jobsAPI.list({ status: status.value || undefined, kind: kind.value || undefined, limit: 100 })
    selected.value = selected.value.filter(id => jobs.value.some(row => row.id === id))
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载任务失败'
  } finally {
    loading.value = false
  }
}

async function toggleEvents(job: any) {
  if (expanded.value === job.id) {
    expanded.value = null
    return
  }
  events.value = { ...events.value, [job.id]: await jobsAPI.events(job.id) }
  expanded.value = job.id
}

async function cancel(job: any) {
  await jobsAPI.cancel(job.id)
  await load()
}

async function retry(job: any) {
  await jobsAPI.retry(job.id)
  await load()
}

async function batchCancel() {
  if (!cancellableSelected.value.length) return
  await jobsAPI.batchCancel(cancellableSelected.value)
  selected.value = []
  await load()
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

onMounted(async () => {
  await load()
  timer = window.setInterval(() => { if (active.value && document.visibilityState === 'visible') load() }, 4000)
})
onUnmounted(() => { if (timer) window.clearInterval(timer) })
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div><h1 class="page-title">任务中心</h1><p class="page-desc">生成、合成与导出任务</p></div>
      <button class="btn" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="audit-toolbar">
      <select v-model="status" aria-label="任务状态" @change="load">
        <option value="">全部状态</option><option value="queued">排队</option><option value="running">运行中</option>
        <option value="waiting_provider">等待厂商</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="canceled">已取消</option>
      </select>
      <input v-model.trim="kind" aria-label="任务类型" placeholder="按任务类型过滤" @keyup.enter="load" />
      <button class="btn btn-danger" :disabled="!cancellableSelected.length" @click="batchCancel">批量取消</button>
    </div>
    <div v-if="error" class="auth-error" role="alert">{{ error }}</div>
    <div class="panel audit-table-wrap">
      <table class="table jobs-table">
        <thead><tr><th></th><th>任务</th><th>状态</th><th>进度</th><th>厂商</th><th>尝试</th><th>更新时间</th><th>操作</th></tr></thead>
        <tbody>
          <template v-for="job in jobs" :key="job.id">
            <tr>
              <td><input v-model="selected" type="checkbox" :value="job.id" :aria-label="`选择任务 ${job.id}`" /></td>
              <td>#{{ job.id }}<br><span class="muted">{{ job.kind }}</span></td>
              <td>{{ job.status }}<div v-if="job.last_error" class="job-error">{{ job.last_error }}</div></td>
              <td><div class="progress-bar"><i :style="{ width: `${job.progress || 0}%` }"></i></div><span class="muted">{{ job.progress || 0 }}%</span></td>
              <td>{{ job.provider || '-' }}<br><span class="muted">{{ job.provider_task_id || '' }}</span></td>
              <td>{{ job.attempt }} / {{ job.max_attempts }}</td>
              <td>{{ formatTime(job.updated_at) }}</td>
              <td><div class="mini-actions">
                <button class="btn" @click="toggleEvents(job)">{{ expanded === job.id ? '收起日志' : '查看日志' }}</button>
                <button v-if="!['succeeded','failed','canceled'].includes(job.status)" class="btn btn-danger" @click="cancel(job)">取消</button>
                <button v-if="['failed','canceled'].includes(job.status)" class="btn" :disabled="job.attempt >= job.max_attempts" @click="retry(job)">重试</button>
              </div></td>
            </tr>
            <tr v-if="expanded === job.id"><td colspan="8"><div class="job-events">
              <div v-for="event in events[job.id] || []" :key="event.id"><span class="muted">{{ formatTime(event.created_at) }}</span> · {{ event.stage }} · {{ event.message }}</div>
              <div v-if="!(events[job.id] || []).length" class="muted">暂无阶段日志</div>
            </div></td></tr>
          </template>
        </tbody>
      </table>
      <div v-if="!loading && !jobs.length" class="empty">暂无任务</div>
    </div>
  </main>
</template>
