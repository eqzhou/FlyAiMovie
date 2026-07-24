<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { assetAPI, dramaAPI, episodeAPI, uploadAPI } from '../api'
import { safeMediaHref } from '../utils/mediaUrl'

const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const assets = ref<any[]>([])
const storyboards = ref<any[]>([])
const projectLoading = ref(true)
const projectError = ref('')
const loading = ref(false)
const busy = ref('')
const message = ref('')
const typeFilter = ref('')
const categoryFilter = ref('')
const episodeFilter = ref(0)
const applyEpisodeId = ref(0)
const applyStoryboardId = ref(0)
const applyFrameType = ref('first_frame')
const editing = ref<any | null>(null)
const editForm = ref({ name: '', description: '', category: '' })
const showUpload = ref(false)
const uploadKind = ref<'image' | 'media'>('image')
const uploadFile = ref<File | null>(null)
const uploadStoryboards = ref<any[]>([])
const uploadForm = ref({ target: 'project', target_id: 0, episode_id: 0, name: '', category: 'reference' })
let projectRequest = 0
let assetRequest = 0
let storyboardRequest = 0
let uploadStoryboardRequest = 0

const dramaId = computed(() => Number(route.params.id))
const episodes = computed<any[]>(() => drama.value?.episodes || [])
const uploadTargets = computed<any[]>(() => {
  if (uploadForm.value.target === 'episode') return episodes.value
  if (uploadForm.value.target === 'character') return drama.value?.characters || []
  if (uploadForm.value.target === 'scene') return drama.value?.scenes || []
  if (uploadForm.value.target === 'prop') return drama.value?.props || []
  if (uploadForm.value.target === 'storyboard') return uploadStoryboards.value
  return []
})
const categories = computed(() => {
  const values = new Set(assets.value.map((item) => item.category).filter(Boolean))
  return Array.from(values).sort()
})

function notify(text: string) {
  message.value = text
  window.setTimeout(() => {
    if (message.value === text) message.value = ''
  }, 2600)
}

async function loadDrama() {
	const request = ++projectRequest
	const result = await dramaAPI.get(dramaId.value)
	if (request !== projectRequest) return
	drama.value = result
	if (!episodes.value.some((episode) => episode.id === applyEpisodeId.value)) {
		applyEpisodeId.value = episodes.value[0]?.id || 0
	}
}

async function loadAssets() {
	const request = ++assetRequest
	loading.value = true
	try {
		const result = await assetAPI.list({
			drama_id: dramaId.value,
      episode_id: episodeFilter.value || undefined,
      type: typeFilter.value || undefined,
			category: categoryFilter.value || undefined,
		})
		if (request === assetRequest) assets.value = result
	} catch (error: any) {
		if (request === assetRequest) notify(error.message)
	} finally {
		if (request === assetRequest) loading.value = false
	}
}

async function loadStoryboards() {
	const request = ++storyboardRequest
	applyStoryboardId.value = 0
	if (!applyEpisodeId.value) {
		if (request === storyboardRequest) storyboards.value = []
		return
	}
	try {
		const result = await episodeAPI.storyboards(applyEpisodeId.value)
		if (request === storyboardRequest) storyboards.value = result
	} catch {
		if (request === storyboardRequest) storyboards.value = []
	}
}

function openUpload(kind: 'image' | 'media' = 'image') {
  uploadKind.value = kind
  uploadFile.value = null
  uploadForm.value = {
    target: 'project',
    target_id: 0,
    episode_id: episodes.value[0]?.id || 0,
    name: '',
    category: 'reference',
  }
  showUpload.value = true
  loadUploadStoryboards()
}

function chooseUploadFile(event: Event) {
  const input = event.target as HTMLInputElement
  uploadFile.value = input.files?.[0] || null
  if (uploadFile.value && !uploadForm.value.name) uploadForm.value.name = uploadFile.value.name
}

async function loadUploadStoryboards() {
	const request = ++uploadStoryboardRequest
	uploadForm.value.target_id = 0
	if (!uploadForm.value.episode_id) {
		if (request === uploadStoryboardRequest) uploadStoryboards.value = []
		return
	}
	try {
		const result = await episodeAPI.storyboards(uploadForm.value.episode_id)
		if (request === uploadStoryboardRequest) uploadStoryboards.value = result
	} catch {
		if (request === uploadStoryboardRequest) uploadStoryboards.value = []
	}
}

