<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Plus, RefreshCw, Settings2, WandSparkles } from 'lucide-vue-next'
import {
  agentAPI, characterAPI, composeAPI, dramaAPI, episodeAPI, gridAPI,
  mergeAPI, sceneAPI, settingsAPI, storyboardAPI
  , assetAPI, jobsAPI, productionAPI, uploadAPI
} from '../api'
import { authStore } from '../auth'

const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const episode = ref<any>(null)
const characters = ref<any[]>([])
const scenes = ref<any[]>([])
const storyboards = ref<any[]>([])
const status = ref<any>(null)
const voices = ref<any[]>([])
const configs = ref<any[]>([])
const promptTemplates = ref<any[]>([])
const gridHist = ref<any[]>([])
type WorkbenchStage = 'script' | 'cast' | 'grid' | 'boards' | 'export'
const workbenchStages: WorkbenchStage[] = ['script', 'cast', 'grid', 'boards', 'export']
const initialStage = workbenchStages.includes(String(route.query.stage) as WorkbenchStage)
  ? String(route.query.stage) as WorkbenchStage
  : 'script'
const tab = ref<WorkbenchStage>(initialStage)
const rawContent = ref('')
const busy = ref('')
const log = ref('')
const toast = ref('')
const loading = ref(true)
const loadError = ref('')
const assetRefreshWarning = ref('')
const settingsLoadWarning = ref('')
const refreshWarning = computed(() => [assetRefreshWarning.value, settingsLoadWarning.value].filter(Boolean).join('；'))
const pollTimer = ref<number | null>(null)

// grid state
const gridRows = ref(2)
const gridCols = ref(2)
const gridMode = ref('first_frame')
const gridPrompt = ref('')
const gridCells = ref<string[]>([])
type GridCellAssignment = { cell_index: number; storyboard_id: number; frame_type: string }
type GridCellTarget = { storyboard_id: number; frame_type: string }
const gridAssignments = ref<GridCellAssignment[]>([])
const gridCellsVerified = ref(false)
const gridCellTargets = ref<Record<number, GridCellTarget>>({})
const gridCellErrors = ref<Record<number, string>>({})
const assigningGridCell = ref<number | null>(null)
let gridContextVersion = 0
const gridImage = ref('')
const gridHistoryId = ref<number | null>(null)
const selectedShotIds = ref<number[]>([])
const shotSelectionInitialized = ref(false)
const assets = ref<any[]>([])
const jobs = ref<any[]>([])
const productions = ref<any[]>([])
const assetTargetShot = ref<Record<number, number>>({})
const assetTargetFrame = ref<Record<number, string>>({})

// voice assign panel
const assignCharId = ref<number | null>(null)
const characterForm = ref<any | null>(null)
const sceneForm = ref<any | null>(null)
const sceneTransfer = ref<any | null>(null)
const storyboardForm = ref<any | null>(null)
const selectedStoryboardId = ref<number | null>(null)
const promptEditor = ref<any | null>(null)
const episodeConfigForm = ref<any | null>(null)
const showProductionModal = ref(false)
const productionError = ref('')
const characterError = ref('')
const sceneError = ref('')
const storyboardError = ref('')
const characterNameInput = ref<HTMLInputElement | null>(null)
const sceneLocationInput = ref<HTMLInputElement | null>(null)
const storyboardTitleInput = ref<HTMLInputElement | null>(null)

const dramaId = computed(() => Number(route.params.id))
const episodeNumber = computed(() => Number(route.params.episodeNumber))
const promptEditorTemplates = computed(() => promptEditor.value
  ? promptTemplates.value.filter((template) => template.is_active !== false && template.category === promptEditor.value.category)
  : [])
const hasActiveJobs = computed(() => jobs.value.some((job) => !['succeeded', 'failed', 'canceled'].includes(job.status)))
const currentProduction = computed(() => productions.value[0] || null)
const selectedStoryboard = computed(() => storyboards.value.find((item) => item.id === selectedStoryboardId.value) || storyboards.value[0] || null)
const hasActiveProduction = computed(() => currentProduction.value?.status === 'queued')
const canEdit = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const gridSelectionError = computed(() => {
  if (gridMode.value !== 'first_last') return ''
  const capacity = gridRows.value * gridCols.value
  if (capacity % 2 !== 0) return '首尾参考需要偶数宫格'
  const required = capacity / 2
  return selectedShotIds.value.length === required
    ? ''
    : `首尾参考需选择 ${required} 个镜头，当前已选 ${selectedShotIds.value.length} 个`
})
const productionServices = computed(() => ['text', 'image', 'video', 'audio'].map((type) => {
  const candidates = configs.value.filter((item) => item.service_type === type && item.is_active !== false)
  const boundConfigID = type === 'text' ? 0 : Number(episode.value?.[`${type}_config_id`] || 0)
  const config = type === 'text'
    ? candidates.find((item) => item.is_default) || candidates[0]
    : candidates.find((item) => Number(item.id) === boundConfigID)
  return { type, label: ({ text: '文本', image: '图片', video: '视频', audio: '音频' } as Record<string, string>)[type], config }
}))
const missingProductionServices = computed(() => productionServices.value.filter((item) => !item.config))
const productionReady = computed(() => missingProductionServices.value.length === 0)
const productionUsesExternalService = computed(() => productionServices.value.some((item) => item.config && item.config.provider !== 'mock'))
const stages = computed(() => [
  { id: 'script', label: '剧本', detail: status.value?.has_script ? '已就绪' : '待完成', complete: Boolean(status.value?.has_script) },
  { id: 'cast', label: '角色与场景', detail: `${status.value?.characters || 0} 角色 · ${status.value?.scenes || 0} 场景`, complete: Boolean(status.value?.characters && status.value?.scenes) },
  { id: 'grid', label: '宫格帧', detail: `${status.value?.storyboards || 0} 个镜头`, complete: Boolean(status.value?.storyboards) },
  { id: 'boards', label: '分镜与视频', detail: `${status.value?.with_video || 0} 个视频`, complete: Boolean(status.value?.with_video) },
  { id: 'export', label: '合成导出', detail: episode.value?.video_url ? '成片完成' : `${status.value?.composed || 0} 个已合成`, complete: Boolean(episode.value?.video_url) },
])
const currentStageIndex = computed(() => Math.max(0, stages.value.findIndex((stage) => stage.id === tab.value)))
const nextStage = computed(() => stages.value[currentStageIndex.value + 1] || null)

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

function listOf(value: unknown): any[] {
  return Array.isArray(value) ? value.filter(Boolean) : []
}

function parseJSONList<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value.filter(Boolean) as T[]
  if (typeof value !== 'string' || !value.trim()) return []
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? parsed.filter(Boolean) as T[] : []
  } catch {
    return []
  }
}

function defaultGridAssignments(history: any, cells: string[]): GridCellAssignment[] {
  const storyboardIds = parseJSONList<number>(history.storyboard_ids).map(Number).filter((id) => id > 0)
  if (history.mode === 'first_last') {
    const half = Math.floor(storyboardIds.length / 2)
    if (!half) return []
    return cells.slice(0, storyboardIds.length).map((_, index) => ({
      cell_index: index,
      storyboard_id: storyboardIds[index % half],
      frame_type: index >= Math.floor(cells.length / 2) ? 'last_frame' : 'first_frame',
    }))
  }
  return cells.slice(0, storyboardIds.length).map((_, index) => ({
    cell_index: index,
    storyboard_id: storyboardIds[index],
    frame_type: 'first_frame',
  }))
}

function resetGridOutput() {
  gridContextVersion += 1
  gridHistoryId.value = null
  gridImage.value = ''
  gridCells.value = []
  gridAssignments.value = []
  gridCellsVerified.value = false
  gridCellTargets.value = {}
  gridCellErrors.value = {}
  assigningGridCell.value = null
}

function selectGridMode(mode: string) {
  if (busy.value) return
  if (gridMode.value === mode) return
  resetGridOutput()
  gridMode.value = mode
}

