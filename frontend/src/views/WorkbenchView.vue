<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  agentAPI, characterAPI, composeAPI, dramaAPI, episodeAPI, gridAPI,
  mergeAPI, sceneAPI, settingsAPI, storyboardAPI
  , assetAPI, jobsAPI, uploadAPI
} from '../api'

const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const episode = ref<any>(null)
const characters = ref<any[]>([])
const scenes = ref<any[]>([])
const storyboards = ref<any[]>([])
const status = ref<any>(null)
const voices = ref<any[]>([])
const gridHist = ref<any[]>([])
const tab = ref<'script' | 'cast' | 'grid' | 'boards' | 'export'>('script')
const rawContent = ref('')
const busy = ref('')
const log = ref('')
const toast = ref('')
const loading = ref(true)
const loadError = ref('')
const pollTimer = ref<number | null>(null)

// grid state
const gridRows = ref(2)
const gridCols = ref(2)
const gridMode = ref('first_frame')
const gridPrompt = ref('')
const gridCells = ref<string[]>([])
const gridImage = ref('')
const gridHistoryId = ref<number | null>(null)
const selectedShotIds = ref<number[]>([])
const assets = ref<any[]>([])
const jobs = ref<any[]>([])
const assetTargetShot = ref<Record<number, number>>({})
const assetTargetFrame = ref<Record<number, string>>({})

// voice assign panel
const assignCharId = ref<number | null>(null)
const characterForm = ref<any | null>(null)
const sceneForm = ref<any | null>(null)
const storyboardForm = ref<any | null>(null)

const dramaId = computed(() => Number(route.params.id))
const episodeNumber = computed(() => Number(route.params.episodeNumber))

const progressPct = computed(() => {
  if (!status.value) return 0
  const s = status.value
  const parts = [
    s.has_script ? 1 : 0,
    s.characters > 0 ? 1 : 0,
    s.scenes > 0 ? 1 : 0,
    s.storyboards > 0 ? 1 : 0,
    s.with_video > 0 ? 1 : 0,
    s.composed > 0 ? 1 : 0,
  ]
  return Math.round((parts.reduce((a, b) => a + b, 0) / parts.length) * 100)
})

function show(msg: string) {
  toast.value = msg
  setTimeout(() => (toast.value = ''), 2600)
}

async function load() {
  drama.value = await dramaAPI.get(dramaId.value)
  episode.value = (drama.value.episodes || []).find((e: any) => e.episode_number === episodeNumber.value)
  if (!episode.value) return
  rawContent.value = episode.value.content || ''
  await refreshAssets()
  try {
    voices.value = await settingsAPI.voices()
  } catch { /* optional */ }
}

async function loadWorkbench() {
  stopPoll()
  loading.value = true
  loadError.value = ''
  try {
    await load()
    if (!episode.value) loadError.value = '未找到对应剧集'
  } catch (error) {
    episode.value = null
    loadError.value = error instanceof Error ? error.message : '加载失败'
  } finally {
    loading.value = false
  }
  if (episode.value) startPoll()
}

async function refreshAssets() {
  if (!episode.value) return
  characters.value = await episodeAPI.characters(episode.value.id)
  scenes.value = await episodeAPI.scenes(episode.value.id)
  storyboards.value = await episodeAPI.storyboards(episode.value.id)
  status.value = await episodeAPI.pipelineStatus(episode.value.id)
  try {
    gridHist.value = await gridAPI.history({ episode_id: episode.value.id })
  } catch { gridHist.value = [] }
  try {
    assets.value = await assetAPI.list({ episode_id: episode.value.id })
  } catch { assets.value = [] }
  try {
    jobs.value = await jobsAPI.list({ limit: 50 })
  } catch { jobs.value = [] }
  if (!selectedShotIds.value.length && storyboards.value.length) {
    selectedShotIds.value = storyboards.value.map((s: any) => s.id)
  }
  // refresh episode video_url
  drama.value = await dramaAPI.get(dramaId.value)
  episode.value = (drama.value.episodes || []).find((e: any) => e.episode_number === episodeNumber.value) || episode.value
}

function startPoll() {
  stopPoll()
  pollTimer.value = window.setInterval(() => {
    if (document.visibilityState === 'visible') refreshAssets().catch(() => {})
  }, 5000)
}
function stopPoll() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

async function saveContent() {
  await episodeAPI.update(episode.value.id, { content: rawContent.value })
  show('已保存原文')
  await load()
}