async function uploadImage() {
  const file = uploadFile.value
  if (!file) {
    notify('请选择图片文件')
    return
  }
  if (uploadForm.value.target !== 'project' && !uploadForm.value.target_id) {
    notify('请选择绑定目标')
    return
  }
  busy.value = 'upload'
  try {
    const fields: Record<string, string | number> = {
      drama_id: dramaId.value,
      name: uploadForm.value.name.trim() || file.name,
      category: uploadForm.value.category.trim() || 'reference',
    }
    const target = uploadForm.value.target
    if (target === 'episode') fields.episode_id = uploadForm.value.target_id
    if (target === 'character') fields.character_id = uploadForm.value.target_id
    if (target === 'scene') fields.scene_id = uploadForm.value.target_id
    if (target === 'prop') fields.prop_id = uploadForm.value.target_id
    if (target === 'storyboard') {
      fields.episode_id = uploadForm.value.episode_id
      fields.storyboard_id = uploadForm.value.target_id
    }
    if (uploadKind.value === 'image') await uploadAPI.image(file, fields)
    else await uploadAPI.media(file, fields)
    showUpload.value = false
    notify('素材已加入素材库')
    await loadDrama()
    await loadAssets()
  } catch (error: any) {
    notify(error.message)
  } finally {
    busy.value = ''
  }
}

async function toggleFavorite(asset: any) {
  busy.value = `favorite-${asset.id}`
  try {
    await assetAPI.update(asset.id, { is_favorite: !asset.is_favorite })
    await loadAssets()
  } catch (error: any) {
    notify(error.message)
  } finally {
    busy.value = ''
  }
}

function openEdit(asset: any) {
  editing.value = asset
  editForm.value = {
    name: asset.name || '',
    description: asset.description || '',
    category: asset.category || '',
  }
}

async function saveEdit() {
  if (!editing.value || !editForm.value.name.trim()) return
  busy.value = `edit-${editing.value.id}`
  try {
    await assetAPI.update(editing.value.id, {
      name: editForm.value.name.trim(),
      description: editForm.value.description.trim(),
      category: editForm.value.category.trim(),
    })
    editing.value = null
    notify('素材信息已更新')
    await loadAssets()
  } catch (error: any) {
    notify(error.message)
  } finally {
    busy.value = ''
  }
}

async function removeAsset(asset: any) {
  if (!window.confirm(`删除素材“${asset.name}”？`)) return
  busy.value = `delete-${asset.id}`
  try {
    await assetAPI.del(asset.id)
    notify('素材已删除')
    await loadAssets()
  } catch (error: any) {
    notify(error.message)
  } finally {
    busy.value = ''
  }
}

async function applyAsset(asset: any) {
  if (!applyStoryboardId.value) {
    notify('请先选择目标分镜')
    return
  }
  busy.value = `apply-${asset.id}`
  try {
    await assetAPI.apply(asset.id, {
      storyboard_id: applyStoryboardId.value,
      frame_type: applyFrameType.value,
    })
    notify('图片已写入目标分镜')
  } catch (error: any) {
    notify(error.message)
  } finally {
    busy.value = ''
  }
}

watch([typeFilter, categoryFilter, episodeFilter], loadAssets)
watch(applyEpisodeId, loadStoryboards)
watch(() => uploadForm.value.episode_id, loadUploadStoryboards)
watch(() => uploadForm.value.target, () => { uploadForm.value.target_id = 0 })
async function loadProject() {
	const request = ++projectRequest
	projectLoading.value = true
	projectError.value = ''
	episodeFilter.value = 0
	applyEpisodeId.value = 0
	storyboards.value = []
	assets.value = []
	drama.value = null
	try {
		const result = await dramaAPI.get(dramaId.value)
		if (request !== projectRequest) return
		drama.value = result
		if (!episodes.value.some((episode) => episode.id === applyEpisodeId.value)) {
			applyEpisodeId.value = episodes.value[0]?.id || 0
		}
		await Promise.all([loadAssets(), loadStoryboards()])
	} catch (error: any) {
		if (request !== projectRequest) return
		projectError.value = error?.message || '项目加载失败'
		drama.value = null
	} finally {
		if (request === projectRequest) projectLoading.value = false
	}
}
watch(dramaId, loadProject)
onMounted(loadProject)
</script>

