<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { dramaAPI, episodeAPI, propAPI, settingsAPI } from '../api'
import { authStore } from '../auth'

const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const configs = ref<any[]>([])
const propsList = ref<any[]>([])
const activeView = ref<'episodes' | 'assets'>('episodes')
const assetView = ref<'characters' | 'scenes' | 'props'>('characters')
const loading = ref(true)
const busy = ref('')
const error = ref('')
const message = ref('')
const showAddEpisode = ref(false)
const showPropDialog = ref(false)
const episodeError = ref('')
const propError = ref('')
const episodeTitleInput = ref<HTMLInputElement | null>(null)
const propNameInput = ref<HTMLInputElement | null>(null)
const episodeForm = ref({ title: '', image_config_id: 0, video_config_id: 0, audio_config_id: 0 })
const propForm = ref<{ id?: number; name: string; description: string; prompt: string }>({ name: '', description: '', prompt: '' })
const styleLabels: Record<string, string> = { realistic: '写实', anime: '动漫', cinematic: '电影感' }

const id = computed(() => Number(route.params.id))
const canEdit = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const sortedEpisodes = computed(() => [...(drama.value?.episodes || [])].sort((a: any, b: any) => (a.episode_number || 0) - (b.episode_number || 0)))
const projectSummary = computed(() => {
  const value = String(drama.value?.description || '').replace(/^#+\s*/gm, '').replace(/\s+/g, ' ').trim()
  return value || '暂无简介'
})
const imageConfigs = computed(() => configs.value.filter((config) => config.service_type === 'image' && config.is_active !== false))
const videoConfigs = computed(() => configs.value.filter((config) => config.service_type === 'video' && config.is_active !== false))
const audioConfigs = computed(() => configs.value.filter((config) => config.service_type === 'audio' && config.is_active !== false))
const projectStyle = computed(() => styleLabels[drama.value?.style || ''] || drama.value?.style || '未设置风格')

function notify(text: string) {
  message.value = text
  window.setTimeout(() => { if (message.value === text) message.value = '' }, 2400)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [project, aiConfigs] = await Promise.all([dramaAPI.get(id.value), settingsAPI.aiConfigs()])
    drama.value = project
    configs.value = aiConfigs
    try { propsList.value = await propAPI.list(id.value) } catch { propsList.value = project.props || [] }
    episodeForm.value.image_config_id = imageConfigs.value[0]?.id || 0
    episodeForm.value.video_config_id = videoConfigs.value[0]?.id || 0
    episodeForm.value.audio_config_id = audioConfigs.value[0]?.id || 0
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '项目加载失败'
  } finally {
    loading.value = false
  }
}

async function openEpisodeDialog() {
  episodeError.value = ''
  showAddEpisode.value = true
  await nextTick()
  episodeTitleInput.value?.focus()
}

async function addEpisode() {
  episodeError.value = ''
  if (!episodeForm.value.image_config_id || !episodeForm.value.video_config_id || !episodeForm.value.audio_config_id) {
    episodeError.value = '请先选择图片、视频和音频服务'
    return
  }
  busy.value = 'episode'
  try {
    await episodeAPI.create({
      drama_id: id.value,
      title: episodeForm.value.title.trim() || undefined,
      image_config_id: episodeForm.value.image_config_id,
      video_config_id: episodeForm.value.video_config_id,
      audio_config_id: episodeForm.value.audio_config_id,
    })
    showAddEpisode.value = false
    episodeForm.value.title = ''
    notify('剧集已创建')
    await load()
  } catch (reason) {
    episodeError.value = reason instanceof Error ? reason.message : '创建剧集失败'
  } finally {
    busy.value = ''
  }
}

async function openPropDialog(prop?: any) {
  propError.value = ''
  propForm.value = prop
    ? {
        id: prop.id,
        name: prop.name || '',
        description: prop.description || '',
        prompt: prop.prompt || '',
      }
    : { name: '', description: '', prompt: '' }
  showPropDialog.value = true
  await nextTick()
  propNameInput.value?.focus()
}

function closePropDialog() {
  showPropDialog.value = false
  propError.value = ''
  propForm.value = { name: '', description: '', prompt: '' }
}

