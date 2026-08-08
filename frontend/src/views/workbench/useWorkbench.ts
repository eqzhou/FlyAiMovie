import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  assetAPI, dramaAPI, episodeAPI, gridAPI, jobsAPI, productionAPI, settingsAPI,
} from '../../api'
import { authStore } from '../../auth'
import { usePolling } from '../../composables/usePolling'
import { errorMessage } from '../../utils/errorMessage'
import {
  gridFrameLabel, shotStatusDot, storyboardStatusLabel,
} from './labels'
import {
  defaultGridAssignments, listOf, parseJSONList,
  type GridCellAssignment, type GridCellTarget,
} from './grid'
import { provideWorkbenchContext, type WorkbenchContext } from './context'
import { createWorkbenchGridActions } from './useWorkbenchGridActions'
import { createWorkbenchMediaActions } from './useWorkbenchMediaActions'
import { createWorkbenchStoryboardActions } from './useWorkbenchStoryboardActions'
import { useWorkbenchStages } from './useWorkbenchStages'

export function useWorkbench() {

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
const { active: tab, select: selectStage, handleKeydown: handleStageKeydown } = useWorkbenchStages()
const rawContent = ref('')
const busy = ref('')
const log = ref('')
const toast = ref('')
const loading = ref(true)
const workbenchReady = ref(false)
const loadError = ref('')
const assetRefreshWarning = ref('')
const settingsLoadWarning = ref('')
const refreshWarning = computed(() => [loadError.value, assetRefreshWarning.value, settingsLoadWarning.value].filter(Boolean).join('；'))
let toastTimer: number | null = null
let loadRequest = 0
let refreshRequest = 0
let disposed = false

// grid state
const gridRows = ref(2)
const gridCols = ref(2)
const gridMode = ref('first_frame')
const gridPrompt = ref('')
const gridCells = ref<string[]>([])
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
const pendingJobActionIDs = ref<number[]>([])
const productions = ref<any[]>([])
const assetTargetShot = ref<Record<number, number>>({})
const assetTargetFrame = ref<Record<number, string>>({})

// voice assign panel
const assignCharId = ref<number | null>(null)
const characterForm = ref<any | null>(null)
const sceneForm = ref<any | null>(null)
const sceneTransfer = ref<any | null>(null)
const storyboardForm = ref<any | null>(null)
const showCharacterLibraryImport = ref(false)
const characterLibraryTemplates = ref<any[]>([])
const characterLibraryQuery = ref('')
const characterLibraryError = ref('')
const characterLibraryLoading = ref(false)
const selectedStoryboardId = ref<number | null>(null)
const promptEditor = ref<any | null>(null)
const episodeConfigForm = ref<any | null>(null)
const showProductionModal = ref(false)
const productionError = ref('')
const characterError = ref('')
const sceneError = ref('')
const storyboardError = ref('')
const characterFormModal = ref<{ focus: () => void } | null>(null)
const sceneFormModal = ref<{ focus: () => void } | null>(null)
const storyboardFormModal = ref<{ focus: () => void } | null>(null)

const dramaId = computed(() => Number(route.params.id))
const episodeNumber = computed(() => Number(route.params.episodeNumber))
const promptEditorTemplates = computed(() => promptEditor.value
  ? promptTemplates.value.filter((template) => template.is_active !== false && template.category === promptEditor.value.category)
  : [])
const hasActiveJobs = computed(() => jobs.value.some((job) => !['succeeded', 'failed', 'canceled'].includes(job.status)))
const currentProduction = computed(() => productions.value[0] || null)
const selectedStoryboard = computed(() => storyboards.value.find((item) => item.id === selectedStoryboardId.value) || storyboards.value[0] || null)
const selectedStoryboardScene = computed(() => {
  const sceneID = Number(selectedStoryboard.value?.scene_id) || 0
  if (!sceneID) return null
  return scenes.value.find((item) => Number(item.id) === sceneID) || null
})
const selectedStoryboardFacts = computed(() => {
  const shot = selectedStoryboard.value
  if (!shot) return [] as { label: string; value: string }[]
  const scene = selectedStoryboardScene.value
  return [
    { label: '所属场景', value: scene ? `${scene.location}${scene.time ? ` · ${scene.time}` : ''}` : '' },
    { label: '地点', value: shot.location || '' },
    { label: '时间', value: shot.time || '' },
    { label: '氛围', value: shot.atmosphere || '' },
    { label: '结果', value: shot.result || '' },
    { label: '背景音乐', value: shot.bgm_prompt || '' },
    { label: '音效', value: shot.sound_effect || '' },
  ].filter((item) => Boolean(item.value.trim()))
})
const hasActiveProduction = computed(() => currentProduction.value?.status === 'queued')
// 后端按项目校验场景，而这里只加载了本集场景。若镜头绑定的是同项目其他集的
// 场景，缺少对应选项会让保存时静默清空绑定，因此补一个只读占位项。
const storyboardSceneOptions = computed(() => {
  const options = scenes.value.map((scene) => ({
    id: Number(scene.id),
    label: `${scene.location}${scene.time ? ` · ${scene.time}` : ''}`,
  }))
  const boundID = Number(storyboardForm.value?.scene_id) || 0
  if (boundID > 0 && !options.some((option) => option.id === boundID)) {
    options.unshift({ id: boundID, label: `场景 #${boundID}（其他剧集）` })
  }
  return options
})
// 与场景同理：后端按项目校验角色，而这里只加载了本集角色。若镜头绑定的是同项目
// 其他集的角色，补一个占位项，避免保存时把看不见的绑定静默丢掉。
const storyboardCharacterOptions = computed(() => {
  const options = characters.value.map((character) => ({
    id: Number(character.id),
    label: character.name || `角色 #${character.id}`,
  }))
  const bound = Array.isArray(storyboardForm.value?.character_ids) ? storyboardForm.value.character_ids : []
  for (const value of bound) {
    const id = Number(value)
    if (id > 0 && !options.some((option) => option.id === id)) {
      options.push({ id, label: `角色 #${id}（其他剧集）` })
    }
  }
  return options
})
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
const filteredCharacterLibraryTemplates = computed(() => {
  const keyword = characterLibraryQuery.value.trim().toLocaleLowerCase('zh-CN')
  if (!keyword) return characterLibraryTemplates.value
  return characterLibraryTemplates.value.filter((template) => [template.name, template.role, template.appearance, template.personality, template.voice_style]
    .some((value) => String(value || '').toLocaleLowerCase('zh-CN').includes(keyword)))
})
const stages = computed(() => [
  { id: 'script', label: '剧本', detail: status.value?.has_script ? '已就绪' : '待完成', complete: Boolean(status.value?.has_script) },
  { id: 'cast', label: '角色与场景', detail: `${status.value?.characters || 0} 角色 · ${status.value?.scenes || 0} 场景`, complete: Boolean(status.value?.characters && status.value?.scenes) },
  { id: 'grid', label: '宫格帧', detail: `${status.value?.storyboards || 0} 个镜头`, complete: Boolean(status.value?.storyboards) },
  {
    id: 'boards',
    label: '分镜与视频',
    detail: `${status.value?.with_video || 0} 视频 · ${status.value?.with_tts || 0} 配音`,
    complete: Boolean(status.value?.with_video),
  },
  { id: 'export', label: '合成导出', detail: episode.value?.video_url ? '成片完成' : `${status.value?.composed || 0} 个已合成`, complete: Boolean(episode.value?.video_url) },
])
const currentStageIndex = computed(() => Math.max(0, stages.value.findIndex((stage) => stage.id === tab.value)))
const currentStage = computed(() => stages.value[currentStageIndex.value] || stages.value[0])
const previousStage = computed(() => stages.value[currentStageIndex.value - 1] || null)
const nextStage = computed(() => stages.value[currentStageIndex.value + 1] || null)
const completedStageCount = computed(() => stages.value.filter((stage) => stage.complete).length)
const stageProgressLabel = computed(() => `${completedStageCount.value}/${stages.value.length} 阶段完成 · 总进度 ${progressPct.value}%`)
const busyLabel = computed(() => {
  const value = busy.value
  if (!value) return ''
  const exact: Record<string, string> = {
    'save-content': '正在保存原文',
    'episode-config': '正在保存生成配置',
    'production-start': '正在启动自动制作',
    'production-cancel': '正在取消自动制作',
    'production-retry': '正在重试自动制作',
    'character-save': '正在保存角色',
    'scene-save': '正在保存场景',
    'storyboard-save': '正在保存分镜',
    'grid-prompt': '正在生成宫格提示词',
    'grid-gen': '正在生成宫格图',
    'grid-split': '正在切分宫格图',
    'batch-frames': '正在批量生成画面',
    'batch-videos': '正在批量生成视频',
    'batch-tts': '正在批量生成配音',
    'compose-all': '正在批量合成镜头',
    merge: '正在拼接导出成片',
  }
  if (exact[value]) return exact[value]
  if (value.startsWith('char-img-')) return '正在生成角色形象'
  if (value.startsWith('char-library-')) return '正在保存到角色库'
  if (value.startsWith('char-import-')) return '正在导入角色模板'
  if (value.startsWith('scene-img-')) return '正在生成场景画面'
  if (value.startsWith('upload-')) return '正在上传素材'
  if (value.startsWith('voice-')) return '正在生成角色试听'
  if (value.startsWith('frame-')) return '正在生成镜头画面'
  if (value.startsWith('video-')) return '正在生成镜头视频'
  if (value.startsWith('tts-')) return '正在生成镜头配音'
  if (value.startsWith('compose-')) return '正在合成镜头'
  if (value.startsWith('storyboard-delete-')) return '正在删除分镜'
  if (value.startsWith('storyboard-copy-')) return '正在复制分镜'
  if (value.startsWith('storyboard-move-')) return '正在调整镜头顺序'
  if (value.startsWith('save-shot-')) return '正在保存镜头内容'
  return '正在处理，请稍候'
})

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
  if (toastTimer) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => {
    if (toast.value === msg) toast.value = ''
    toastTimer = null
  }, 2600)
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