function updateGridDimension(field: 'rows' | 'cols', raw: number) {
  if (busy.value) return
  const value = Math.max(1, Math.min(4, Number.isFinite(raw) ? Math.round(raw) : 2))
  const target = field === 'rows' ? gridRows : gridCols
  if (target.value === value) return
  resetGridOutput()
  target.value = value
}

function setGridAssignments(assignments: GridCellAssignment[]) {
  const normalized = assignments
    .filter((item) => Number.isInteger(Number(item.cell_index)) && Number(item.cell_index) >= 0 && Number(item.storyboard_id) > 0)
    .map((item) => ({
      cell_index: Number(item.cell_index),
      storyboard_id: Number(item.storyboard_id),
      frame_type: ['first_frame', 'last_frame', 'composed'].includes(item.frame_type) ? item.frame_type : 'first_frame',
    }))
  gridAssignments.value = normalized
  gridCellTargets.value = Object.fromEntries(normalized.map((item) => [item.cell_index, {
    storyboard_id: item.storyboard_id,
    frame_type: item.frame_type,
  }]))
}

function loadGridHistory(history: any) {
  if (busy.value || assigningGridCell.value !== null) return
  gridContextVersion += 1
  const cells = parseJSONList<string>(history.cells_json).filter((url) => typeof url === 'string' && url.trim())
  gridImage.value = history.image_url || ''
  gridHistoryId.value = Number(history.id) || null
  gridRows.value = Number(history.rows) || 2
  gridCols.value = Number(history.cols) || 2
  gridMode.value = history.mode || 'first_frame'
  gridPrompt.value = history.prompt || gridPrompt.value
  gridCells.value = cells
  gridCellsVerified.value = history.cells_verified === true
  gridCellErrors.value = {}
  assigningGridCell.value = null
  const stored = parseJSONList<GridCellAssignment>(history.assignments_json)
  setGridAssignments(stored.length ? stored : defaultGridAssignments(history, cells))
}

function gridCellTarget(index: number): GridCellTarget {
  return gridCellTargets.value[index] || { storyboard_id: 0, frame_type: 'first_frame' }
}

function updateGridCellTarget(index: number, patch: Partial<GridCellTarget>) {
  gridCellTargets.value = {
    ...gridCellTargets.value,
    [index]: { ...gridCellTarget(index), ...patch },
  }
  gridCellErrors.value = { ...gridCellErrors.value, [index]: '' }
}

function gridFrameLabel(frameType: string) {
  return ({ first_frame: '首帧', last_frame: '尾帧', composed: '分镜板' } as Record<string, string>)[frameType] || frameType
}

function gridAssignmentLabel(index: number) {
  const assignment = gridAssignments.value.find((item) => item.cell_index === index)
  const storyboard = storyboards.value.find((item) => item.id === assignment?.storyboard_id)
  if (!assignment || !storyboard) return '尚未分配'
  return `镜头 #${storyboard.storyboard_number} · ${gridFrameLabel(assignment.frame_type)}`
}

async function load() {
  const loadedDrama = await dramaAPI.get(dramaId.value)
  const loadedEpisode = (loadedDrama.episodes || []).find((e: any) => e.episode_number === episodeNumber.value)
  drama.value = loadedDrama
  if (!loadedEpisode) {
    episode.value = null
    return
  }
  episode.value = loadedEpisode
  rawContent.value = loadedEpisode.content || ''
  await refreshAssets()
  const settingsResults = await Promise.allSettled([
    settingsAPI.voices(),
    settingsAPI.aiConfigs(),
    settingsAPI.promptTemplates(),
  ])
  if (settingsResults[0].status === 'fulfilled') voices.value = listOf(settingsResults[0].value)
  if (settingsResults[1].status === 'fulfilled') configs.value = listOf(settingsResults[1].value)
  if (settingsResults[2].status === 'fulfilled') promptTemplates.value = listOf(settingsResults[2].value)
	const settingsLabels = ['音色', 'AI 配置', '提示词']
	settingsLoadWarning.value = settingsResults.flatMap((result, index) => {
		if (result.status === 'fulfilled') return []
		const detail = result.reason instanceof Error ? result.reason.message : '服务暂时不可用'
		return [`${settingsLabels[index]}加载失败：${detail}`]
	}).join('；')
}

async function loadWorkbench() {
  stopPoll()
  loading.value = true
  loadError.value = ''
  try {
    await load()
    if (!episode.value) loadError.value = '未找到对应剧集'
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : '加载失败'
  } finally {
    loading.value = false
  }
  if (episode.value) startPoll()
}

async function refreshAssets() {
  const currentEpisode = episode.value
  if (!currentEpisode) return
  const episodeId = currentEpisode.id
  const results = await Promise.allSettled([
    episodeAPI.characters(episodeId),
    episodeAPI.scenes(episodeId),
    episodeAPI.storyboards(episodeId),
    episodeAPI.pipelineStatus(episodeId),
    gridAPI.history({ episode_id: episodeId }),
    assetAPI.list({ episode_id: episodeId }),
    jobsAPI.list({ limit: 50 }),
    productionAPI.list(episodeId, 10),
    dramaAPI.get(dramaId.value),
  ])
  const labels = ['角色', '场景', '分镜', '制作进度', '宫格历史', '素材', '任务', '自动制作', '剧集']
  const failures: string[] = []
  results.forEach((result, index) => {
    if (result.status === 'rejected') {
      const detail = result.reason instanceof Error ? result.reason.message : '服务暂时不可用'
      failures.push(`${labels[index]}加载失败：${detail}`)
    }
  })
  if (results[0].status === 'fulfilled') characters.value = listOf(results[0].value)
  if (results[1].status === 'fulfilled') scenes.value = listOf(results[1].value)
  if (results[2].status === 'fulfilled') {
    const nextStoryboards = listOf(results[2].value)
    storyboards.value = nextStoryboards
    const selectedExists = nextStoryboards.some((item) => item.id === selectedStoryboardId.value)
    if (!selectedExists) selectedStoryboardId.value = nextStoryboards[0]?.id || null
    if (!shotSelectionInitialized.value) {
      selectedShotIds.value = nextStoryboards.map((item) => item.id)
      shotSelectionInitialized.value = true
    } else {
      selectedShotIds.value = selectedShotIds.value.filter((id) => nextStoryboards.some((item) => item.id === id))
    }
  }
  if (results[3].status === 'fulfilled') status.value = results[3].value
  if (results[4].status === 'fulfilled') gridHist.value = listOf(results[4].value)
  if (results[5].status === 'fulfilled') assets.value = listOf(results[5].value)
  if (results[6].status === 'fulfilled') jobs.value = listOf(results[6].value)
  if (results[7].status === 'fulfilled') productions.value = listOf(results[7].value)
  if (results[8].status === 'fulfilled') {
    const refreshedDrama = results[8].value as any
    const refreshedEpisode = (refreshedDrama.episodes || []).find((item: any) => item.episode_number === episodeNumber.value)
    drama.value = refreshedDrama
    if (refreshedEpisode) episode.value = refreshedEpisode
  }
  assetRefreshWarning.value = failures.join('；')
}

