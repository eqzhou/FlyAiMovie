<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { characterLibraryAPI, dramaAPI } from '../api'
import { authStore } from '../auth'

const templates = ref<any[]>([])
const dramas = ref<any[]>([])
const selectedDrama = ref(0)
const selectedEpisode = ref(0)
const busy = ref(false)
const message = ref('')
const form = ref({ name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' })
const showCreateModal = ref(false)
const importCandidate = ref<any | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)

const episodes = computed(() => dramas.value.find((item) => item.id === selectedDrama.value)?.episodes || [])
const canEdit = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')

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
    showCreateModal.value = false
    message.value = '角色模板已创建'
    await load()
  } finally { busy.value = false }
}

async function openCreate() {
  showCreateModal.value = true
  await nextTick()
  nameInput.value?.focus()
}

function openImport(template: any) {
  importCandidate.value = template
  selectedEpisode.value = 0
}

async function importTemplate() {
  if (!selectedDrama.value || !importCandidate.value) return
  const candidate = importCandidate.value
  busy.value = true
  try {
    await characterLibraryAPI.import(candidate.id, selectedDrama.value, selectedEpisode.value || undefined)
    message.value = `已将 ${candidate.name} 导入项目`
    importCandidate.value = null
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
      <button v-if="canEdit" class="btn btn-primary" @click="openCreate">新建角色模板</button>
    </div>

    <div class="settings-section-head character-library-head">
      <div><h2>角色模板</h2><p class="muted">{{ templates.length }} 个可复用角色</p></div>
    </div>

    <div class="panel character-library-list">
      <table v-if="templates.length" class="table character-library-table">
        <thead><tr><th>形象</th><th>名称</th><th>角色定位</th><th>音色</th><th></th></tr></thead>
        <tbody>
          <tr v-for="template in templates" :key="template.id">
            <td><img v-if="template.image_url" :src="template.image_url" :alt="template.name" class="character-library-thumb" /><span v-else class="character-library-placeholder">{{ template.name.slice(0, 1) }}</span></td>
            <td><strong>{{ template.name }}</strong><p class="muted character-library-description">{{ template.appearance || template.personality || '未填写角色设定' }}</p></td>
            <td>{{ template.role || '未设置' }}</td>
            <td>{{ template.voice_style || '未绑定' }}</td>
            <td><div v-if="canEdit" class="toolbar character-library-actions"><button class="btn btn-primary" :disabled="busy || !selectedDrama" @click="openImport(template)">导入项目</button><button class="btn btn-danger" :disabled="busy" @click="remove(template)">删除</button></div></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty character-library-empty"><strong>暂无角色模板</strong><span class="muted">创建模板后，可在不同短剧项目中复用。</span><button v-if="canEdit" class="btn btn-primary" @click="openCreate">新建角色模板</button></div>
    </div>

    <div v-if="showCreateModal" class="modal-mask" @click.self="showCreateModal = false">
      <form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="create-character-title" @keydown.esc="showCreateModal = false" @submit.prevent="create">
        <h3 id="create-character-title">新建角色模板</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field-grid">
          <div class="field"><label for="character-name">名称 <span class="required-mark">*</span></label><input id="character-name" ref="nameInput" v-model.trim="form.name" maxlength="120" required /></div>
          <div class="field"><label for="character-role">角色定位</label><input id="character-role" v-model="form.role" maxlength="120" /></div>
          <div class="field settings-span"><label for="character-appearance">外观</label><textarea id="character-appearance" v-model="form.appearance" rows="4" maxlength="4000" /></div>
          <div class="field settings-span"><label for="character-personality">性格</label><textarea id="character-personality" v-model="form.personality" rows="3" maxlength="4000" /></div>
          <div class="field"><label for="character-voice">音色</label><input id="character-voice" v-model="form.voice_style" maxlength="120" /></div>
          <div class="field"><label for="character-image">形象 URL</label><input id="character-image" v-model="form.image_url" maxlength="2000" /></div>
        </div>
        <div class="modal-actions"><button type="button" class="btn" @click="showCreateModal = false">取消</button><button type="submit" class="btn btn-primary" :disabled="busy || !form.name">创建模板</button></div>
      </form>
    </div>

    <div v-if="importCandidate" class="modal-mask" @click.self="importCandidate = null">
      <form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="import-character-title" @keydown.esc="importCandidate = null" @submit.prevent="importTemplate">
        <h3 id="import-character-title">导入角色模板</h3>
        <p class="character-import-name">{{ importCandidate.name }}</p>
        <div class="field"><label for="import-drama">项目</label><select id="import-drama" v-model.number="selectedDrama" autofocus @change="selectedEpisode=0"><option v-for="drama in dramas" :key="drama.id" :value="drama.id">{{ drama.title }}</option></select></div>
        <div class="field"><label for="import-episode">剧集（可选）</label><select id="import-episode" v-model.number="selectedEpisode"><option :value="0">仅导入项目</option><option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集 · {{ episode.title }}</option></select></div>
        <p class="muted character-import-note">导入后创建独立角色副本，模板后续修改不会影响项目。</p>
        <div class="modal-actions"><button type="button" class="btn" @click="importCandidate = null">取消</button><button type="submit" class="btn btn-primary" :disabled="busy || !selectedDrama">确认导入</button></div>
      </form>
    </div>

    <div v-if="message" class="toast">{{ message }}</div>
  </div>
</template>