function gridAssignmentLabel(index: number) {
  const assignment = gridAssignments.value.find((item) => item.cell_index === index)
  const storyboard = storyboards.value.find((item) => item.id === assignment?.storyboard_id)
  if (!assignment || !storyboard) return '尚未分配'
  return `镜头 #${storyboard.storyboard_number} · ${gridFrameLabel(assignment.frame_type)}`
}

async function load(request = loadRequest): Promise<boolean> {
  const loadedDrama = await dramaAPI.get(dramaId.value)
  if (disposed || request !== loadRequest) return false
  const loadedEpisode = (loadedDrama.episodes || []).find((e: any) => e.episode_number === episodeNumber.value)
  drama.value = loadedDrama
  if (!loadedEpisode) {
    episode.value = null
    return true
  }
  episode.value = loadedEpisode
  rawContent.value = loadedEpisode.content || ''
  await refreshAssets()
  if (disposed || request !== loadRequest) return false
  const settingsResults = await Promise.allSettled([
    settingsAPI.voices(),
    settingsAPI.aiConfigs(),
    settingsAPI.promptTemplates(),
  ])
  if (disposed || request !== loadRequest) return false
  if (settingsResults[0].status === 'fulfilled') voices.value = listOf(settingsResults[0].value)
  if (settingsResults[1].status === 'fulfilled') configs.value = listOf(settingsResults[1].value)
  if (settingsResults[2].status === 'fulfilled') promptTemplates.value = listOf(settingsResults[2].value)
  const settingsLabels = ['音色', 'AI 配置', '提示词']
  settingsLoadWarning.value = settingsResults.flatMap((result, index) => {
    if (result.status === 'fulfilled') return []
    const detail = errorMessage(result.reason, '服务暂时不可用')
    return [`${settingsLabels[index]}加载失败：${detail}`]
  }).join('；')
  return true
}