async function saveProp() {
  propError.value = ''
  const name = propForm.value.name.trim()
  if (!name) {
    propError.value = '请填写道具名称'
    propNameInput.value?.focus()
    return
  }
  busy.value = 'prop'
  try {
    const payload = {
      name,
      description: propForm.value.description.trim(),
      prompt: propForm.value.prompt.trim(),
    }
    if (propForm.value.id) {
      await propAPI.update(propForm.value.id, payload)
      notify('道具已更新')
    } else {
      await propAPI.create({ drama_id: id.value, ...payload })
      notify('道具已添加')
    }
    closePropDialog()
    await load()
  } catch (reason) {
    propError.value = reason instanceof Error ? reason.message : (propForm.value.id ? '更新道具失败' : '添加道具失败')
  } finally {
    busy.value = ''
  }
}

async function generatePropImage(prop: any) {
  busy.value = `prop-${prop.id}`
  try {
    const episode = (drama.value.episodes || [])[0]
    await propAPI.generateImage(prop.id, episode?.id)
    notify('道具图片任务已提交')
    await load()
  } catch (reason) {
    notify(reason instanceof Error ? reason.message : '生成道具图片失败')
  } finally {
    busy.value = ''
  }
}

async function removeProp(prop: any) {
  if (!confirm(`删除道具「${prop.name}」？`)) return
  busy.value = `prop-del-${prop.id}`
  try {
    await propAPI.del(prop.id)
    notify('道具已删除')
    await load()
  } catch (reason) {
    notify(reason instanceof Error ? reason.message : '删除道具失败')
  } finally {
    busy.value = ''
  }
}

function openEpisode(episode: any) {
  router.push(`/drama/${id.value}/episode/${episode.episode_number}`)
}

async function delEpisode(episode: any, event?: Event) {
  event?.stopPropagation()
  event?.preventDefault()
  const title = episode.title || `第 ${episode.episode_number} 集`
  if (!confirm(`删除剧集「${title}」？相关分镜与制作记录将不再可用。`)) return
  busy.value = `episode-del-${episode.id}`
  try {
    await episodeAPI.del(episode.id)
    notify('剧集已删除')
    await load()
  } catch (reason) {
    notify(reason instanceof Error ? reason.message : '删除剧集失败')
  } finally {
    busy.value = ''
  }
}

async function copyEpisode(episode: any, event?: Event) {
  event?.stopPropagation()
  event?.preventDefault()
  const title = episode.title || `第 ${episode.episode_number} 集`
  busy.value = `episode-copy-${episode.id}`
  try {
    await episodeAPI.copy(episode.id)
    notify(`已复制「${title}」`)
    await load()
  } catch (reason) {
    notify(reason instanceof Error ? reason.message : '复制剧集失败')
  } finally {
    busy.value = ''
  }
}

async function moveEpisode(episode: any, direction: 'up' | 'down', event?: Event) {
  event?.stopPropagation()
  event?.preventDefault()
  busy.value = `episode-move-${episode.id}-${direction}`
  try {
    await episodeAPI.move(episode.id, direction)
    notify(direction === 'up' ? '已上移' : '已下移')
    await load()
  } catch (reason) {
    notify(reason instanceof Error ? reason.message : '调整顺序失败')
  } finally {
    busy.value = ''
  }
}

