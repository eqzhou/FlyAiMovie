<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { dramaAPI } from '../api'

const router = useRouter()
const dramas = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const form = ref({ title: '', description: '', style: 'realistic', total_episodes: 1 })
const err = ref('')

async function load() {
  loading.value = true
  try {
    const data = await dramaAPI.list()
    dramas.value = data.items || []
  } catch (e: any) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!form.value.title.trim()) return
  await dramaAPI.create(form.value)
  showCreate.value = false
  form.value = { title: '', description: '', style: 'realistic', total_episodes: 1 }
  await load()
}

async function delDrama(d: any) {
  if (!confirm(`删除项目「${d.title}」？`)) return
  await dramaAPI.del(d.id)
  await load()
}

function fmtDate(s?: string) {
  if (!s) return ''
  return new Date(s).toLocaleString()
}

function progress(d: any) {
  const eps = d.episodes?.length || 0
  const chars = d.characters?.length || 0
  const scenes = d.scenes?.length || 0
  return Math.min(100, eps * 20 + chars * 5 + scenes * 5)
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">短剧项目</h1>
        <p class="page-desc">{{ dramas.length }} 个项目 · 一句话到成片</p>
      </div>
      <button class="btn btn-primary" @click="showCreate = true">新建项目</button>
    </div>

    <p v-if="err" class="muted">{{ err }}</p>
    <div v-if="loading" class="muted">加载中…</div>
    <div v-else class="grid">
      <div
        v-for="d in dramas"
        :key="d.id"
        class="card project-card"
        @click="router.push(`/drama/${d.id}`)"
      >
        <div>
          <div class="row" style="justify-content:space-between">
            <span class="badge">{{ d.episodes?.length || 0 }} 集</span>
            <button class="btn btn-ghost" @click.stop="delDrama(d)">删除</button>
          </div>
          <h3 class="project-title">{{ d.title }}</h3>
          <div class="project-meta">
            <span v-if="d.style" class="style-tag">{{ d.style }}</span>
            <span>角色 {{ d.characters?.length || 0 }}</span>
            <span>场景 {{ d.scenes?.length || 0 }}</span>
          </div>
        </div>
        <div class="card-footer">
          <div class="progress-mini-track"><div class="progress-mini-fill" :style="{ width: progress(d) + '%' }"></div></div>
          <span>{{ fmtDate(d.updated_at) }}</span>
        </div>
      </div>
      <div v-if="!dramas.length" class="card empty" @click="showCreate = true">还没有项目，点击创建</div>
    </div>

    <div v-if="showCreate" class="modal-mask" @click.self="showCreate = false">
      <div class="modal">
        <h3>新建短剧项目</h3>
        <div class="field"><label>标题</label><input v-model="form.title" placeholder="例如：雨夜重逢" /></div>
        <div class="field"><label>简介</label><textarea v-model="form.description" rows="3" /></div>
        <div class="field"><label>风格</label>
          <select v-model="form.style">
            <option value="realistic">写实</option>
            <option value="anime">动漫</option>
            <option value="cinematic">电影感</option>
          </select>
        </div>
        <div class="field"><label>集数</label><input v-model.number="form.total_episodes" type="number" min="1" max="50" /></div>
        <div class="modal-actions">
          <button class="btn" @click="showCreate = false">取消</button>
          <button class="btn btn-primary" @click="create">创建</button>
        </div>
      </div>
    </div>
  </div>
</template>
