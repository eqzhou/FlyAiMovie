<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { dramaAPI } from '../api'

const router = useRouter()
const dramas = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const form = ref({ title: '', description: '', style: 'realistic', total_episodes: 1 })
const err = ref('')
const formError = ref('')
const titleInput = ref<HTMLInputElement | null>(null)
const episodeInput = ref<HTMLInputElement | null>(null)

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
  formError.value = ''
  const title = form.value.title.trim()
  if (!title) {
    formError.value = '请填写项目标题'
    await nextTick()
    titleInput.value?.focus()
    return
  }
  if (!form.value.style) {
    formError.value = '请选择项目风格'
    return
  }
  const episodeCount = Number(form.value.total_episodes)
  if (!Number.isInteger(episodeCount) || episodeCount < 1 || episodeCount > 50) {
    formError.value = '集数必须是 1 到 50 之间的整数'
    await nextTick()
    episodeInput.value?.focus()
    return
  }
  try {
    await dramaAPI.create({ ...form.value, title, total_episodes: episodeCount })
    closeCreate()
    form.value = { title: '', description: '', style: 'realistic', total_episodes: 1 }
    await load()
  } catch (e: any) {
    formError.value = e.message || '创建项目失败，请稍后重试'
  }
}

function openCreate() {
  formError.value = ''
  showCreate.value = true
  nextTick(() => titleInput.value?.focus())
}

function closeCreate() {
  formError.value = ''
  showCreate.value = false
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
      <button class="btn btn-primary" @click="openCreate">新建项目</button>
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
      <div v-if="!dramas.length" class="card empty" @click="openCreate">还没有项目，点击创建</div>
    </div>

    <div v-if="showCreate" class="modal-mask" @click.self="closeCreate">
      <form class="modal" role="dialog" aria-modal="true" aria-labelledby="create-drama-title" novalidate @submit.prevent="create">
        <h3 id="create-drama-title">新建短剧项目</h3>
        <p class="form-required-note"><span class="required-mark" aria-hidden="true">*</span> 为必填项</p>
        <div class="field"><label for="drama-title">标题 <span class="required-mark" aria-hidden="true">*</span></label><input id="drama-title" ref="titleInput" v-model="form.title" placeholder="例如：雨夜重逢" maxlength="200" required :aria-invalid="!!formError && !form.title.trim()" /></div>
        <div class="field"><label for="drama-description">简介（选填）</label><textarea id="drama-description" v-model="form.description" rows="3" maxlength="10000" /></div>
        <div class="field"><label for="drama-style">风格 <span class="required-mark" aria-hidden="true">*</span></label>
          <select id="drama-style" v-model="form.style" required>
            <option value="realistic">写实</option>
            <option value="anime">动漫</option>
            <option value="cinematic">电影感</option>
          </select>
        </div>
        <div class="field"><label for="drama-episodes">集数 <span class="required-mark" aria-hidden="true">*</span></label><input id="drama-episodes" ref="episodeInput" v-model.number="form.total_episodes" type="number" min="1" max="50" step="1" required /></div>
        <p v-if="formError" class="auth-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button class="btn" type="button" @click="closeCreate">取消</button>
          <button class="btn btn-primary" type="submit">创建</button>
        </div>
      </form>
    </div>
  </div>
</template>
