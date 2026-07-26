<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { FolderInput, Pencil, Plus, Trash2 } from 'lucide-vue-next'
import { characterLibraryAPI, dramaAPI } from '../api'
import { authStore } from '../auth'
import { confirmAction } from '../composables/useConfirm'

const templates = ref<any[]>([])
const dramas = ref<any[]>([])
const selectedDrama = ref(0)
const selectedEpisode = ref(0)
const busy = ref(false)
const loading = ref(true)
const loadError = ref('')
const formError = ref('')
const importError = ref('')
const message = ref('')
const query = ref('')
const form = ref({ name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' })
const showCreateModal = ref(false)
const editingTemplateID = ref<number | null>(null)
const importCandidate = ref<any | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
let messageTimer: number | null = null

function notify(text: string) {
  message.value = text
  if (messageTimer) window.clearTimeout(messageTimer)
  messageTimer = window.setTimeout(() => {
    if (message.value === text) message.value = ''
    messageTimer = null
  }, 2600)
}

const episodes = computed(() => dramas.value.find((item) => item.id === selectedDrama.value)?.episodes || [])
const canEdit = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const filteredTemplates = computed(() => {
  const keyword = query.value.trim().toLocaleLowerCase('zh-CN')
  if (!keyword) return templates.value
  return templates.value.filter((template) => [template.name, template.role, template.appearance, template.personality, template.voice_style]
    .some((value) => String(value || '').toLocaleLowerCase('zh-CN').includes(keyword)))
})

async function load() {
  loading.value = true
  loadError.value = ''
  const [library, projects] = await Promise.allSettled([characterLibraryAPI.list(), dramaAPI.list()])
  const failures: string[] = []
  if (library.status === 'fulfilled') templates.value = library.value
  else failures.push(library.reason instanceof Error ? library.reason.message : '角色模板加载失败')
  if (projects.status === 'fulfilled') dramas.value = projects.value.items || []
  else failures.push(projects.reason instanceof Error ? projects.reason.message : '项目列表加载失败')
  loadError.value = failures.join('；')
  if (!selectedDrama.value && dramas.value.length) selectedDrama.value = dramas.value[0].id
  loading.value = false
}

async function create() {
  formError.value = ''
  if (!form.value.name.trim()) {
    formError.value = '请填写角色名称'
    nameInput.value?.focus()
    return
  }
  busy.value = true
  try {
    const isEditing = editingTemplateID.value !== null
    const payload = { ...form.value, name: form.value.name.trim() }
    if (editingTemplateID.value) await characterLibraryAPI.update(editingTemplateID.value, payload)
    else await characterLibraryAPI.create(payload)
    form.value = { name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' }
    editingTemplateID.value = null
    showCreateModal.value = false
    notify(isEditing ? '角色模板已更新' : '角色模板已创建')
    await load()
  } catch (reason) {
    formError.value = reason instanceof Error ? reason.message : '模板保存失败'
  } finally { busy.value = false }
}

async function openCreate() {
  formError.value = ''
  editingTemplateID.value = null
  form.value = { name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' }
  showCreateModal.value = true
  await nextTick()
  nameInput.value?.focus()
}

async function openEdit(template: any) {
  formError.value = ''
  editingTemplateID.value = template.id
  form.value = {
    name: template.name || '',
    role: template.role || '',
    appearance: template.appearance || '',
    personality: template.personality || '',
    voice_style: template.voice_style || '',
    image_url: template.image_url || '',
  }
  showCreateModal.value = true
  await nextTick()
  nameInput.value?.focus()
}

function closeEditor() {
  formError.value = ''
  showCreateModal.value = false
  editingTemplateID.value = null
  form.value = { name: '', role: '', appearance: '', personality: '', voice_style: '', image_url: '' }
}

function openImport(template: any) {
  importError.value = ''
  importCandidate.value = template
  selectedEpisode.value = 0
}

async function importTemplate() {
  if (!selectedDrama.value || !importCandidate.value) return
  const candidate = importCandidate.value
  busy.value = true
  try {
    await characterLibraryAPI.import(candidate.id, selectedDrama.value, selectedEpisode.value || undefined)
    notify(`已将 ${candidate.name} 导入项目`)
    importCandidate.value = null
  } catch (reason) {
    importError.value = reason instanceof Error ? reason.message : '角色导入失败'
  } finally { busy.value = false }
}

async function remove(template: any) {
  if (!await confirmAction({
    title: '删除角色模板',
    message: `确定删除角色模板「${template.name}」？`,
    detail: '已导入到项目中的角色不受影响。',
    confirmText: '删除模板',
    tone: 'danger',
  })) return
  try {
    await characterLibraryAPI.del(template.id)
    await load()
  } catch (reason) {
    loadError.value = reason instanceof Error ? reason.message : '角色模板删除失败'
  }
}

onMounted(load)
onUnmounted(() => { if (messageTimer) window.clearTimeout(messageTimer) })
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div><h1 class="page-title">角色库</h1><p class="page-desc">跨项目复用角色设定、形象和音色</p></div>
      <button v-if="canEdit" class="btn btn-primary" @click="openCreate"><Plus :size="16" aria-hidden="true" />新建角色模板</button>
    </div>

    <div class="settings-section-head character-library-head">
      <div><h2>角色模板</h2><p class="muted">{{ templates.length }} 个可复用角色</p></div>
      <label class="library-search"><span class="sr-only">搜索角色模板</span><input v-model="query" type="search" aria-label="搜索角色模板" placeholder="搜索名称、定位或音色" /></label>
    </div>

    <div v-if="loadError" class="inline-alert" role="alert"><div><strong>角色库加载不完整</strong><span>{{ loadError }}</span></div><button class="btn" type="button" @click="load">重试加载</button></div>

    <div v-if="loading" class="panel character-library-list">
      <div class="page-loading" role="status" aria-live="polite">
        <div class="page-loading-mark" aria-hidden="true"></div>
        <div>
          <strong>正在加载角色模板</strong>
          <p class="muted">同步跨项目角色设定、形象与音色…</p>
        </div>
      </div>
    </div>
    <div v-else class="panel character-library-list">
      <table v-if="filteredTemplates.length" class="table character-library-table">
        <thead><tr><th>形象</th><th>名称</th><th>角色定位</th><th>音色</th><th></th></tr></thead>
        <tbody>
          <tr v-for="template in filteredTemplates" :key="template.id">
            <td><img v-if="template.image_url" :src="template.image_url" :alt="template.name" class="character-library-thumb" /><span v-else class="character-library-placeholder">{{ template.name.slice(0, 1) }}</span></td>
            <td><strong>{{ template.name }}</strong><p class="muted character-library-description">{{ template.appearance || template.personality || '未填写角色设定' }}</p></td>
            <td>{{ template.role || '未设置' }}</td>
            <td>{{ template.voice_style || '未绑定' }}</td>
            <td><div v-if="canEdit" class="toolbar character-library-actions"><button class="btn" :disabled="busy" @click="openEdit(template)"><Pencil :size="14" aria-hidden="true" />编辑</button><button class="btn btn-primary" :disabled="busy || !selectedDrama" @click="openImport(template)"><FolderInput :size="14" aria-hidden="true" />导入项目</button><button class="btn btn-danger" :disabled="busy" :aria-label="`删除角色模板 ${template.name}`" title="删除角色模板" @click="remove(template)"><Trash2 :size="14" aria-hidden="true" /></button></div></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty character-library-empty"><strong>{{ templates.length ? '没有匹配的角色模板' : '暂无角色模板' }}</strong><span class="muted">{{ templates.length ? '尝试更换关键词。' : '创建模板后，可在不同短剧项目中复用。' }}</span><button v-if="canEdit && !templates.length" class="btn btn-primary" @click="openCreate"><Plus :size="16" aria-hidden="true" />新建角色模板</button></div>
    </div>

    <div v-if="showCreateModal" class="modal-mask" @click.self="closeEditor">
      <form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="create-character-title" @keydown.esc="closeEditor" @submit.prevent="create">
        <h3 id="create-character-title">{{ editingTemplateID ? '编辑角色模板' : '新建角色模板' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field-grid">
          <div class="field"><label for="character-name">名称 <span class="required-mark">*</span></label><input id="character-name" ref="nameInput" v-model.trim="form.name" maxlength="120" required /></div>
          <div class="field"><label for="character-role">角色定位</label><input id="character-role" v-model="form.role" maxlength="120" /></div>
          <div class="field settings-span"><label for="character-appearance">外观</label><textarea id="character-appearance" v-model="form.appearance" rows="4" maxlength="4000" /></div>
          <div class="field settings-span"><label for="character-personality">性格</label><textarea id="character-personality" v-model="form.personality" rows="3" maxlength="4000" /></div>
          <div class="field"><label for="character-voice">音色</label><input id="character-voice" v-model="form.voice_style" maxlength="120" /></div>
          <div class="field"><label for="character-image">形象 URL</label><input id="character-image" v-model="form.image_url" maxlength="2000" /></div>
        </div>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closeEditor">取消</button><button type="submit" class="btn btn-primary" :disabled="busy || !form.name">{{ editingTemplateID ? '保存修改' : '创建模板' }}</button></div>
      </form>
    </div>

    <div v-if="importCandidate" class="modal-mask" @click.self="importCandidate = null">
      <form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="import-character-title" @keydown.esc="importCandidate = null" @submit.prevent="importTemplate">
        <h3 id="import-character-title">导入角色模板</h3>
        <p class="character-import-name">{{ importCandidate.name }}</p>
        <div class="field"><label for="import-drama">项目</label><select id="import-drama" v-model.number="selectedDrama" autofocus @change="selectedEpisode=0"><option v-for="drama in dramas" :key="drama.id" :value="drama.id">{{ drama.title }}</option></select></div>
        <div class="field"><label for="import-episode">剧集（可选）</label><select id="import-episode" v-model.number="selectedEpisode"><option :value="0">仅导入项目</option><option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集 · {{ episode.title }}</option></select></div>
        <p class="muted character-import-note">导入后创建独立角色副本，模板后续修改不会影响项目。</p>
        <p v-if="importError" class="form-error" role="alert">{{ importError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="importCandidate = null">取消</button><button type="submit" class="btn btn-primary" :disabled="busy || !selectedDrama">确认导入</button></div>
      </form>
    </div>

    <div v-if="message" class="toast" role="status">{{ message }}</div>
  </div>
</template>
