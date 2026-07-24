<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { auditAPI } from '../api'

const rows = ref<any[]>([])
const page = ref(1)
const pageSize = 50
const total = ref(0)
const resourceType = ref('')
const loading = ref(false)
const error = ref('')
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

async function load(nextPage = page.value) {
  loading.value = true
  error.value = ''
  try {
    const result = await auditAPI.list({ page: nextPage, page_size: pageSize, resource_type: resourceType.value || undefined })
    rows.value = result.items
    total.value = result.pagination.total
    page.value = result.pagination.page
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

onMounted(() => load(1))
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">审计日志</h1>
        <p class="page-desc">组织内写操作记录</p>
      </div>
      <button class="btn" :disabled="loading" @click="load()">刷新</button>
    </div>

    <div class="audit-toolbar">
      <select v-model="resourceType" aria-label="资源类型" @change="load(1)">
        <option value="">全部资源</option>
        <option value="dramas">项目</option>
        <option value="episodes">剧集</option>
        <option value="storyboards">分镜</option>
        <option value="assets">素材</option>
        <option value="ai-configs">AI 配置</option>
        <option value="agent-configs">Agent 配置</option>
        <option value="jobs">任务</option>
      </select>
      <span class="muted">共 {{ total }} 条</span>
    </div>

    <div v-if="error" class="inline-alert" role="alert">
      <div><strong>审计日志加载失败</strong><span>{{ error }}</span></div>
      <button class="btn" type="button" @click="load()">重试</button>
    </div>

    <div class="panel audit-table-wrap">
      <table v-if="rows.length" class="table audit-table">
        <thead><tr><th>时间</th><th>成员</th><th>动作</th><th>资源</th><th>结果</th><th>来源 IP</th></tr></thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <td>{{ formatTime(row.created_at) }}</td>
            <td>#{{ row.user_id }} · {{ row.role }}</td>
            <td>{{ row.method }}</td>
            <td>{{ row.resource_type }}<span v-if="row.resource_id"> #{{ row.resource_id }}</span></td>
            <td><span :class="['audit-status', row.status_code < 400 ? 'ok' : 'fail']">{{ row.status_code }}</span></td>
            <td>{{ row.source_ip || '-' }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="loading && !rows.length" class="page-loading" role="status" aria-live="polite">
        <div class="page-loading-mark" aria-hidden="true"></div>
        <div>
          <strong>加载审计记录</strong>
          <p class="muted" style="margin:6px 0 0">同步组织内写操作与结果状态…</p>
        </div>
      </div>
      <div v-else-if="!loading && !error && !rows.length" class="empty surface-empty">
        <strong>暂无审计记录</strong>
        <span class="muted">创建项目、修改配置或执行生成后，写操作会记录在这里。</span>
      </div>
    </div>

    <div class="audit-pagination">
      <button class="btn" :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button>
      <span class="muted">{{ page }} / {{ pages }}</span>
      <button class="btn" :disabled="page >= pages || loading" @click="load(page + 1)">下一页</button>
    </div>
  </main>
</template>