<template>
  <div v-if="projectLoading" class="page">
    <div class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>正在打开素材库</strong>
        <p class="muted" style="margin:6px 0 0">同步项目素材、分类与分镜绑定…</p>
      </div>
    </div>
  </div>
  <div v-else-if="projectError" class="page">
    <div class="inline-alert" role="alert">
      <div><strong>素材库打开失败</strong><span>{{ projectError }}</span></div>
      <button class="btn" type="button" @click="loadProject">重试</button>
    </div>
  </div>
  <div class="page" v-else-if="drama">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ drama.title }} · 素材库</h1>
        <p class="page-desc">集中管理项目中的图片、视频和音频，并复用到分镜。</p>
      </div>
      <div class="row">
        <button class="btn" @click="router.push(`/drama/${dramaId}`)">返回项目</button>
        <button class="btn" :disabled="!!busy" @click="openUpload('media')">上传视频/音频</button>
        <button class="btn btn-primary" :disabled="!!busy" @click="openUpload('image')">上传图片</button>
      </div>
    </div>

    <div class="asset-toolbar">
      <div class="seg" aria-label="素材类型">
        <button :class="{ active: typeFilter === '' }" @click="typeFilter = ''">全部</button>
        <button :class="{ active: typeFilter === 'image' }" @click="typeFilter = 'image'">图片</button>
        <button :class="{ active: typeFilter === 'video' }" @click="typeFilter = 'video'">视频</button>
        <button :class="{ active: typeFilter === 'audio' }" @click="typeFilter = 'audio'">音频</button>
      </div>
      <select v-model.number="episodeFilter" aria-label="按剧集筛选">
        <option :value="0">全部剧集</option>
        <option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集</option>
      </select>
      <select v-model="categoryFilter" aria-label="按分类筛选">
        <option value="">全部分类</option>
        <option v-for="category in categories" :key="category" :value="category">{{ category }}</option>
      </select>
      <span class="muted asset-count">{{ loading ? '加载中' : `${assets.length} 项素材` }}</span>
    </div>

    <div class="asset-apply-bar">
      <strong>复用目标</strong>
      <select v-model.number="applyEpisodeId" aria-label="目标剧集">
        <option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集</option>
      </select>
      <select v-model.number="applyStoryboardId" aria-label="目标分镜">
        <option :value="0">选择分镜</option>
        <option v-for="shot in storyboards" :key="shot.id" :value="shot.id">#{{ shot.storyboard_number }} {{ shot.title || '镜头' }}</option>
      </select>
      <select v-model="applyFrameType" aria-label="目标帧">
        <option value="first_frame">首帧</option>
        <option value="last_frame">尾帧</option>
        <option value="composed">分镜板</option>
      </select>
    </div>

    <div v-if="assets.length" class="asset-grid">
      <article v-for="asset in assets" :key="asset.id" class="asset-card">
        <div class="asset-preview">
          <img v-if="asset.type === 'image' || asset.mime_type?.startsWith('image')" :src="asset.thumbnail_url || asset.url" :alt="asset.name" />
          <video v-else-if="asset.type === 'video' || asset.mime_type?.startsWith('video')" :src="asset.url" controls preload="metadata" />
          <audio v-else-if="asset.type === 'audio' || asset.mime_type?.startsWith('audio')" :src="asset.url" controls preload="metadata" />
          <a v-else-if="safeMediaHref(asset.url)" :href="safeMediaHref(asset.url)" target="_blank" rel="noopener noreferrer">打开素材</a>
          <span v-else class="muted">素材地址无效</span>
        </div>
        <div class="asset-card-body">
          <div class="asset-title-row">
            <div>
              <h3>{{ asset.name }}</h3>
              <span class="muted">{{ asset.type }} · {{ asset.category || '未分类' }}<template v-if="asset.duration_seconds"> · {{ asset.duration_seconds.toFixed(2) }} 秒</template></span>
              <span v-if="asset.width && asset.height" class="muted">{{ asset.width }}×{{ asset.height }}<template v-if="asset.frame_rate"> · {{ asset.frame_rate.toFixed(2) }} fps</template><template v-if="asset.codec"> · {{ asset.codec }}</template></span>
              <span v-if="asset.probe_status === 'failed'" class="muted">元数据解析失败</span>
            </div>
            <button class="icon-btn" :title="asset.is_favorite ? '取消收藏' : '收藏'" :aria-label="asset.is_favorite ? '取消收藏' : '收藏'" :disabled="!!busy" @click="toggleFavorite(asset)">
              {{ asset.is_favorite ? '★' : '☆' }}
            </button>
          </div>
          <p v-if="asset.description" class="asset-description">{{ asset.description }}</p>
          <div class="asset-actions">
            <button v-if="asset.type === 'image' || asset.mime_type?.startsWith('image')" class="btn btn-primary" :disabled="!!busy" @click="applyAsset(asset)">复用到分镜</button>
            <button class="btn" :disabled="!!busy" @click="openEdit(asset)">编辑</button>
            <a v-if="safeMediaHref(asset.url)" class="btn" :href="safeMediaHref(asset.url)" target="_blank" rel="noopener noreferrer">打开</a>
            <button class="btn btn-danger" :disabled="!!busy" @click="removeAsset(asset)">删除</button>
          </div>
        </div>
      </article>
    </div>
    <div v-else-if="loading" class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>加载素材中</strong>
        <p class="muted" style="margin:6px 0 0">按类型、剧集与分类同步素材列表…</p>
      </div>
    </div>
    <div v-else class="empty surface-empty">
      <strong>当前筛选条件下暂无素材</strong>
      <span class="muted">试试切换类型/剧集，或上传图片、视频、音频到项目素材库。</span>
    </div>

    <div v-if="editing" class="modal-mask" @click.self="editing = null">
      <div class="modal">
        <h3>编辑素材</h3>
        <div class="field"><label>名称</label><input v-model="editForm.name" maxlength="120" /></div>
        <div class="field"><label>分类</label><input v-model="editForm.category" maxlength="60" /></div>
        <div class="field"><label>描述</label><textarea v-model="editForm.description" rows="4" maxlength="1000" /></div>
        <div class="modal-actions">
          <button class="btn" @click="editing = null">取消</button>
          <button class="btn btn-primary" :disabled="!!busy || !editForm.name.trim()" @click="saveEdit">保存</button>
        </div>
      </div>
    </div>

    <div v-if="showUpload" class="modal-mask" @click.self="showUpload = false">
      <div class="modal">
        <h3>{{ uploadKind === 'image' ? '上传图片素材' : '上传视频或音频素材' }}</h3>
        <div class="field">
          <label for="upload-target">绑定范围</label>
          <select id="upload-target" v-model="uploadForm.target">
            <option value="project">项目素材</option>
            <option value="episode">剧集素材</option>
            <option value="character">角色形象</option>
            <option value="scene">场景图片</option>
            <option value="prop">道具图片</option>
            <option value="storyboard">分镜参考图</option>
          </select>
        </div>
        <div v-if="uploadForm.target === 'storyboard'" class="field">
          <label for="upload-episode">所属剧集</label>
          <select id="upload-episode" v-model.number="uploadForm.episode_id">
            <option v-for="episode in episodes" :key="episode.id" :value="episode.id">第 {{ episode.episode_number }} 集</option>
          </select>
        </div>
        <div v-if="uploadForm.target !== 'project'" class="field">
          <label for="upload-target-id">绑定目标</label>
          <select id="upload-target-id" v-model.number="uploadForm.target_id">
            <option :value="0">请选择</option>
            <option v-for="target in uploadTargets" :key="target.id" :value="target.id">
              <template v-if="uploadForm.target === 'episode'">第 {{ target.episode_number }} 集 · {{ target.title }}</template>
              <template v-else-if="uploadForm.target === 'scene'">{{ target.location }} · {{ target.time }}</template>
              <template v-else-if="uploadForm.target === 'storyboard'">#{{ target.storyboard_number }} {{ target.title || '镜头' }}</template>
              <template v-else>{{ target.name }}</template>
            </option>
          </select>
        </div>
        <div class="field"><label for="upload-name">名称</label><input id="upload-name" v-model="uploadForm.name" maxlength="120" placeholder="默认使用文件名" /></div>
        <div class="field"><label for="upload-category">分类</label><input id="upload-category" v-model="uploadForm.category" maxlength="60" /></div>
        <div class="field">
          <label for="upload-file">素材文件</label>
          <input id="upload-file" type="file" :accept="uploadKind === 'image' ? 'image/png,image/jpeg,image/webp' : 'video/*,audio/*'" :disabled="!!busy" @change="chooseUploadFile" />
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showUpload = false">取消</button>
          <button class="btn btn-primary" :disabled="!!busy || !uploadFile" @click="uploadImage">上传并绑定</button>
        </div>
      </div>
    </div>

    <div v-if="message" class="toast" role="status">{{ message }}</div>
  </div>
</template>