function startPoll() {
  stopPoll()
  pollTimer.value = window.setInterval(() => {
    if (document.visibilityState === 'visible' && (hasActiveJobs.value || hasActiveProduction.value)) refreshAssets().catch(() => {})
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

function openEpisodeConfig() {
  const firstConfig = (serviceType: string) => configs.value.find((item) => item.service_type === serviceType)?.id || 0
  episodeConfigForm.value = {
    image_config_id: episode.value.image_config_id || firstConfig('image'),
    video_config_id: episode.value.video_config_id || firstConfig('video'),
    audio_config_id: episode.value.audio_config_id || firstConfig('audio'),
  }
}

async function saveEpisodeConfig() {
  const form = episodeConfigForm.value
  if (!form?.image_config_id || !form?.video_config_id || !form?.audio_config_id) {
    show('请选择图片、视频和音频配置')
    return
  }
  busy.value = 'episode-config'
  try {
    await episodeAPI.update(episode.value.id, { ...form })
    episodeConfigForm.value = null
    show('生成配置已更新')
    await load()
  } catch (error: any) {
    show(error.message || '生成配置保存失败')
  } finally {
    busy.value = ''
  }
}

function openProduction() {
  productionError.value = productionReady.value ? '' : `请先绑定${missingProductionServices.value.map((item) => item.label).join('、')}服务`
  showProductionModal.value = true
}

async function startProduction() {
  if (!productionReady.value) {
    productionError.value = `请先绑定${missingProductionServices.value.map((item) => item.label).join('、')}服务`
    return
  }
  busy.value = 'production-start'
  productionError.value = ''
  try {
    const run = await productionAPI.create(dramaId.value, episode.value.id)
    productions.value = [run, ...productions.value.filter((item) => item.id !== run.id)]
    showProductionModal.value = false
    show('自动制作已开始')
    startPoll()
  } catch (error) {
    productionError.value = error instanceof Error ? error.message : '自动制作启动失败'
  } finally {
    busy.value = ''
  }
}

async function cancelProduction() {
  const run = currentProduction.value
  if (!run) return
  busy.value = 'production-cancel'
  try {
    const updated = await productionAPI.cancel(run.id)
    productions.value = [updated, ...productions.value.filter((item) => item.id !== updated.id)]
    show('自动制作已取消')
  } catch (error: any) {
    show(error.message || '取消失败')
  } finally {
    busy.value = ''
  }
}

async function retryProduction() {
  const run = currentProduction.value
  if (!run) return
  busy.value = 'production-retry'
  try {
    const updated = await productionAPI.retry(run.id)
    productions.value = [updated, ...productions.value.filter((item) => item.id !== updated.id)]
    show('自动制作已重新排队')
    startPoll()
  } catch (error: any) {
    show(error.message || '重试失败')
  } finally {
    busy.value = ''
  }
}

function productionStageLabel(stage?: string) {
  return ({ script: '剧本生成', extract: '角色场景提取', storyboards: '分镜拆解', frames: '首帧生成', videos: '视频生成', tts: '对白配音', compose: '镜头合成', merge: '成片导出', completed: '制作完成' } as Record<string, string>)[stage || ''] || '准备中'
}

function productionStatusLabel(status?: string) {
  return ({ queued: '制作中', succeeded: '已完成', failed: '失败', canceled: '已取消' } as Record<string, string>)[status || ''] || status || '等待中'
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
    const result = await characterAPI.batchImages(ids, episode.value.id)
    const errors = Array.isArray(result?.errors) ? result.errors : []
    if (errors.length) show(`批量角色图完成 ${result?.count || 0} 项，失败 ${errors.length}：${errors.slice(0, 3).join('；')}`)
    else show(`批量角色图已提交 ${result?.count || 0} 项`)
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

async function editCharacter(character?: any) {
  characterError.value = ''
  characterForm.value = character
    ? { id: character.id, name: character.name, role: character.role || '', appearance: character.appearance || '', description: character.description || '', personality: character.personality || '' }
    : { name: '', role: '', appearance: '', description: '', personality: '' }
  await nextTick()
  characterNameInput.value?.focus()
}

async function saveCharacter() {
  const form = characterForm.value
  characterError.value = ''
  if (!form?.name?.trim()) {
    characterError.value = '请输入角色名'
    characterNameInput.value?.focus()
    return
  }
  busy.value = 'character-save'
  try {
    const data = { ...form, drama_id: dramaId.value, episode_id: episode.value.id }
    if (form.id) await characterAPI.update(form.id, data)
    else await characterAPI.create(data)
    characterForm.value = null
    show(form.id ? '角色已更新' : '角色已添加')
    await refreshAssets()
  } catch (e: any) { characterError.value = e.message || '角色保存失败' } finally { busy.value = '' }
}

async function removeCharacter(character: any) {
  if (!window.confirm(`删除角色“${character.name}”？`)) return
  try {
    await characterAPI.del(character.id)
    show('角色已删除')
    await refreshAssets()
  } catch (e: any) { show(e.message) }
}

async function editScene(scene?: any) {
  sceneError.value = ''
  sceneForm.value = scene
    ? { id: scene.id, location: scene.location, time: scene.time || '', prompt: scene.prompt || '' }
    : { location: '', time: '', prompt: '' }
  await nextTick()
  sceneLocationInput.value?.focus()
}

async function saveScene() {
  const form = sceneForm.value
  sceneError.value = ''
  if (!form?.location?.trim()) {
    sceneError.value = '请输入场景地点'
    sceneLocationInput.value?.focus()
    return
  }
  busy.value = 'scene-save'
  try {
    const data = { ...form, drama_id: dramaId.value, episode_id: episode.value.id }
    if (form.id) await sceneAPI.update(form.id, data)
    else await sceneAPI.create(data)
    sceneForm.value = null
    show(form.id ? '场景已更新' : '场景已添加')
    await refreshAssets()
  } catch (e: any) { sceneError.value = e.message || '场景保存失败' } finally { busy.value = '' }
}

async function removeScene(scene: any) {
  if (!window.confirm(`删除场景“${scene.location}”？`)) return
  try {
    await sceneAPI.del(scene.id)
    show('场景已删除')
    await refreshAssets()
  } catch (e: any) { show(e.message) }
}

function transferScene(scene: any, mode: 'copy' | 'move') {
  const target = (drama.value?.episodes || []).find((item: any) => item.id !== episode.value.id)
  sceneTransfer.value = { scene, mode, target_episode_id: target?.id || 0, move_storyboards: true }
}

async function confirmSceneTransfer() {
  const transfer = sceneTransfer.value
  if (!transfer?.target_episode_id) {
    show('请选择目标剧集')
    return
  }
  busy.value = `scene-${transfer.mode}`
  try {
    if (transfer.mode === 'copy') await sceneAPI.copy(transfer.scene.id, transfer.target_episode_id)
    else await sceneAPI.move(transfer.scene.id, transfer.target_episode_id, { move_storyboards: transfer.move_storyboards })
    show(transfer.mode === 'copy' ? '场景已复制' : '场景已迁移')
    sceneTransfer.value = null
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
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
  if (!selectedShotIds.value.length) return show('请至少选择一个镜头')
  busy.value = 'batch-frames'
  try {
    const result = await storyboardAPI.batchFrames({
      episode_id: episode.value.id,
      frame_type: frameType,
      storyboard_ids: selectedShotIds.value,
    })
    const errors = Array.isArray(result?.errors) ? result.errors : []
    if (errors.length) show(`批量帧完成 ${result?.count || 0} 项，失败 ${errors.length}：${errors.slice(0, 3).join('；')}`)
    else show(`批量帧生成已提交 ${result?.count || 0} 项`)
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
  if (!selectedShotIds.value.length) return show('请至少选择一个镜头')
  busy.value = 'batch-videos'
  try {
    const result = await storyboardAPI.batchVideos({
      episode_id: episode.value.id,
      storyboard_ids: selectedShotIds.value,
    })
    const errors = Array.isArray(result?.errors) ? result.errors : []
    if (errors.length) show(`批量视频完成 ${result?.count || 0} 项，失败 ${errors.length}：${errors.slice(0, 3).join('；')}`)
    else show(`批量视频已提交 ${result?.count || 0} 项`)
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
  if (!selectedShotIds.value.length) return show('请至少选择一个镜头')
  busy.value = 'batch-tts'
  try {
    const res = await storyboardAPI.batchTTS({
      episode_id: episode.value.id,
      storyboard_ids: selectedShotIds.value,
    })
    show(`配音任务已排队 ok=${res.ok} skipped=${res.skipped || 0} fail=${res.fail}`)
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
  if (gridSelectionError.value) {
    show(gridSelectionError.value)
    return
  }
  if (!gridPrompt.value.trim()) {
    await buildGridPrompt()
  }
  busy.value = 'grid-gen'
  try {
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    resetGridOutput()
    busy.value = 'grid-gen'
    const contextVersion = gridContextVersion
    const res = await gridAPI.generate({
      prompt: gridPrompt.value,
      drama_id: dramaId.value,
      episode_id: episode.value.id,
      mode: gridMode.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
    })
    if (gridContextVersion !== contextVersion) return
    const img = res.image || res
    const hist = res.history
    gridImage.value = img.image_url || ''
    gridHistoryId.value = hist?.id || null
    if (img.id && !gridImage.value) {
      // poll image status
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000))
        if (gridContextVersion !== contextVersion) return
        const st = await gridAPI.status(img.id)
        if (gridContextVersion !== contextVersion) return
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
  if (gridSelectionError.value) {
    show(gridSelectionError.value)
    return
  }
  busy.value = 'grid-split'
  try {
    const contextVersion = gridContextVersion
    const historyID = gridHistoryId.value
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    const res = await gridAPI.split({
      image_url: gridImage.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
      frame_type: gridMode.value === 'first_last' ? 'first_last' : 'first_frame',
      history_id: gridHistoryId.value || undefined,
    })
    if (gridContextVersion !== contextVersion || gridHistoryId.value !== historyID) return
    gridCells.value = res.cells || []
    gridCellsVerified.value = res.cells_verified === true
    gridCellErrors.value = {}
    setGridAssignments(listOf(res.assignments) as GridCellAssignment[])
    show(`已切分 ${res.count} 格并写回分镜`)
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function assignGridCell(index: number) {
  const target = gridCellTarget(index)
  if (!gridHistoryId.value) {
    show('请先载入或切分已保存的宫格')
    return
  }
  if (!target.storyboard_id) {
    show('请选择目标镜头')
    return
  }
  const historyID = gridHistoryId.value
  const contextVersion = gridContextVersion
  assigningGridCell.value = index
  gridCellErrors.value = { ...gridCellErrors.value, [index]: '' }
  try {
    const result = await gridAPI.assignCell(historyID, {
      cell_index: index,
      storyboard_id: target.storyboard_id,
      frame_type: target.frame_type,
    })
    if (gridContextVersion !== contextVersion || gridHistoryId.value !== historyID) return
    setGridAssignments(listOf(result.assignments).length ? result.assignments : [result])
    const storyboard = storyboards.value.find((item) => item.id === target.storyboard_id)
    show(`切片 ${index + 1} 已写入镜头 #${storyboard?.storyboard_number || target.storyboard_id} ${gridFrameLabel(target.frame_type)}`)
    await refreshAssets()
  } catch (error) {
    if (gridContextVersion !== contextVersion || gridHistoryId.value !== historyID) return
    const message = error instanceof Error ? error.message : '切片分配失败'
    gridCellErrors.value = { ...gridCellErrors.value, [index]: message }
  } finally {
    if (gridContextVersion === contextVersion && gridHistoryId.value === historyID) assigningGridCell.value = null
  }
}

async function addStoryboard() {
  storyboardError.value = ''
  storyboardForm.value = {
    title: '',
    duration: 12,
    shot_type: '',
    description: '',
    dialogue: '',
    image_prompt: '',
    video_prompt: '',
    reference_images: '',
  }
  await nextTick()
  storyboardTitleInput.value?.focus()
}

async function saveStoryboard() {
  const form = storyboardForm.value
  storyboardError.value = ''
  if (!form?.title?.trim()) {
    storyboardError.value = '请输入分镜标题'
    storyboardTitleInput.value?.focus()
    return
  }
  busy.value = 'storyboard-save'
  try {
    await storyboardAPI.create({
      ...form,
      title: form.title.trim(),
      reference_images: JSON.stringify(form.reference_images.split(/\r?\n/).map((value: string) => value.trim()).filter(Boolean)),
      episode_id: episode.value.id,
    })
    storyboardForm.value = null
    show('分镜已添加')
    await refreshAssets()
  } catch (error: any) {
    storyboardError.value = error.message || '分镜保存失败'
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
    if (selectedStoryboardId.value === storyboard.id) selectedStoryboardId.value = null
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
    return true
  } catch (e: any) {
    show(e.message)
    return false
  } finally {
    busy.value = ''
  }
}

function openPromptEditor(sb: any, field: string, label: string) {
  const category = field === 'image_prompt' ? 'image' : field === 'video_prompt' ? 'video' : ''
  const matching = promptTemplates.value.filter((template) => template.is_active !== false && template.category === category)
  promptEditor.value = {
    target: 'storyboard', storyboard: sb, field, label, category,
    value: sb[field] || '', template_id: matching[0]?.id || 0, error: '',
  }
}

function openGridPromptEditor() {
  const matching = promptTemplates.value.filter((template) => template.is_active !== false && template.category === 'grid')
  promptEditor.value = {
    target: 'grid', storyboard: null, field: 'grid_prompt', label: '宫格提示词', category: 'grid',
    value: gridPrompt.value, template_id: matching[0]?.id || 0, error: '',
  }
}

function promptRuntimeVariables(editor: any) {
  const storyboard = editor.storyboard || {}
  return {
    drama_title: drama.value?.title || '', episode_title: episode.value?.title || '',
    user_instruction: editor.value || '', character_names: characters.value.map((item) => item.name).join('、'),
    scene_names: scenes.value.map((item) => `${item.location}${item.time ? `·${item.time}` : ''}`).join('、'),
    shot_title: storyboard.title || '', shot_description: storyboard.description || storyboard.action || '',
    image_prompt: storyboard.image_prompt || '', video_prompt: storyboard.video_prompt || '',
    grid_rows: String(gridRows.value), grid_cols: String(gridCols.value), grid_mode: gridMode.value,
  }
}

function selectedPromptVariables(editor: any) {
  const template = promptTemplates.value.find((item) => Number(item.id) === Number(editor.template_id))
  let names: string[] = []
  try { names = JSON.parse(template?.variables_json || '[]') } catch { names = [] }
  const runtime = promptRuntimeVariables(editor)
  return Object.fromEntries(names.map((name) => [name, runtime[name as keyof typeof runtime] || '']))
}

async function applySelectedPromptTemplate() {
  const editor = promptEditor.value
  if (!editor) return
  if (!editor.template_id) editor.template_id = promptEditorTemplates.value[0]?.id || 0
  if (!editor.template_id) {
    editor.error = '当前分类没有可用模板'
    return
  }
  editor.error = ''
  busy.value = 'prompt-preview'
  try {
    const result = await settingsAPI.previewPromptTemplate(editor.template_id, selectedPromptVariables(editor))
    editor.value = result.rendered
  } catch (error) {
    editor.error = error instanceof Error ? error.message : '模板应用失败'
  } finally {
    busy.value = ''
  }
}

async function savePromptEditor() {
  const editor = promptEditor.value
  if (!editor) return
  if (editor.target === 'grid') {
    gridPrompt.value = editor.value
    promptEditor.value = null
    show('宫格提示词已应用')
    return
  }
  if (await saveShot(editor.storyboard, { [editor.field]: editor.value })) promptEditor.value = null
}

function toggleShot(id: number) {
  selectedShotIds.value = selectedShotIds.value.includes(id)
    ? selectedShotIds.value.filter((item) => item !== id)
    : [...selectedShotIds.value, id]
}

function shotStatusDot(sb: any) {
  if (sb.composed_video_url) return 'ok'
  if (sb.video_url) return 'run'
  if (sb.status === 'failed') return 'fail'
  return ''
}

function storyboardStatusLabel(value?: string) {
  return ({ pending: '待制作', processing: '制作中', generated: '已生成', composed: '已合成', completed: '已完成', failed: '失败' } as Record<string, string>)[value || ''] || value || '待制作'
}

function selectStage(stage: string) {
  if (!workbenchStages.includes(stage as WorkbenchStage)) return
  tab.value = stage as WorkbenchStage
  router.replace({ query: { ...route.query, stage } })
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

watch(() => route.query.stage, (stage) => {
  const value = String(stage || '') as WorkbenchStage
  if (workbenchStages.includes(value) && value !== tab.value) tab.value = value
})

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
        <button v-if="canEdit" class="btn btn-primary" :disabled="hasActiveProduction || !!busy" @click="openProduction"><WandSparkles :size="16" aria-hidden="true" />自动制作</button>
        <button v-if="canEdit" class="btn" @click="openEpisodeConfig"><Settings2 :size="16" aria-hidden="true" />生成配置</button>
        <button class="btn" @click="loadWorkbench"><RefreshCw :size="16" aria-hidden="true" />刷新</button>
        <button class="btn" @click="router.push(`/drama/${dramaId}`)"><ArrowLeft :size="16" aria-hidden="true" />返回项目</button>
      </div>
    </div>

    <div v-if="refreshWarning" class="inline-alert" role="alert">
      <div><strong>部分内容暂未更新</strong><span>{{ refreshWarning }}</span></div>
      <button class="btn" type="button" @click="loadWorkbench">重试加载</button>
    </div>

    <section v-if="currentProduction" class="automation-status" aria-label="自动制作状态">
      <div class="automation-main">
        <div class="automation-heading">
          <span class="automation-mark" :class="currentProduction.status"></span>
          <div><strong>{{ productionStageLabel(currentProduction.stage) }}</strong><span>{{ productionStatusLabel(currentProduction.status) }} · 第 {{ currentProduction.attempt }} 次</span></div>
        </div>
        <div class="automation-progress"><div class="progress-bar"><i :style="`width: ${currentProduction.progress || 0}%`"></i></div><strong>{{ currentProduction.progress || 0 }}%</strong></div>
      </div>
      <div class="automation-footer">
        <span :class="{ 'job-error': currentProduction.last_error }">{{ currentProduction.last_error || currentProduction.status_message || '等待任务调度' }}</span>
        <div class="mini-actions">
          <button v-if="hasActiveProduction && canEdit" class="btn btn-danger" :disabled="!!busy" @click="cancelProduction">取消自动制作</button>
          <button v-if="['failed','canceled'].includes(currentProduction.status) && canEdit" class="btn" :disabled="!!busy || currentProduction.attempt >= currentProduction.max_attempts" @click="retryProduction">重试自动制作</button>
          <button class="btn" @click="router.push('/jobs')">查看任务</button>
        </div>
      </div>
    </section>

    <section class="production-overview" aria-label="制作进度">
      <div class="production-summary">
        <div class="production-stats">
          <span class="stat-chip">剧本 <strong>{{ status?.has_script ? '✓' : '—' }}</strong></span>
          <span class="stat-chip">角色 <strong>{{ status?.characters || 0 }}</strong></span>
          <span class="stat-chip">场景 <strong>{{ status?.scenes || 0 }}</strong></span>
          <span class="stat-chip">分镜 <strong>{{ status?.storyboards || 0 }}</strong></span>
          <span class="stat-chip">视频 <strong>{{ status?.with_video || 0 }}</strong></span>
          <span class="stat-chip">配音 <strong>{{ status?.with_tts || 0 }}</strong></span>
          <span class="stat-chip">合成 <strong>{{ status?.composed || 0 }}</strong></span>
        </div>
        <div class="production-progress">
          <div class="production-progress-label"><span>制作进度</span><strong>{{ progressPct }}%</strong></div>
          <div class="progress-bar"><i :style="`width: ${progressPct}%`"></i></div>
        </div>
      </div>

      <div class="stage-tabs" role="tablist" aria-label="制作阶段">
        <button
          v-for="(stage, index) in stages"
          :key="stage.id"
          class="stage-tab"
          :class="{ active: tab === stage.id, complete: stage.complete }"
          role="tab"
          :aria-label="stage.label"
          :aria-selected="tab === stage.id"
          @click="selectStage(stage.id)"
        >
          <span class="stage-index" aria-hidden="true">{{ stage.complete ? '✓' : index + 1 }}</span>
          <span class="stage-copy"><strong>{{ stage.label }}</strong><small>{{ stage.detail }}</small></span>
        </button>
      </div>
    </section>

    <div class="wb-shell">
      <div class="wb-main">
        <div class="stage-commandbar">
          <div><span>阶段 {{ currentStageIndex + 1 }} / {{ stages.length }}</span><strong>{{ stages[currentStageIndex].label }}</strong></div>
          <button v-if="nextStage" class="btn stage-next" @click="selectStage(nextStage.id)">下一步：{{ nextStage.label }}<ArrowRight :size="15" aria-hidden="true" /></button>
        </div>
        <!-- SCRIPT -->
        <div v-if="tab==='script'" class="panel">
          <div v-if="canEdit" class="toolbar">
            <button class="btn btn-primary" :disabled="!!busy" @click="saveContent">保存原文</button>
            <button class="btn" :disabled="!!busy" @click="runAgent('script_rewriter', '请将当前集内容改写为格式化剧本并保存')">AI 改写剧本</button>
          </div>
          <div class="split-2">
            <div class="field">
              <label>原始内容 / 大纲</label>
              <textarea v-model="rawContent" rows="18" :readonly="!canEdit" placeholder="粘贴小说、大纲或分场内容…" />
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
            <div v-if="canEdit" class="toolbar">
              <button class="btn btn-primary" :disabled="!!busy" @click="runAgent('extractor', '请提取本集角色与场景并去重保存')">AI 提取</button>
              <button class="btn" :disabled="!!busy" @click="runAgent('voice_assigner', '请为所有角色分配音色')">AI 分配音色</button>
              <button class="btn" :disabled="!!busy || !characters.length" @click="batchCharImages">批量角色图</button>
              <button class="btn" :disabled="!!busy" @click="editCharacter()">添加角色</button>
            </div>
            <h3>角色</h3>
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
                    <img v-if="c.image_url" class="thumb" :src="c.image_url" :alt="`${c.name} 角色形象`" />
                    <div v-if="canEdit" class="mini-actions">
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
              <button v-if="canEdit" class="btn" :disabled="!!busy" @click="editScene()">添加场景</button>
            </div>
            <div class="list">
              <div v-for="sc in scenes" :key="sc.id" class="list-item">
                <div class="row" style="justify-content:space-between">
                  <div>
                    <h4>{{ sc.location }} · {{ sc.time }}</h4>
                    <p class="muted" style="margin:0;font-size:12px">{{ sc.prompt }}</p>
                  </div>
                  <div class="row">
                    <img v-if="sc.image_url" class="thumb" :src="sc.image_url" :alt="`${sc.location} 场景图`" />
                    <button v-if="canEdit" class="btn" :disabled="!!busy" @click="genSceneImage(sc)">生成场景</button>
                    <label v-if="canEdit" class="btn" :aria-disabled="!!busy">上传<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="!!busy" style="display:none" @change="uploadBoundImage('scene', sc, $event)" /></label>
                    <button v-if="canEdit" class="btn" :disabled="!!busy" @click="editScene(sc)">编辑</button>
                    <button v-if="canEdit" class="btn" :disabled="!!busy || (drama?.episodes || []).length < 2" @click="transferScene(sc, 'copy')">复制</button>
                    <button v-if="canEdit" class="btn" :disabled="!!busy || (drama?.episodes || []).length < 2" @click="transferScene(sc, 'move')">迁移</button>
                    <button v-if="canEdit" class="btn btn-danger" :disabled="!!busy" @click="removeScene(sc)">删除</button>
                  </div>
                </div>
              </div>
              <div v-if="!scenes.length" class="empty">尚未提取场景</div>
            </div>
            <div v-if="sceneTransfer" class="modal-mask" @click.self="sceneTransfer=null">
              <div class="modal" role="dialog" aria-modal="true" aria-labelledby="scene-transfer-title">
                <h3 id="scene-transfer-title">{{ sceneTransfer.mode === 'copy' ? '复制场景' : '迁移场景' }}</h3>
                <div class="field"><label for="scene-target-episode">目标剧集</label><select id="scene-target-episode" v-model.number="sceneTransfer.target_episode_id">
                  <option :value="0">请选择</option><option v-for="item in (drama?.episodes || []).filter((row:any) => row.id !== episode?.id)" :key="item.id" :value="item.id">第 {{ item.episode_number }} 集 · {{ item.title }}</option>
                </select></div>
                <label v-if="sceneTransfer.mode === 'move'" class="row" style="align-items:center"><input v-model="sceneTransfer.move_storyboards" type="checkbox" /> 同时迁移关联分镜</label>
                <div class="modal-actions"><button class="btn" @click="sceneTransfer=null">取消</button><button class="btn btn-primary" :disabled="!!busy" @click="confirmSceneTransfer">确认</button></div>
              </div>
            </div>
          </div>
        </div>

        <!-- GRID -->
        <div v-else-if="tab==='grid'" class="panel">
          <div v-if="canEdit" class="toolbar">
            <div class="seg">
              <button :disabled="!!busy" :class="{ active: gridMode==='first_frame' }" @click="selectGridMode('first_frame')">首帧宫格</button>
              <button :disabled="!!busy" :class="{ active: gridMode==='first_last' }" @click="selectGridMode('first_last')">首尾参考</button>
              <button :disabled="!!busy" :class="{ active: gridMode==='multi_ref' }" @click="selectGridMode('multi_ref')">多参一致</button>
            </div>
            <label class="muted" style="font-size:12px">行
              <input type="number" min="1" max="4" :value="gridRows" :disabled="!!busy" style="width:52px;margin-left:4px" @change="updateGridDimension('rows', Number(($event.target as HTMLInputElement).value))" />
            </label>
            <label class="muted" style="font-size:12px">列
              <input type="number" min="1" max="4" :value="gridCols" :disabled="!!busy" style="width:52px;margin-left:4px" @change="updateGridDimension('cols', Number(($event.target as HTMLInputElement).value))" />
            </label>
            <button class="btn" :disabled="!!busy" @click="buildGridPrompt">生成提示词</button>
            <button class="btn" :disabled="!!busy" @click="openGridPromptEditor">套用提示词模板</button>
            <button class="btn btn-primary" :disabled="!!busy || !!gridSelectionError" :title="gridSelectionError || undefined" @click="generateGrid">生成宫格图</button>
            <button class="btn" :disabled="!!busy || !gridImage || !!gridCells.length" @click="splitGrid">{{ gridCells.length ? '已切分，可在下方重新分配' : '切分写回分镜' }}</button>
            <button class="btn" :disabled="!!busy" @click="runAgent('grid_prompt_generator', '请为本集镜头生成宫格首帧提示词')">Agent 提示词</button>
            <span v-if="gridSelectionError" class="muted grid-selection-hint" role="status">{{ gridSelectionError }}</span>
          </div>
          <div class="field">
            <label for="grid-prompt">宫格提示词</label>
            <textarea id="grid-prompt" v-model="gridPrompt" rows="8" :readonly="!canEdit" placeholder="可先点击「生成提示词」" />
          </div>
          <div class="split-2">
            <div>
              <div class="muted" style="margin-bottom:8px;font-size:12px">预览</div>
              <img v-if="gridImage" class="grid-preview" :src="gridImage" alt="宫格图预览" />
              <div v-else class="empty">尚未生成宫格图</div>
              <div v-if="gridCells.length" class="cell-grid" style="margin-top:12px">
                <div v-if="!gridCellsVerified" class="inline-alert grid-cell-legacy" role="status"><div><strong>历史切片仅供查看</strong><span>这份历史生成于安全归属记录之前。请生成新宫格并重新切分后再分配。</span></div></div>
                <div v-for="(u,i) in gridCells" :key="`${gridHistoryId || 'draft'}-${i}`" class="grid-cell-card" role="group" :aria-label="`宫格切片 ${i + 1}`">
                  <div class="grid-cell-visual"><span>#{{ i + 1 }}</span><img :src="u" :alt="`宫格切片 ${i + 1}`" /></div>
                  <p class="grid-cell-assignment">{{ gridAssignmentLabel(i) }}</p>
                  <p v-if="gridCellErrors[i]" class="grid-cell-error" role="alert">{{ gridCellErrors[i] }}</p>
                  <div v-if="canEdit" class="grid-cell-controls">
                    <label><span>目标镜头</span><select :value="gridCellTarget(i).storyboard_id" aria-label="目标镜头" :disabled="!gridCellsVerified || assigningGridCell !== null" @change="updateGridCellTarget(i, { storyboard_id: Number(($event.target as HTMLSelectElement).value) })"><option :value="0">请选择</option><option v-for="sb in storyboards" :key="sb.id" :value="sb.id">#{{ sb.storyboard_number }} {{ sb.title || '镜头' }}</option></select></label>
                    <label><span>写入位置</span><select :value="gridCellTarget(i).frame_type" aria-label="写入位置" :disabled="!gridCellsVerified || assigningGridCell !== null" @change="updateGridCellTarget(i, { frame_type: ($event.target as HTMLSelectElement).value })"><option value="first_frame">首帧</option><option value="last_frame">尾帧</option><option value="composed">分镜板</option></select></label>
                    <button class="btn" type="button" :disabled="assigningGridCell !== null || !gridHistoryId || !gridCellsVerified || !gridCellTarget(i).storyboard_id" @click="assignGridCell(i)">{{ assigningGridCell === i ? '分配中…' : '重新分配' }}</button>
                  </div>
                </div>
              </div>
            </div>
            <div>
              <div class="muted" style="margin-bottom:8px;font-size:12px">写入分镜（勾选）</div>
              <div class="list">
                <label v-for="sb in storyboards" :key="sb.id" class="list-item" style="display:flex;gap:10px;align-items:center;cursor:pointer">
                  <input v-if="canEdit" type="checkbox" :checked="selectedShotIds.includes(sb.id)" @change="toggleShot(sb.id)" />
                  <span style="flex:1">#{{ sb.storyboard_number }} {{ sb.title || '镜头' }}</span>
                  <img v-if="sb.first_frame_image" class="thumb" style="width:48px;height:48px" :src="sb.first_frame_image" :alt="`镜头 ${sb.storyboard_number} 首帧`" />
                </label>
                <div v-if="!storyboards.length" class="empty">请先拆解分镜</div>
              </div>
              <div style="margin-top:12px">
                <div class="muted" style="font-size:12px;margin-bottom:6px">历史</div>
                <div class="list">
                  <div v-for="h in gridHist" :key="h.id" class="list-item" style="font-size:12px">
                    #{{ h.id }} · {{ h.mode }} · {{ h.rows }}x{{ h.cols }} · {{ h.status }}
                    <button v-if="h.image_url" class="btn" style="margin-top:6px" :disabled="!!busy || assigningGridCell !== null" :aria-label="`载入宫格 #${h.id}`" @click="loadGridHistory(h)">载入</button>
                  </div>
                  <div v-if="!gridHist.length" class="muted">暂无历史</div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- BOARDS -->
        <div v-else-if="tab==='boards'" class="panel">
          <div v-if="canEdit" class="toolbar">
            <button class="btn btn-primary" :disabled="!!busy" @click="runAgent('storyboard_breaker', '请根据当前剧本完整拆解分镜并保存')">AI 拆解分镜</button>
            <button class="btn" :disabled="!!busy" @click="addStoryboard"><Plus :size="15" aria-hidden="true" />添加分镜</button>
            <button class="btn" :disabled="!!busy || !selectedShotIds.length" @click="batchFrames('first_frame')">批量首帧</button>
            <button class="btn" :disabled="!!busy || !selectedShotIds.length" @click="batchFrames('last_frame')">批量尾帧</button>
            <button class="btn" :disabled="!!busy || !selectedShotIds.length" @click="batchFrames('composed')">批量分镜板</button>
            <button class="btn" :disabled="!!busy || !selectedShotIds.length" @click="batchVideos">批量视频</button>
            <button class="btn" :disabled="!!busy || !selectedShotIds.length" @click="batchTTS">批量配音</button>
          </div>
          <div v-if="storyboards.length" class="storyboard-workspace">
            <aside class="storyboard-rail">
              <div class="storyboard-rail-head"><strong>镜头列表</strong><span>{{ storyboards.length }} 个镜头</span></div>
              <div class="storyboard-rail-list" role="list" aria-label="镜头列表">
                <div v-for="sb in storyboards" :key="sb.id" class="storyboard-rail-row" role="listitem">
                  <button
                    class="storyboard-rail-item"
                    type="button"
                    :class="{ active: selectedStoryboard?.id === sb.id }"
                    :aria-current="selectedStoryboard?.id === sb.id ? 'true' : undefined"
                    :aria-label="`镜头 ${sb.storyboard_number} ${sb.title || ''}`"
                    @click="selectedStoryboardId = sb.id"
                  >
                    <span class="storyboard-rail-index"><i class="status-dot" :class="shotStatusDot(sb)"></i>#{{ sb.storyboard_number }}</span>
                    <strong>{{ sb.title || '未命名镜头' }}</strong>
                    <small>{{ sb.duration }}s · {{ sb.shot_type || '未设置景别' }}</small>
                  </button>
                  <label v-if="canEdit" class="storyboard-batch-check" :title="`选择镜头 ${sb.storyboard_number}`">
                    <input type="checkbox" :checked="selectedShotIds.includes(sb.id)" :aria-label="`选择镜头 ${sb.storyboard_number}`" @change="toggleShot(sb.id)" />
                  </label>
                </div>
              </div>
            </aside>

            <section v-if="selectedStoryboard" class="storyboard-inspector" role="region" aria-label="当前镜头">
              <div class="storyboard-inspector-head">
                <div><span class="muted">镜头 {{ selectedStoryboard.storyboard_number }}</span><h3>#{{ selectedStoryboard.storyboard_number }} {{ selectedStoryboard.title || '镜头' }}</h3></div>
                <span class="job-status" :class="selectedStoryboard.status">{{ selectedStoryboard.duration }}s · {{ storyboardStatusLabel(selectedStoryboard.status) }}</span>
              </div>
              <div class="storyboard-copy-grid">
                <div><span>镜头参数</span><p>{{ selectedStoryboard.shot_type || '未设置' }} / {{ selectedStoryboard.angle || '未设置' }} / {{ selectedStoryboard.movement || '未设置' }}</p></div>
                <div><span>镜头描述</span><p>{{ selectedStoryboard.description || selectedStoryboard.action || '暂无描述' }}</p></div>
                <div class="storyboard-dialogue"><span>对白</span><p>{{ selectedStoryboard.dialogue || '（无）' }}</p></div>
              </div>
              <div class="storyboard-media-grid">
                <div class="storyboard-media-cell"><span>首帧</span><img v-if="selectedStoryboard.first_frame_image" :src="selectedStoryboard.first_frame_image" :alt="`镜头 ${selectedStoryboard.storyboard_number} 首帧`" /><div v-else class="media-placeholder">未生成</div></div>
                <div class="storyboard-media-cell"><span>尾帧</span><img v-if="selectedStoryboard.last_frame_image" :src="selectedStoryboard.last_frame_image" :alt="`镜头 ${selectedStoryboard.storyboard_number} 尾帧`" /><div v-else class="media-placeholder">未生成</div></div>
                <div class="storyboard-media-cell"><span>分镜板</span><img v-if="selectedStoryboard.composed_image" :src="selectedStoryboard.composed_image" :alt="`镜头 ${selectedStoryboard.storyboard_number} 分镜板`" /><div v-else class="media-placeholder">未生成</div></div>
                <div class="storyboard-media-cell storyboard-video-cell"><span>视频</span><video v-if="selectedStoryboard.video_url" :src="selectedStoryboard.video_url" controls /><div v-else class="media-placeholder">未生成</div></div>
              </div>
              <audio v-if="selectedStoryboard.tts_audio_url" class="storyboard-audio" :src="selectedStoryboard.tts_audio_url" controls />
              <div v-if="canEdit" class="storyboard-primary-actions">
                <button class="btn" :disabled="!!busy" @click="genFrame(selectedStoryboard,'first_frame')">生成首帧</button>
                <button class="btn" :disabled="!!busy" @click="genFrame(selectedStoryboard,'last_frame')">生成尾帧</button>
                <button class="btn" :disabled="!!busy" @click="genFrame(selectedStoryboard,'composed')">生成分镜板</button>
                <button class="btn btn-primary" :disabled="!!busy" @click="genVideo(selectedStoryboard)">生成视频</button>
                <button class="btn" :disabled="!!busy" @click="genTTS(selectedStoryboard)">生成配音</button>
                <button class="btn" :disabled="!!busy" @click="composeShot(selectedStoryboard)">合成镜头</button>
              </div>
              <div v-if="canEdit" class="storyboard-secondary-actions">
                <button class="btn btn-ghost" :disabled="!!busy" @click="openPromptEditor(selectedStoryboard,'image_prompt','图片提示词')">改图词</button>
                <button class="btn btn-ghost" :disabled="!!busy" @click="openPromptEditor(selectedStoryboard,'video_prompt','视频提示词')">改视频词</button>
                <button class="btn btn-ghost" :disabled="!!busy" @click="openPromptEditor(selectedStoryboard,'dialogue','对白')">改对白</button>
                <button class="btn btn-danger" :disabled="!!busy" @click="removeStoryboard(selectedStoryboard)">删除镜头</button>
              </div>
            </section>
          </div>
          <div v-if="!storyboards.length" class="empty" style="margin-top:12px">尚未拆解分镜</div>
        </div>

        <!-- EXPORT -->
        <div v-else class="panel">
          <div v-if="canEdit" class="toolbar">
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
                  <img v-if="asset.mime_type?.startsWith('image/')" class="thumb" :src="asset.url" :alt="asset.name" style="margin-top:8px" />
                  <a :href="asset.url" target="_blank" class="muted" style="font-size:12px;display:block;margin-top:6px">打开素材</a>
                  <div v-if="canEdit && (asset.type === 'image' || asset.mime_type?.startsWith('image/'))" class="row" style="margin-top:8px;align-items:center">
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
                  <button v-if="canEdit && !['succeeded','failed','canceled'].includes(job.status)" class="btn btn-danger" style="margin-top:8px" @click="cancelJob(job)">取消任务</button>
                  <button v-if="canEdit && ['failed','canceled'].includes(job.status)" class="btn" style="margin-top:8px" @click="retryJob(job)">重试任务</button>
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

    <div v-if="characterForm" class="modal-mask" @click.self="characterForm=null">
      <form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" :aria-labelledby="characterForm.id ? 'edit-character-title' : 'add-character-title'" @keydown.esc="characterForm=null" @submit.prevent="saveCharacter">
        <h3 :id="characterForm.id ? 'edit-character-title' : 'add-character-title'">{{ characterForm.id ? '编辑角色' : '添加角色' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field-grid">
          <div class="field"><label for="workbench-character-name">角色名 <span class="required-mark">*</span></label><input id="workbench-character-name" ref="characterNameInput" v-model="characterForm.name" maxlength="120" required /></div>
          <div class="field"><label for="workbench-character-role">定位</label><input id="workbench-character-role" v-model="characterForm.role" maxlength="120" /></div>
          <div class="field"><label for="workbench-character-appearance">外貌</label><textarea id="workbench-character-appearance" v-model="characterForm.appearance" rows="3" maxlength="4000" /></div>
          <div class="field"><label for="workbench-character-personality">性格</label><textarea id="workbench-character-personality" v-model="characterForm.personality" rows="3" maxlength="4000" /></div>
          <div class="field settings-span"><label for="workbench-character-description">说明</label><textarea id="workbench-character-description" v-model="characterForm.description" rows="3" maxlength="4000" /></div>
        </div>
        <p v-if="characterError" class="form-error" role="alert">{{ characterError }}</p>
        <div class="modal-actions"><button class="btn" type="button" @click="characterForm=null">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'character-save' ? '保存中…' : '保存角色' }}</button></div>
      </form>
    </div>

    <div v-if="sceneForm" class="modal-mask" @click.self="sceneForm=null">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="sceneForm.id ? 'edit-scene-title' : 'add-scene-title'" @keydown.esc="sceneForm=null" @submit.prevent="saveScene">
        <h3 :id="sceneForm.id ? 'edit-scene-title' : 'add-scene-title'">{{ sceneForm.id ? '编辑场景' : '添加场景' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field-grid">
          <div class="field"><label for="workbench-scene-location">地点 <span class="required-mark">*</span></label><input id="workbench-scene-location" ref="sceneLocationInput" v-model="sceneForm.location" maxlength="200" required /></div>
          <div class="field"><label for="workbench-scene-time">时间</label><input id="workbench-scene-time" v-model="sceneForm.time" maxlength="120" /></div>
          <div class="field settings-span"><label for="workbench-scene-prompt">画面提示词</label><textarea id="workbench-scene-prompt" v-model="sceneForm.prompt" rows="5" maxlength="10000" /></div>
        </div>
        <p v-if="sceneError" class="form-error" role="alert">{{ sceneError }}</p>
        <div class="modal-actions"><button class="btn" type="button" @click="sceneForm=null">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'scene-save' ? '保存中…' : '保存场景' }}</button></div>
      </form>
    </div>

    <div v-if="storyboardForm" class="modal-mask" @click.self="storyboardForm=null">
      <form class="modal settings-modal settings-modal-wide storyboard-modal" role="dialog" aria-modal="true" aria-labelledby="add-storyboard-title" @keydown.esc="storyboardForm=null" @submit.prevent="saveStoryboard">
        <h3 id="add-storyboard-title">添加分镜</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field-grid">
          <div class="field"><label for="storyboard-title">分镜标题 <span class="required-mark">*</span></label><input id="storyboard-title" ref="storyboardTitleInput" v-model="storyboardForm.title" maxlength="200" required /></div>
          <div class="field"><label for="storyboard-duration">时长（秒）</label><input id="storyboard-duration" v-model.number="storyboardForm.duration" type="number" min="1" max="3600" /></div>
          <div class="field"><label for="storyboard-shot-type">景别</label><input id="storyboard-shot-type" v-model="storyboardForm.shot_type" maxlength="200" /></div>
          <div class="field"><label for="storyboard-dialogue">对白</label><textarea id="storyboard-dialogue" v-model="storyboardForm.dialogue" rows="2" maxlength="10000" /></div>
          <div class="field settings-span"><label for="storyboard-description">镜头描述</label><textarea id="storyboard-description" v-model="storyboardForm.description" rows="3" maxlength="10000" /></div>
          <div class="field"><label for="storyboard-image-prompt">图片提示词</label><textarea id="storyboard-image-prompt" v-model="storyboardForm.image_prompt" rows="3" maxlength="10000" /></div>
          <div class="field"><label for="storyboard-video-prompt">视频提示词</label><textarea id="storyboard-video-prompt" v-model="storyboardForm.video_prompt" rows="3" maxlength="10000" /></div>
          <div class="field settings-span"><label for="storyboard-reference-images">多参考图 URL（每行一个，最多 8 张）</label><textarea id="storyboard-reference-images" v-model="storyboardForm.reference_images" rows="3" maxlength="10000" /></div>
        </div>
        <p v-if="storyboardError" class="form-error" role="alert">{{ storyboardError }}</p>
        <div class="modal-actions"><button class="btn" type="button" @click="storyboardForm=null">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'storyboard-save' ? '保存中…' : '保存分镜' }}</button></div>
      </form>
    </div>

    <div v-if="promptEditor" class="modal-mask" @click.self="promptEditor=null">
      <div class="modal settings-modal settings-modal-wide prompt-editor-modal" role="dialog" aria-modal="true" :aria-labelledby="`prompt-editor-${promptEditor.field}`" @keydown.esc="promptEditor=null">
        <h3 :id="`prompt-editor-${promptEditor.field}`">编辑{{ promptEditor.label }}</h3>
        <div v-if="promptEditor.category && promptEditorTemplates.length" class="prompt-editor-template-row">
          <div class="field">
            <label for="workbench-prompt-template">提示词模板</label>
            <select id="workbench-prompt-template" v-model.number="promptEditor.template_id">
              <option v-for="template in promptEditorTemplates" :key="template.id" :value="template.id">{{ template.name }} · v{{ template.version }}</option>
            </select>
          </div>
          <button class="btn" type="button" :disabled="!!busy || !promptEditorTemplates.length" @click="applySelectedPromptTemplate">套用模板</button>
        </div>
        <div class="field">
          <label for="workbench-prompt-value">{{ promptEditor.label }}</label>
          <textarea id="workbench-prompt-value" v-model="promptEditor.value" rows="10" maxlength="20000" />
        </div>
        <p v-if="promptEditor.error" class="form-error" role="alert">{{ promptEditor.error }}</p>
        <div class="modal-actions">
          <button class="btn" type="button" @click="promptEditor=null">取消</button>
          <button class="btn btn-primary" type="button" :disabled="!!busy" @click="savePromptEditor">{{ promptEditor.target === 'grid' ? '应用' : '保存' }}</button>
        </div>
      </div>
    </div>

    <div v-if="showProductionModal" class="modal-mask" @click.self="showProductionModal=false">
      <div class="modal production-modal" role="dialog" aria-modal="true" aria-labelledby="production-modal-title" @keydown.esc="showProductionModal=false">
        <h3 id="production-modal-title">自动制作本集</h3>
        <div class="production-modal-flow" aria-label="自动制作阶段">
          <span v-for="label in ['剧本', '角色场景', '分镜', '首帧', '视频', '配音', '合成', '成片']" :key="label">{{ label }}</span>
        </div>
        <div class="production-service-list" aria-label="本次使用服务">
          <span v-for="service in productionServices" :key="service.type" :class="{ missing: !service.config }"><small>{{ service.label }}</small><strong>{{ service.config?.name || '未绑定' }}</strong><em>{{ service.config?.provider || '请先配置' }}</em></span>
        </div>
        <p v-if="productionUsesExternalService" class="production-cost-note">包含外部 AI 服务，可能产生厂商费用。</p>
        <p class="muted">将使用本集绑定的图片、视频和音频服务。制作期间仍可返回手动调整，失败后可从当前阶段重试。</p>
        <p v-if="productionError" class="auth-error" role="alert">{{ productionError }}</p>
        <div class="modal-actions">
          <button class="btn" type="button" @click="showProductionModal=false">取消</button>
          <button class="btn btn-primary" type="button" :disabled="!!busy || !productionReady" @click="startProduction">{{ busy === 'production-start' ? '正在启动…' : '开始制作' }}</button>
        </div>
      </div>
    </div>

    <div v-if="episodeConfigForm" class="modal-mask" @click.self="episodeConfigForm=null">
      <div class="modal" role="dialog" aria-modal="true" aria-labelledby="episode-config-title">
        <h3 id="episode-config-title">剧集生成配置</h3>
        <div class="field">
          <label for="episode-image-config">图片配置</label>
          <select id="episode-image-config" v-model.number="episodeConfigForm.image_config_id">
            <option :value="0">请选择</option>
            <option v-for="item in configs.filter(row => row.service_type === 'image')" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </div>
        <div class="field">
          <label for="episode-video-config">视频配置</label>
          <select id="episode-video-config" v-model.number="episodeConfigForm.video_config_id">
            <option :value="0">请选择</option>
            <option v-for="item in configs.filter(row => row.service_type === 'video')" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </div>
        <div class="field">
          <label for="episode-audio-config">音频配置</label>
          <select id="episode-audio-config" v-model.number="episodeConfigForm.audio_config_id">
            <option :value="0">请选择</option>
            <option v-for="item in configs.filter(row => row.service_type === 'audio')" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </div>
        <div class="modal-actions">
          <button class="btn" @click="episodeConfigForm=null">取消</button>
          <button class="btn btn-primary" :disabled="!!busy" @click="saveEpisodeConfig">保存配置</button>
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