async function loadWorkbench() {
  const request = ++loadRequest
  let loadedSuccessfully = false
  stopPoll()
  loading.value = true
  loadError.value = ''
  try {
    if (!await load(request)) return
    if (!episode.value) loadError.value = '未找到对应剧集'
    else {
      workbenchReady.value = true
      loadedSuccessfully = true
    }
  } catch (error) {
    if (disposed || request !== loadRequest) return
    loadError.value = errorMessage(error, '加载失败')
  } finally {
    if (!disposed && request === loadRequest) loading.value = false
  }
  if (!disposed && request === loadRequest && loadedSuccessfully) startPoll()
}

async function refreshAssets() {
  const request = ++refreshRequest
  const currentEpisode = episode.value
  if (!currentEpisode) return
  const episodeId = currentEpisode.id
  const requestDramaId = dramaId.value
  const requestEpisodeNumber = episodeNumber.value
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
  if (disposed || request !== refreshRequest || dramaId.value !== requestDramaId || episodeNumber.value !== requestEpisodeNumber || episode.value?.id !== episodeId) return
  const labels = ['角色', '场景', '分镜', '制作进度', '宫格历史', '素材', '任务', '自动制作', '剧集']
  const failures: string[] = []
  results.forEach((result, index) => {
    if (result.status === 'rejected') {
      const detail = errorMessage(result.reason, '服务暂时不可用')
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

const assetPoll = usePolling(() => refreshAssets().catch(() => {}), {
  intervalMs: 5000,
  shouldRun: () => hasActiveJobs.value || hasActiveProduction.value,
})

function startPoll() {
  if (disposed) return
  assetPoll.start()
}
function stopPoll() {
  assetPoll.stop()
}

  const mediaActions = createWorkbenchMediaActions({
    busy, episode, rawContent, show, load, configs, episodeConfigForm,
    productionError, productionReady, missingProductionServices, showProductionModal,
    dramaId, productions, currentProduction, startPoll, log, refreshAssets, characters,
    characterForm, characterError, characterFormModal, showCharacterLibraryImport,
    characterLibraryError, characterLibraryQuery, characterLibraryLoading,
    characterLibraryTemplates, sceneForm, sceneError, sceneFormModal, sceneTransfer,
    drama, assignCharId, selectedShotIds, pendingJobActionIDs, assetTargetShot, assetTargetFrame,
  })
  const gridActions = createWorkbenchGridActions({
    busy, gridPrompt, gridRows, gridCols, gridMode, episode, dramaId,
    gridSelectionError, selectedShotIds, resetGridOutput,
    getGridContextVersion: () => gridContextVersion, gridImage, gridHistoryId,
    gridCells, gridCellsVerified, gridCellErrors, setGridAssignments, gridCellTarget,
    assigningGridCell, storyboards, show, refreshAssets,
  })
  const storyboardActions = createWorkbenchStoryboardActions({
    storyboardError, storyboardForm, storyboardFormModal, episode, drama, scenes,
    characters, selectedStoryboardId, selectedShotIds, busy, show, refreshAssets,
    promptTemplates, promptEditor, gridPrompt, gridRows, gridCols, gridMode,
    promptEditorTemplates,
  })
  const actions = { ...mediaActions, ...gridActions, ...storyboardActions }


  const workbenchContext = reactive({
    drama, episode, characters, scenes, storyboards, status, voices, configs, promptTemplates,
    gridHist, assets, jobs, productions,
    rawContent, busy, canEdit,
    gridRows, gridCols, gridMode, gridPrompt, gridImage, gridCells, gridCellsVerified,
    gridHistoryId, gridAssignments, gridCellTargets, gridCellErrors, assigningGridCell,
    gridSelectionError, selectedShotIds, selectedStoryboardId, selectedStoryboard, selectedStoryboardFacts,
    assignCharId, sceneTransfer, assetTargetShot, assetTargetFrame, pendingJobActionIDs,
    ...actions,
    selectGridMode, updateGridDimension, gridAssignmentLabel, gridCellTarget, updateGridCellTarget,
    loadGridHistory, storyboardStatusLabel, shotStatusDot,
  }) as unknown as WorkbenchContext
  provideWorkbenchContext(workbenchContext)

watch([dramaId, episodeNumber], ([nextDramaId, nextEpisodeNumber], [previousDramaId, previousEpisodeNumber]) => {
  if (nextDramaId === previousDramaId && nextEpisodeNumber === previousEpisodeNumber) return
  stopPoll()
  workbenchReady.value = false
  episode.value = null
  shotSelectionInitialized.value = false
  selectedShotIds.value = []
  selectedStoryboardId.value = null
  resetGridOutput()
  loadWorkbench()
})

onMounted(() => {
  disposed = false
  loadWorkbench()
})
onUnmounted(() => {
  disposed = true
  loadRequest += 1
  refreshRequest += 1
  stopPoll()
  gridContextVersion += 1
  if (toastTimer) window.clearTimeout(toastTimer)
})

  return {
    route, router, drama, episode, characters, scenes, storyboards, status, voices, configs, promptTemplates,
    gridHist, rawContent, busy, log, toast, loading, workbenchReady, loadError, assetRefreshWarning,
    settingsLoadWarning, refreshWarning, gridRows, gridCols, gridMode, gridPrompt, gridCells,
    gridAssignments, gridCellsVerified, gridCellTargets, gridCellErrors, assigningGridCell, gridImage,
    gridHistoryId, selectedShotIds, assets, jobs, pendingJobActionIDs, productions, assetTargetShot,
    assetTargetFrame, assignCharId, characterForm, sceneForm, sceneTransfer, showCharacterLibraryImport,
    characterLibraryTemplates, characterLibraryQuery, characterLibraryError, characterLibraryLoading,
    selectedStoryboardId, promptEditor, episodeConfigForm, showProductionModal, productionError,
    characterError, sceneError, storyboardError, characterFormModal, sceneFormModal, storyboardFormModal,
    storyboardForm,
    dramaId, episodeNumber, promptEditorTemplates, hasActiveJobs, currentProduction, selectedStoryboard,
    selectedStoryboardScene, selectedStoryboardFacts, hasActiveProduction, storyboardSceneOptions,
    storyboardCharacterOptions, canEdit, gridSelectionError, productionServices, missingProductionServices,
    productionReady, productionUsesExternalService, filteredCharacterLibraryTemplates, stages,
    currentStageIndex, currentStage, previousStage, nextStage, completedStageCount, stageProgressLabel,
    busyLabel, progressPct, tab, selectStage, handleStageKeydown,
    show, resetGridOutput, selectGridMode, updateGridDimension, setGridAssignments, loadGridHistory,
    gridCellTarget, updateGridCellTarget, gridAssignmentLabel, load, loadWorkbench, refreshAssets,
    startPoll, stopPoll, ...actions,
  }
}