function statusText(status?: string) {
  return ({ draft: '草稿', processing: '制作中', completed: '已完成', failed: '失败' } as Record<string, string>)[status || ''] || status || '草稿'
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="page">
    <div class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>正在打开项目</strong>
        <p class="muted" style="margin:6px 0 0">加载剧集、资产与生成配置…</p>
      </div>
    </div>
  </div>
  <div v-else-if="error" class="page"><div class="load-error"><h2>项目加载失败</h2><p class="muted">{{ error }}</p><button class="btn" @click="load">重新加载</button></div></div>
  <div v-else-if="drama" class="page">
    <div class="page-head">
      <div><h1 class="page-title">{{ drama.title }}</h1><p class="page-desc project-description">{{ projectSummary }} · {{ projectStyle }}</p></div>
      <div class="toolbar project-head-actions"><button class="btn" @click="router.push('/')">返回项目</button><button class="btn" @click="router.push(`/drama/${id}/assets`)">素材库</button></div>
    </div>

    <div class="settings-tabs" role="tablist" aria-label="项目内容">
      <button role="tab" :aria-selected="activeView === 'episodes'" :class="{ active: activeView === 'episodes' }" @click="activeView = 'episodes'">剧集</button>
      <button role="tab" :aria-selected="activeView === 'assets'" :class="{ active: activeView === 'assets' }" @click="activeView = 'assets'">项目资产</button>
    </div>

    <section v-if="activeView === 'episodes'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>剧集</h2><p class="muted">{{ sortedEpisodes.length }} 集 · 按制作顺序进入工作台</p></div><button v-if="canEdit" class="btn btn-primary" @click="openEpisodeDialog">新增剧集</button></div>
      <div v-if="sortedEpisodes.length" class="episode-grid">
        <article v-for="(episode, episodeIndex) in sortedEpisodes" :key="episode.id" class="card episode-card" role="button" tabindex="0" @click="openEpisode(episode)" @keydown.enter="openEpisode(episode)" @keydown.space.prevent="openEpisode(episode)">
          <div class="episode-number">{{ String(episode.episode_number).padStart(2, '0') }}</div>
          <div class="episode-main"><div class="episode-title-row"><h3>{{ episode.title || `第 ${episode.episode_number} 集` }}</h3><span class="audit-status" :class="episode.status === 'failed' ? 'fail' : episode.status === 'completed' ? 'ok' : ''">{{ statusText(episode.status) }}</span></div><p class="muted">{{ episode.script_content ? '剧本已就绪' : '等待录入或生成剧本' }}</p></div>
          <div class="episode-actions" @click.stop>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`上移剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="上移" :disabled="!!busy || episodeIndex === 0" @click="moveEpisode(episode, 'up', $event)">上移</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`下移剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="下移" :disabled="!!busy || episodeIndex === sortedEpisodes.length - 1" @click="moveEpisode(episode, 'down', $event)">下移</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`复制剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="复制剧集" :disabled="!!busy" @click="copyEpisode(episode, $event)">复制</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`删除剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="删除剧集" :disabled="!!busy" @click="delEpisode(episode, $event)">删除</button>
            <span class="episode-enter">进入工作台</span>
          </div>
        </article>
      </div>
      <div v-else class="empty project-empty"><strong>还没有剧集</strong><span class="muted">创建第一集后即可进入 AI 制作工作台。</span><button v-if="canEdit" class="btn btn-primary" @click="openEpisodeDialog">新增剧集</button></div>
    </section>

    <section v-else class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>项目资产</h2><p class="muted">角色 {{ drama.characters?.length || 0 }} · 场景 {{ drama.scenes?.length || 0 }} · 道具 {{ propsList.length }}</p></div><button v-if="canEdit && assetView === 'props'" class="btn btn-primary" @click="openPropDialog">添加道具</button></div>
      <div class="project-asset-tabs" role="tablist" aria-label="资产类型">
        <button role="tab" :aria-selected="assetView === 'characters'" :class="{ active: assetView === 'characters' }" @click="assetView = 'characters'">角色 {{ drama.characters?.length || 0 }}</button>
        <button role="tab" :aria-selected="assetView === 'scenes'" :class="{ active: assetView === 'scenes' }" @click="assetView = 'scenes'">场景 {{ drama.scenes?.length || 0 }}</button>
        <button role="tab" :aria-selected="assetView === 'props'" :class="{ active: assetView === 'props' }" @click="assetView = 'props'">道具 {{ propsList.length }}</button>
      </div>

      <div class="panel project-asset-list">
        <table v-if="assetView === 'characters' && drama.characters?.length" class="table"><thead><tr><th>名称</th><th>定位</th><th>外观</th><th>音色</th></tr></thead><tbody><tr v-for="character in drama.characters" :key="character.id"><td><strong>{{ character.name }}</strong></td><td>{{ character.role || '未设置' }}</td><td>{{ character.appearance || character.description || '未填写' }}</td><td>{{ character.voice_style || '未绑定' }}</td></tr></tbody></table>
        <table v-else-if="assetView === 'scenes' && drama.scenes?.length" class="table"><thead><tr><th>地点</th><th>时间</th><th>描述</th></tr></thead><tbody><tr v-for="scene in drama.scenes" :key="scene.id"><td><strong>{{ scene.location }}</strong></td><td>{{ scene.time || '未设置' }}</td><td>{{ scene.description || scene.prompt || '未填写' }}</td></tr></tbody></table>
        <table v-else-if="assetView === 'props' && propsList.length" class="table"><thead><tr><th>形象</th><th>名称</th><th>描述</th><th></th></tr></thead><tbody><tr v-for="prop in propsList" :key="prop.id"><td><img v-if="prop.image_url" class="thumb" :src="prop.image_url" :alt="prop.name" /><span v-else class="character-library-placeholder">{{ prop.name.slice(0, 1) }}</span></td><td><strong>{{ prop.name }}</strong></td><td>{{ prop.description || prop.prompt || '未填写' }}</td><td><div v-if="canEdit" class="toolbar settings-table-actions"><button class="btn" :disabled="!!busy" @click="generatePropImage(prop)">生成图片</button><button class="btn" :disabled="!!busy" @click="openPropDialog(prop)">编辑</button><button class="btn btn-danger" :disabled="!!busy" @click="removeProp(prop)">删除</button></div></td></tr></tbody></table>
        <div v-else class="empty project-empty"><strong>暂无{{ assetView === 'characters' ? '角色' : assetView === 'scenes' ? '场景' : '道具' }}</strong><span class="muted">角色和场景可在剧集工作台中通过 AI 提取。</span></div>
      </div>
    </section>

    <div v-if="showAddEpisode" class="modal-mask" @click.self="showAddEpisode = false">
      <form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="add-episode-title" @keydown.esc="showAddEpisode = false" @submit.prevent="addEpisode">
        <h3 id="add-episode-title">新增剧集</h3>
        <div class="field"><label for="episode-title">标题（选填）</label><input id="episode-title" ref="episodeTitleInput" v-model="episodeForm.title" maxlength="200" placeholder="默认按集数命名" /></div>
        <div class="field"><label for="episode-image-config">图片服务</label><select id="episode-image-config" v-model.number="episodeForm.image_config_id"><option :value="0">请选择</option><option v-for="config in imageConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <div class="field"><label for="episode-video-config">视频服务</label><select id="episode-video-config" v-model.number="episodeForm.video_config_id"><option :value="0">请选择</option><option v-for="config in videoConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <div class="field"><label for="episode-audio-config">音频服务</label><select id="episode-audio-config" v-model.number="episodeForm.audio_config_id"><option :value="0">请选择</option><option v-for="config in audioConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <p v-if="episodeError" class="auth-error" role="alert">{{ episodeError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="showAddEpisode = false">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'episode'">{{ busy === 'episode' ? '创建中…' : '创建剧集' }}</button></div>
      </form>
    </div>

    <div v-if="showPropDialog" class="modal-mask" @click.self="closePropDialog">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="propForm.id ? 'edit-prop-title' : 'add-prop-title'" @keydown.esc="closePropDialog" @submit.prevent="saveProp">
        <h3 :id="propForm.id ? 'edit-prop-title' : 'add-prop-title'">{{ propForm.id ? '编辑道具' : '添加道具' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field"><label for="prop-name">名称 <span class="required-mark">*</span></label><input id="prop-name" ref="propNameInput" v-model="propForm.name" maxlength="120" required /></div>
        <div class="field"><label for="prop-description">描述</label><textarea id="prop-description" v-model="propForm.description" rows="4" maxlength="4000" /></div>
        <div class="field"><label for="prop-prompt">画面提示词</label><textarea id="prop-prompt" v-model="propForm.prompt" rows="3" maxlength="4000" placeholder="可选，用于生成道具形象" /></div>
        <p v-if="propError" class="auth-error" role="alert">{{ propError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closePropDialog">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'prop'">{{ busy === 'prop' ? '保存中…' : (propForm.id ? '保存道具' : '添加道具') }}</button></div>
      </form>
    </div>

    <div v-if="message" class="toast" role="status">{{ message }}</div>
  </div>
</template>