async function runAgent(type: string, message: string) {
  busy.value = type
  log.value = `运行 ${type}…`
  try {
    const res = await agentAPI.chat(type, {
      message,
      drama_id: dramaId.value,
      episode_id: episode.value.id,
    })
    log.value = res.text || JSON.stringify(res, null, 2)
    show(`${type} 完成`)
    await load()
  } catch (e: any) {
    log.value = e.message
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function genCharImage(c: any) {
  busy.value = 'char-img-' + c.id
  try {
    await characterAPI.generateImage(c.id, episode.value.id)
    show('角色图任务已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function batchCharImages() {
  busy.value = 'char-batch'
  try {
    const ids = characters.value.map((c) => c.id)
    await characterAPI.batchImages(ids, episode.value.id)
    show('批量角色图已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function genSceneImage(sc: any) {
  busy.value = 'scene-img-' + sc.id
  try {
    await sceneAPI.generateImage(sc.id, episode.value.id)
    show('场景图任务已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function uploadBoundImage(kind: 'character' | 'scene', item: any, event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  busy.value = `upload-${kind}-${item.id}`
  try {
    await uploadAPI.image(file, {
      [`${kind}_id`]: item.id,
      drama_id: dramaId.value,
      episode_id: episode.value.id,
      name: `${kind === 'character' ? item.name : item.location} 上传图`,
    })
    show('图片已上传并绑定')
    await refreshAssets()
  } catch (e: any) { show(e.message) } finally {
    busy.value = ''
    input.value = ''
  }
}

function editCharacter(character?: any) {
  characterForm.value = character
    ? { id: character.id, name: character.name, role: character.role || '', appearance: character.appearance || '', description: character.description || '', personality: character.personality || '' }
    : { name: '', role: '', appearance: '', description: '', personality: '' }
}

async function saveCharacter() {
  const form = characterForm.value
  if (!form?.name?.trim()) return show('请输入角色名')
  busy.value = 'character-save'
  try {
    const data = { ...form, drama_id: dramaId.value, episode_id: episode.value.id }
    if (form.id) await characterAPI.update(form.id, data)
    else await characterAPI.create(data)
    characterForm.value = null
    show(form.id ? '角色已更新' : '角色已添加')
    await refreshAssets()
  } catch (e: any) { show(e.message) } finally { busy.value = '' }
}

async function removeCharacter(character: any) {
  if (!window.confirm(`删除角色“${character.name}”？`)) return
  try {
    await characterAPI.del(character.id)
    show('角色已删除')
    await refreshAssets()
  } catch (e: any) { show(e.message) }
}

function editScene(scene?: any) {
  sceneForm.value = scene
    ? { id: scene.id, location: scene.location, time: scene.time || '', prompt: scene.prompt || '' }
    : { location: '', time: '', prompt: '' }
}

async function saveScene() {
  const form = sceneForm.value
  if (!form?.location?.trim()) return show('请输入场景地点')
  busy.value = 'scene-save'
  try {
    const data = { ...form, drama_id: dramaId.value, episode_id: episode.value.id }
    if (form.id) await sceneAPI.update(form.id, data)
    else await sceneAPI.create(data)
    sceneForm.value = null
    show(form.id ? '场景已更新' : '场景已添加')
    await refreshAssets()
  } catch (e: any) { show(e.message) } finally { busy.value = '' }
}

async function removeScene(scene: any) {
  if (!window.confirm(`删除场景“${scene.location}”？`)) return
  try {
    await sceneAPI.del(scene.id)
    show('场景已删除')
    await refreshAssets()
  } catch (e: any) { show(e.message) }
}

async function voiceSample(c: any) {
  busy.value = 'voice-' + c.id
  try {
    await characterAPI.voiceSample(c.id, episode.value.id)
    show('试听已生成')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function assignVoice(c: any, voiceId: string, provider = 'minimax') {
  await characterAPI.update(c.id, { voice_style: voiceId, voice_provider: provider })
  show(`已分配音色 ${voiceId}`)
  assignCharId.value = null
  await refreshAssets()
}

async function genFrame(sb: any, frameType = 'first_frame') {
  busy.value = `frame-${frameType}-${sb.id}`
  try {
    await storyboardAPI.generateFrame(sb.id, { frame_type: frameType, episode_id: episode.value.id })
    show('帧图任务已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function batchFrames(frameType = 'first_frame') {
  busy.value = 'batch-frames'
  try {
    await storyboardAPI.batchFrames({
      episode_id: episode.value.id,
      frame_type: frameType,
      storyboard_ids: selectedShotIds.value,
    })
    show('批量帧生成已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function genVideo(sb: any) {
  busy.value = 'video-' + sb.id
  try {
    await storyboardAPI.generateVideo(sb.id, {})
    show('视频生成已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function batchVideos() {
  busy.value = 'batch-videos'
  try {
    await storyboardAPI.batchVideos({
      episode_id: episode.value.id,
      storyboard_ids: selectedShotIds.value,
    })
    show('批量视频已提交')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function genTTS(sb: any) {
  busy.value = 'tts-' + sb.id
  try {
    const res = await storyboardAPI.generateTTS(sb.id)
    show(res.job_id ? `配音任务已排队 #${res.job_id}` : '配音已完成')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function batchTTS() {
  busy.value = 'batch-tts'
  try {
    const res = await storyboardAPI.batchTTS({
      episode_id: episode.value.id,
      storyboard_ids: selectedShotIds.value,
    })
    show(`配音任务已排队 ok=${res.ok} fail=${res.fail}`)
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function composeShot(sb: any) {
  busy.value = 'compose-' + sb.id
  try {
    const res = await composeAPI.shot(sb.id)
    show(res.job_id ? `镜头合成任务已排队 #${res.job_id}` : '镜头合成完成')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function composeAll() {
  busy.value = 'compose-all'
  try {
    const res = await composeAPI.all(episode.value.id)
    show(res.job_id ? `批量合成任务已排队 #${res.job_id}` : `合成完成 ok=${res.ok} fail=${res.fail}`)
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function mergeAll() {
  busy.value = 'merge'
  try {
    const res = await mergeAPI.merge(episode.value.id)
    show(res.job_id ? `成片导出任务已排队 #${res.job_id}` : '成片导出完成')
    if (res.merged_url) log.value = res.merged_url
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function cancelJob(job: any) {
  try {
    await jobsAPI.cancel(job.id)
    show('任务已取消')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  }
}

async function retryJob(job: any) {
  try {
    await jobsAPI.retry(job.id)
    show('任务已重新排队')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  }
}

async function applyAsset(asset: any) {
  const storyboardId = assetTargetShot.value[asset.id]
  if (!storyboardId) {
    show('请选择目标分镜')
    return
  }
  try {
    await assetAPI.apply(asset.id, {
      storyboard_id: storyboardId,
      frame_type: assetTargetFrame.value[asset.id] || 'first_frame',
    })
    show('素材已写回分镜')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  }
}

async function buildGridPrompt() {
  busy.value = 'grid-prompt'
  try {
    const res = await gridAPI.prompt({
      rows: gridRows.value,
      cols: gridCols.value,
      mode: gridMode.value,
      episode_id: episode.value.id,
      drama_id: dramaId.value,
    })
    gridPrompt.value = res.grid_prompt || ''
    show('宫格提示词已生成')
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function generateGrid() {
  if (!gridPrompt.value.trim()) {
    await buildGridPrompt()
  }
  busy.value = 'grid-gen'
  try {
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    if (gridMode.value === 'first_last' && (gridCapacity % 2 || !selected.length)) {
      throw new Error('首尾参考需要偶数宫格，并至少选择一个镜头')
    }
    const res = await gridAPI.generate({
      prompt: gridPrompt.value,
      drama_id: dramaId.value,
      episode_id: episode.value.id,
      mode: gridMode.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
    })
    const img = res.image || res
    const hist = res.history
    gridImage.value = img.image_url || ''
    gridHistoryId.value = hist?.id || null
    if (img.id && !gridImage.value) {
      // poll image status
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000))
        const st = await gridAPI.status(img.id)
        if (st.image_url) {
          gridImage.value = st.image_url
          break
        }
        if (st.status === 'failed') throw new Error(st.error_msg || 'grid failed')
      }
    }
    show('宫格图生成完成')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function splitGrid() {
  if (!gridImage.value) {
    show('请先生成宫格图')
    return
  }
  busy.value = 'grid-split'
  try {
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    if (gridMode.value === 'first_last' && (gridCapacity % 2 || !selected.length)) {
      throw new Error('首尾参考需要偶数宫格，并至少选择一个镜头')
    }
    const res = await gridAPI.split({
      image_url: gridImage.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
      frame_type: gridMode.value === 'first_last' ? 'first_last' : 'first_frame',
      history_id: gridHistoryId.value || undefined,
    })
    gridCells.value = res.cells || []
    show(`已切分 ${res.count} 格并写回分镜`)
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

function addStoryboard() {
  storyboardForm.value = {
    title: '',
    duration: 12,
    shot_type: '',
    description: '',
    dialogue: '',
    image_prompt: '',
    video_prompt: '',
  }
}

async function saveStoryboard() {
  const form = storyboardForm.value
  if (!form?.title?.trim()) return show('请输入分镜标题')
  busy.value = 'storyboard-save'
  try {
    await storyboardAPI.create({
      ...form,
      title: form.title.trim(),
      episode_id: episode.value.id,
    })
    storyboardForm.value = null
    show('分镜已添加')
    await refreshAssets()
  } catch (error: any) {
    show(error.message)
  } finally {
    busy.value = ''
  }
}

async function removeStoryboard(storyboard: any) {
  if (!window.confirm(`删除分镜“${storyboard.title || `#${storyboard.storyboard_number}`}”？`)) return
  busy.value = `storyboard-delete-${storyboard.id}`
  try {
    await storyboardAPI.del(storyboard.id)
    selectedShotIds.value = selectedShotIds.value.filter((id) => id !== storyboard.id)
    show('分镜已删除')
    await refreshAssets()
  } catch (error: any) {
    show(error.message)
  } finally {
    busy.value = ''
  }
}


async function saveShot(sb: any, fields: Record<string, any>) {
  busy.value = 'save-shot-' + sb.id
  try {
    await storyboardAPI.update(sb.id, fields)
    show('分镜已更新')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function editShotField(sb: any, field: string, label: string) {
  const cur = sb[field] || ''
  const next = window.prompt(label, cur)
  if (next === null) return
  await saveShot(sb, { [field]: next })
}

function toggleShot(id: number) {
  const i = selectedShotIds.value.indexOf(id)
  if (i >= 0) selectedShotIds.value.splice(i, 1)
  else selectedShotIds.value.push(id)
}

function shotStatusDot(sb: any) {
  if (sb.composed_video_url) return 'ok'
  if (sb.video_url) return 'run'
  if (sb.status === 'failed') return 'fail'
  return ''
}

onMounted(loadWorkbench)
onUnmounted(stopPoll)
</script>

<template>
  <div class="page workbench" v-if="episode">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ drama?.title }} · {{ episode.title }}</h1>
        <p class="page-desc">制作工作台 · 剧本 → 角色场景 → 宫格帧 → 分镜视频 → 合成导出</p>
      </div>
      <div class="row">
        <button class="btn" @click="loadWorkbench">刷新</button>
        <button class="btn" @click="router.push(`/drama/${dramaId}`)">返回项目</button>
      </div>
    </div>

    <div class="panel" style="margin-bottom:16px">
      <div class="row" style="justify-content:space-between;align-items:center">
        <div class="row" style="gap:8px">
          <span class="stat-chip">剧本 <strong>{{ status?.has_script ? '✓' : '—' }}</strong></span>
          <span class="stat-chip">角色 <strong>{{ status?.characters || 0 }}</strong></span>
          <span class="stat-chip">场景 <strong>{{ status?.scenes || 0 }}</strong></span>
          <span class="stat-chip">分镜 <strong>{{ status?.storyboards || 0 }}</strong></span>
          <span class="stat-chip">视频 <strong>{{ status?.with_video || 0 }}</strong></span>
          <span class="stat-chip">配音 <strong>{{ status?.with_tts || 0 }}</strong></span>
          <span class="stat-chip">合成 <strong>{{ status?.composed || 0 }}</strong></span>
        </div>
        <div style="min-width:140px">
          <div class="muted" style="font-size:12px;margin-bottom:4px">流水线 {{ progressPct }}%</div>
          <div class="progress-bar"><i :style="{ width: progressPct + '%' }"></i></div>
        </div>
      </div>
    </div>

    <div class="wb-shell">
      <aside class="wb-side">
        <button class="side-item" :class="{ active: tab==='script' }" @click="tab='script'">1. 剧本</button>
        <button class="side-item" :class="{ active: tab==='cast' }" @click="tab='cast'">2. 角色 / 场景</button>
        <button class="side-item" :class="{ active: tab==='grid' }" @click="tab='grid'">3. 宫格帧</button>
        <button class="side-item" :class="{ active: tab==='boards' }" @click="tab='boards'">4. 分镜 / 视频</button>
        <button class="side-item" :class="{ active: tab==='export' }" @click="tab='export'">5. 合成导出</button>
      </aside>

      <div class="wb-main">
        <!-- SCRIPT -->
        <div v-if="tab==='script'" class="panel">
          <div class="toolbar">
            <button class="btn btn-primary" :disabled="!!busy" @click="saveContent">保存原文</button>
            <button class="btn" :disabled="!!busy" @click="runAgent('script_rewriter', '请将当前集内容改写为格式化剧本并保存')">AI 改写剧本</button>
          </div>
          <div class="split-2">
            <div class="field">
              <label>原始内容 / 大纲</label>
              <textarea v-model="rawContent" rows="18" placeholder="粘贴小说、大纲或分场内容…" />
            </div>
            <div class="field">
              <label>格式化剧本</label>
              <textarea :value="episode.script_content || ''" rows="18" readonly placeholder="AI 改写后显示在此" />
            </div>
          </div>
        </div>

        <!-- CAST -->
        <div v-else-if="tab==='cast'" class="row">
          <div class="col panel">
            <div class="toolbar">
              <button class="btn btn-primary" :disabled="!!busy" @click="runAgent('extractor', '请提取本集角色与场景并去重保存')">AI 提取</button>
              <button class="btn" :disabled="!!busy" @click="runAgent('voice_assigner', '请为所有角色分配音色')">AI 分配音色</button>
              <button class="btn" :disabled="!!busy || !characters.length" @click="batchCharImages">批量角色图</button>
              <button class="btn" :disabled="!!busy" @click="editCharacter()">添加角色</button>
            </div>
            <h3>角色</h3>
            <div v-if="characterForm" class="field-grid" style="margin-bottom:12px">
              <div class="field"><label>角色名</label><input v-model="characterForm.name" /></div>
              <div class="field"><label>定位</label><input v-model="characterForm.role" /></div>
              <div class="field"><label>外貌</label><textarea v-model="characterForm.appearance" rows="2" /></div>
              <div class="field"><label>性格</label><textarea v-model="characterForm.personality" rows="2" /></div>
              <div class="field" style="grid-column:1/-1"><label>说明</label><textarea v-model="characterForm.description" rows="2" /></div>
              <div class="toolbar" style="grid-column:1/-1">
                <button class="btn btn-primary" :disabled="!!busy" @click="saveCharacter">保存角色</button>
                <button class="btn" @click="characterForm=null">取消</button>
              </div>
            </div>
            <div class="list">
              <div v-for="c in characters" :key="c.id" class="list-item">
                <div class="row" style="justify-content:space-between;align-items:flex-start">
                  <div style="flex:1">
                    <h4>{{ c.name }} <span class="muted">{{ c.role }}</span></h4>
                    <p class="muted" style="margin:0;font-size:12px">{{ c.appearance || c.description || '暂无外貌' }}</p>
                    <p class="muted" style="margin:6px 0 0;font-size:12px">音色：{{ c.voice_style || '未分配' }}</p>
                    <audio v-if="c.voice_sample_url" :src="c.voice_sample_url" controls style="width:100%;margin-top:8px" />
                    <div v-if="assignCharId===c.id" class="voice-list" style="margin-top:8px">
                      <div
                        v-for="v in voices"
                        :key="v.voice_id"
                        class="voice-item"
                        :class="{ active: c.voice_style===v.voice_id }"
                        @click="assignVoice(c, v.voice_id, v.provider)"
                      >
                        <div>{{ v.voice_name || v.voice_id }}</div>
                        <div class="muted">{{ v.language || v.provider }}</div>
                      </div>
                      <div v-if="!voices.length" class="muted">无音色，请在设置中同步</div>
                    </div>
                  </div>
                  <div class="row" style="flex-direction:column;align-items:flex-end;gap:8px">
                    <img v-if="c.image_url" class="thumb" :src="c.image_url" />
                    <div class="mini-actions">
                      <button class="btn" :disabled="!!busy" @click="genCharImage(c)">形象</button>
                      <label class="btn" :aria-disabled="!!busy">上传<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="!!busy" style="display:none" @change="uploadBoundImage('character', c, $event)" /></label>
                      <button class="btn" :disabled="!!busy" @click="assignCharId = assignCharId===c.id ? null : c.id">音色</button>
                      <button class="btn" :disabled="!!busy || !c.voice_style" @click="voiceSample(c)">试听</button>
                      <button class="btn" :disabled="!!busy" @click="editCharacter(c)">编辑</button>
                      <button class="btn btn-danger" :disabled="!!busy" @click="removeCharacter(c)">删除</button>
                    </div>
                  </div>
                </div>
              </div>
              <div v-if="!characters.length" class="empty">尚未提取角色</div>
            </div>
          </div>
          <div class="col panel">
            <div class="toolbar" style="justify-content:space-between">
              <h3 style="margin:0">场景</h3>
              <button class="btn" :disabled="!!busy" @click="editScene()">添加场景</button>
            </div>
            <div v-if="sceneForm" class="field-grid" style="margin-bottom:12px">
              <div class="field"><label>地点</label><input v-model="sceneForm.location" /></div>
              <div class="field"><label>时间</label><input v-model="sceneForm.time" /></div>
              <div class="field" style="grid-column:1/-1"><label>画面提示词</label><textarea v-model="sceneForm.prompt" rows="3" /></div>
              <div class="toolbar" style="grid-column:1/-1">
                <button class="btn btn-primary" :disabled="!!busy" @click="saveScene">保存场景</button>
                <button class="btn" @click="sceneForm=null">取消</button>
              </div>
            </div>
            <div class="list">
              <div v-for="sc in scenes" :key="sc.id" class="list-item">
                <div class="row" style="justify-content:space-between">
                  <div>
                    <h4>{{ sc.location }} · {{ sc.time }}</h4>
                    <p class="muted" style="margin:0;font-size:12px">{{ sc.prompt }}</p>
                  </div>
                  <div class="row">
                    <img v-if="sc.image_url" class="thumb" :src="sc.image_url" />
                    <button class="btn" :disabled="!!busy" @click="genSceneImage(sc)">生成场景</button>
                    <label class="btn" :aria-disabled="!!busy">上传<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="!!busy" style="display:none" @change="uploadBoundImage('scene', sc, $event)" /></label>
                    <button class="btn" :disabled="!!busy" @click="editScene(sc)">编辑</button>
                    <button class="btn btn-danger" :disabled="!!busy" @click="removeScene(sc)">删除</button>
                  </div>
                </div>
              </div>
              <div v-if="!scenes.length" class="empty">尚未提取场景</div>
            </div>
          </div>
        </div>

        <!-- GRID -->
        <div v-else-if="tab==='grid'" class="panel">
          <div class="toolbar">
            <div class="seg">
              <button :class="{ active: gridMode==='first_frame' }" @click="gridMode='first_frame'">首帧宫格</button>
              <button :class="{ active: gridMode==='first_last' }" @click="gridMode='first_last'">首尾参考</button>
              <button :class="{ active: gridMode==='multi_ref' }" @click="gridMode='multi_ref'">多参一致</button>
            </div>
            <label class="muted" style="font-size:12px">行
              <input type="number" min="1" max="4" v-model.number="gridRows" style="width:52px;margin-left:4px" />
            </label>
            <label class="muted" style="font-size:12px">列
              <input type="number" min="1" max="4" v-model.number="gridCols" style="width:52px;margin-left:4px" />
            </label>
            <button class="btn" :disabled="!!busy" @click="buildGridPrompt">生成提示词</button>
            <button class="btn btn-primary" :disabled="!!busy" @click="generateGrid">生成宫格图</button>
            <button class="btn" :disabled="!!busy || !gridImage" @click="splitGrid">切分写回分镜</button>
            <button class="btn" :disabled="!!busy" @click="runAgent('grid_prompt_generator', '请为本集镜头生成宫格首帧提示词')">Agent 提示词</button>
          </div>
          <div class="field">
            <label>宫格提示词</label>
            <textarea v-model="gridPrompt" rows="8" placeholder="可先点击「生成提示词」" />
          </div>
          <div class="split-2">
            <div>
              <div class="muted" style="margin-bottom:8px;font-size:12px">预览</div>
              <img v-if="gridImage" class="grid-preview" :src="gridImage" />
              <div v-else class="empty">尚未生成宫格图</div>
              <div v-if="gridCells.length" class="cell-grid" style="margin-top:12px">
                <img v-for="(u,i) in gridCells" :key="i" :src="u" />
              </div>
            </div>
            <div>
              <div class="muted" style="margin-bottom:8px;font-size:12px">写入分镜（勾选）</div>
              <div class="list">
                <label v-for="sb in storyboards" :key="sb.id" class="list-item" style="display:flex;gap:10px;align-items:center;cursor:pointer">
                  <input type="checkbox" :checked="selectedShotIds.includes(sb.id)" @change="toggleShot(sb.id)" />
                  <span style="flex:1">#{{ sb.storyboard_number }} {{ sb.title || '镜头' }}</span>
                  <img v-if="sb.first_frame_image" class="thumb" style="width:48px;height:48px" :src="sb.first_frame_image" />
                </label>
                <div v-if="!storyboards.length" class="empty">请先拆解分镜</div>
              </div>
              <div style="margin-top:12px">
                <div class="muted" style="font-size:12px;margin-bottom:6px">历史</div>
                <div class="list">
                  <div v-for="h in gridHist" :key="h.id" class="list-item" style="font-size:12px">
                    #{{ h.id }} · {{ h.mode }} · {{ h.rows }}x{{ h.cols }} · {{ h.status }}
                    <button v-if="h.image_url" class="btn" style="margin-top:6px" @click="gridImage=h.image_url; gridHistoryId=h.id">载入</button>
                  </div>
                  <div v-if="!gridHist.length" class="muted">暂无历史</div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- BOARDS -->
        <div v-else-if="tab==='boards'" class="panel">
          <div class="toolbar">
            <button class="btn btn-primary" :disabled="!!busy" @click="runAgent('storyboard_breaker', '请根据当前剧本完整拆解分镜并保存')">AI 拆解分镜</button>
            <button class="btn" :disabled="!!busy" @click="addStoryboard">添加分镜</button>
            <button class="btn" :disabled="!!busy" @click="batchFrames('first_frame')">批量首帧</button>
            <button class="btn" :disabled="!!busy" @click="batchFrames('last_frame')">批量尾帧</button>
            <button class="btn" :disabled="!!busy" @click="batchVideos">批量视频</button>
            <button class="btn" :disabled="!!busy" @click="batchTTS">批量配音</button>
          </div>
          <div v-if="storyboardForm" class="storyboard-form field-grid">
            <div class="field">
              <label for="storyboard-title">分镜标题</label>
              <input id="storyboard-title" v-model="storyboardForm.title" maxlength="200" />
            </div>
            <div class="field">
              <label for="storyboard-duration">时长（秒）</label>
              <input id="storyboard-duration" v-model.number="storyboardForm.duration" type="number" min="1" max="3600" />
            </div>
            <div class="field">
              <label for="storyboard-shot-type">景别</label>
              <input id="storyboard-shot-type" v-model="storyboardForm.shot_type" maxlength="200" />
            </div>
            <div class="field">
              <label for="storyboard-dialogue">对白</label>
              <textarea id="storyboard-dialogue" v-model="storyboardForm.dialogue" rows="2" maxlength="10000" />
            </div>
            <div class="field" style="grid-column:1/-1">
              <label for="storyboard-description">镜头描述</label>
              <textarea id="storyboard-description" v-model="storyboardForm.description" rows="3" maxlength="10000" />
            </div>
            <div class="field">
              <label for="storyboard-image-prompt">图片提示词</label>
              <textarea id="storyboard-image-prompt" v-model="storyboardForm.image_prompt" rows="3" maxlength="10000" />
            </div>
            <div class="field">
              <label for="storyboard-video-prompt">视频提示词</label>
              <textarea id="storyboard-video-prompt" v-model="storyboardForm.video_prompt" rows="3" maxlength="10000" />
            </div>
            <div class="toolbar" style="grid-column:1/-1">
              <button class="btn btn-primary" :disabled="!!busy" @click="saveStoryboard">保存分镜</button>
              <button class="btn" :disabled="!!busy" @click="storyboardForm=null">取消</button>
            </div>
          </div>
          <div class="shot-grid">
            <div v-for="sb in storyboards" :key="sb.id" class="shot-card">
              <div class="row" style="justify-content:space-between;align-items:center">
                <h4>
                  <span class="status-dot" :class="shotStatusDot(sb)"></span>
                  #{{ sb.storyboard_number }} {{ sb.title || '镜头' }}
                </h4>
                <span class="muted" style="font-size:11px">{{ sb.duration }}s · {{ sb.status }}</span>
              </div>
              <p class="muted" style="margin:0;font-size:12px">{{ sb.shot_type }} / {{ sb.angle }} / {{ sb.movement }}</p>
              <p style="margin:8px 0 0;font-size:12px;color:var(--text)">{{ sb.description || sb.action }}</p>
              <p class="muted" style="margin:6px 0 0;font-size:12px">对白：{{ sb.dialogue || '（无）' }}</p>
              <div class="frame-row">
                <div class="frame-box">
                  <span class="frame-label">首帧</span>
                  <img v-if="sb.first_frame_image" :src="sb.first_frame_image" />
                  <span v-else class="muted" style="font-size:11px">空</span>
                </div>
                <div class="frame-box">
                  <span class="frame-label">尾帧</span>
                  <img v-if="sb.last_frame_image" :src="sb.last_frame_image" />
                  <span v-else class="muted" style="font-size:11px">空</span>
                </div>
              </div>
              <video v-if="sb.video_url" class="media" style="width:100%;margin-top:6px" :src="sb.video_url" controls />
              <audio v-if="sb.tts_audio_url" :src="sb.tts_audio_url" controls style="width:100%;margin-top:6px" />
              <div class="mini-actions">
                <button class="btn" :disabled="!!busy" @click="genFrame(sb,'first_frame')">首帧</button>
                <button class="btn" :disabled="!!busy" @click="genFrame(sb,'last_frame')">尾帧</button>
                <button class="btn" :disabled="!!busy" @click="genVideo(sb)">视频</button>
                <button class="btn" :disabled="!!busy" @click="genTTS(sb)">配音</button>
                <button class="btn" :disabled="!!busy" @click="composeShot(sb)">合成</button>
                <button class="btn" :disabled="!!busy" @click="editShotField(sb,'image_prompt','图片提示词')">改图词</button>
                <button class="btn" :disabled="!!busy" @click="editShotField(sb,'video_prompt','视频提示词')">改视频词</button>
                <button class="btn" :disabled="!!busy" @click="editShotField(sb,'dialogue','对白')">改对白</button>
                <button class="btn btn-danger" :disabled="!!busy" @click="removeStoryboard(sb)">删除</button>
              </div>
            </div>
          </div>
          <div v-if="!storyboards.length" class="empty" style="margin-top:12px">尚未拆解分镜</div>
        </div>

        <!-- EXPORT -->
        <div v-else class="panel">
          <div class="toolbar">
            <button class="btn" :disabled="!!busy" @click="composeAll">批量合成镜头</button>
            <button class="btn btn-primary" :disabled="!!busy" @click="mergeAll">拼接导出成片</button>
          </div>
          <p class="muted">将已生成视频与配音排队合成为镜头，再拼接为整集成片。任务状态可在右侧查看。</p>
          <video v-if="episode.video_url" class="media" style="width:min(720px,100%);margin-top:12px" :src="episode.video_url" controls />
          <div v-else class="empty" style="margin-top:12px">尚未导出成片</div>
          <div style="margin-top:16px">
            <h3 style="margin-top:0">镜头合成状态</h3>
            <div class="list">
              <div v-for="sb in storyboards" :key="'c'+sb.id" class="list-item" style="font-size:13px">
                <div class="row" style="justify-content:space-between">
                  <span>#{{ sb.storyboard_number }} {{ sb.title || '镜头' }}</span>
                  <span class="muted">
                    {{ sb.composed_video_url ? '已合成' : (sb.video_url ? '有视频' : '缺视频') }}
                    · {{ sb.tts_audio_url ? '有配音' : '无配音' }}
                  </span>
                </div>
                <video v-if="sb.composed_video_url" class="media" style="width:100%;margin-top:8px" :src="sb.composed_video_url" controls />
              </div>
            </div>
          </div>
          <div class="split-2" style="margin-top:16px">
            <div>
              <h3 style="margin-top:0">素材库</h3>
              <div class="list">
                <div v-for="asset in assets" :key="asset.id" class="list-item">
                  <div class="row" style="justify-content:space-between;align-items:center">
                    <span>{{ asset.name }}</span>
                    <span class="muted">{{ asset.type }} · {{ asset.category || '未分类' }}</span>
                  </div>
                  <img v-if="asset.mime_type?.startsWith('image/')" class="thumb" :src="asset.url" style="margin-top:8px" />
                  <a :href="asset.url" target="_blank" class="muted" style="font-size:12px;display:block;margin-top:6px">打开素材</a>
                  <div v-if="asset.type === 'image' || asset.mime_type?.startsWith('image/')" class="row" style="margin-top:8px;align-items:center">
                    <select v-model.number="assetTargetShot[asset.id]" aria-label="目标分镜">
                      <option :value="undefined">选择分镜</option>
                      <option v-for="sb in storyboards" :key="sb.id" :value="sb.id">#{{ sb.storyboard_number }} {{ sb.title || '镜头' }}</option>
                    </select>
                    <select v-model="assetTargetFrame[asset.id]" aria-label="目标帧">
                      <option value="first_frame">首帧</option>
                      <option value="last_frame">尾帧</option>
                      <option value="composed">分镜板</option>
                    </select>
                    <button class="btn" @click="applyAsset(asset)">复用到分镜</button>
                  </div>
                </div>
                <div v-if="!assets.length" class="empty">本集暂无统一素材记录</div>
              </div>
            </div>
            <div>
              <h3 style="margin-top:0">任务状态</h3>
              <div class="list">
                <div v-for="job in jobs" :key="job.id" class="list-item">
                  <div class="row" style="justify-content:space-between;align-items:center">
                    <span>#{{ job.id }} {{ job.kind }}</span>
                    <span class="muted">{{ job.status }}</span>
                  </div>
                  <div class="muted" style="font-size:12px;margin-top:6px">进度 {{ job.progress || 0 }}% · {{ job.last_error || '无错误' }}</div>
                  <button v-if="!['succeeded','failed','canceled'].includes(job.status)" class="btn btn-danger" style="margin-top:8px" @click="cancelJob(job)">取消任务</button>
                  <button v-if="['failed','canceled'].includes(job.status)" class="btn" style="margin-top:8px" @click="retryJob(job)">重试任务</button>
                </div>
                <div v-if="!jobs.length" class="empty">暂无任务记录</div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="log" class="panel" style="margin-top:16px">
          <h3 style="margin-top:0">Agent / 任务输出</h3>
          <pre class="log-box">{{ log }}</pre>
        </div>
      </div>
    </div>

    <div v-if="toast" class="toast">{{ toast }}</div>
    <div v-if="busy" class="toast" style="left:16px;right:auto">处理中：{{ busy }}</div>
  </div>
  <div v-else-if="loading" class="page">
    <div class="empty" role="status" aria-live="polite">正在加载本集…</div>
  </div>
  <div v-else class="page">
    <div class="panel load-error" role="alert">
      <h2>无法加载本集</h2>
      <p class="muted">{{ loadError || '剧集不存在' }}</p>
      <button class="btn btn-primary" type="button" @click="loadWorkbench">重新加载</button>
    </div>
  </div>
</template>
