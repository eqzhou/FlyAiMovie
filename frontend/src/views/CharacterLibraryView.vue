<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { characterLibraryAPI, dramaAPI } from '../api'

const templates = ref<any[]>([])
const dramas = ref<any[]>([])
const selectedDrama = ref(0)
const selectedEpisode = ref(0)
const busy = ref(false)
const message = ref('')
const form = ref({ name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' })

const episodes = computed(() => dramas.value.find((item) => item.id === selectedDrama.value)?.episodes || [])

async function load() {
  const [library, projects] = await Promise.all([characterLibraryAPI.list(), dramaAPI.list()])
  templates.value = library
  dramas.value = projects.items || []
  if (!selectedDrama.value && dramas.value.length) selectedDrama.value = dramas.value[0].id
}

async function create() {
  if (!form.value.name.trim()) return
  busy.value = true
  try {
    await characterLibraryAPI.create(form.value)
    form.value = { name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' }
    message.value = '角色模板已创建'
    await load()
  } finally { busy.value = false }
}

async function importTemplate(template: any) {
  if (!selectedDrama.value) return
  busy.value = true
  try {
    await characterLibraryAPI.import(template.id, selectedDrama.value, selectedEpisode.value || undefined)
    message.value = `已将 ${template.name} 导入项目`
  } finally { busy.value = false }
}

async function remove(template: any) {
  if (!confirm(`删除角色模板「${template.name}」？`)) return
  await characterLibraryAPI.del(template.id)
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div><h1 class="page-title">角色库</h1><p class="page-desc">跨项目复用角色设定、形象和音色</p></div>
    </div>

    <div class="row">
      <section class="col panel">
        <h3 style="margin-top:0">新建角色模板</h3>
        <div class="field"><label>名称</label><input v-model.trim="form.name" maxlength="120" /></div>
        <div class="field"><label>角色定位</label><input v-model="form.role" maxlength="120" /></div>
        <div class="field"><label>外观</label><textarea v-model="form.appearance" rows="4" maxlength="4000" /></div>
        <div class="field"><label>性格</label><textarea v-model="form.personality" rows="3" maxlength="4000" /></div>
        <div class="field"><label>音色</label><input v-model="form.voice_style" maxlength="120" /></div>
        <div class="field"><label>形象 URL</label><input v-model="form.image_url" maxlength="2000" /></div>
        <button class="btn btn-primary" :disabled="busy || !form.name" @click="create">创建模板</button>
      </section>

      <section class="col panel">
        <h3 style="margin-top:0">导入目标</h3>
        <div class="field"><label>项目</label><select v-model.number="selectedDrama" @change="selectedEpisode=0"><option v-for="drama in dramas" :key="drama.id" :value="drama.id">{{ drama.title }}</option></select></div>
        <div class="field"><label>剧集（可选）</label><select v-model.number="selectedEpisode"><option :value="0">仅导入项目</option><option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集 · {{ episode.title }}</option></select></div>
        <p class="muted">导入后创建独立角色副本，模板后续修改不会影响项目。</p>
      </section>
    </div>

    <div class="grid" style="margin-top:16px">
      <article v-for="template in templates" :key="template.id" class="card project-card">
        <img v-if="template.image_url" :src="template.image_url" :alt="template.name" style="width:100%;aspect-ratio:16/9;object-fit:cover" />
        <h3>{{ template.name }}</h3>
        <p class="muted">{{ template.role || template.appearance || '未填写角色设定' }}</p>
        <div class="toolbar">
          <button class="btn btn-primary" :disabled="busy || !selectedDrama" @click="importTemplate(template)">导入项目</button>
          <button class="btn btn-danger" :disabled="busy" @click="remove(template)">删除</button>
        </div>
      </article>
      <div v-if="!templates.length" class="card empty">暂无角色模板</div>
    </div>
    <div v-if="message" class="toast">{{ message }}</div>
  </div>
</template>
