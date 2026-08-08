import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { characterAPI, dramaAPI, episodeAPI, propAPI, sceneAPI, settingsAPI } from '../../api'
import { authStore } from '../../auth'
import { confirmAction } from '../../composables/useConfirm'
import { errorMessage } from '../../utils/errorMessage'

export function useDrama() {
const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const configs = ref<any[]>([])
const voices = ref<any[]>([])
const propsList = ref<any[]>([])
const activeView = ref<'episodes' | 'assets'>('episodes')
const assetView = ref<'characters' | 'scenes' | 'props'>('characters')
const loading = ref(true)
const busy = ref('')
const error = ref('')
const message = ref('')
const showEpisodeDialog = ref(false)
const showPropDialog = ref(false)
const showCharacterDialog = ref(false)
const showSceneDialog = ref(false)
const showSceneTransfer = ref(false)
const episodeError = ref('')
const propError = ref('')
const characterError = ref('')
const sceneError = ref('')
const transferError = ref('')
let messageTimer: number | null = null
let loadRequest = 0
const episodeTitleInput = ref<HTMLInputElement | null>(null)
const propNameInput = ref<HTMLInputElement | null>(null)
const characterNameInput = ref<HTMLInputElement | null>(null)
const sceneLocationInput = ref<HTMLInputElement | null>(null)
const episodeForm = ref<{
  id?: number
  title: string
  image_config_id: number
  video_config_id: number
  audio_config_id: number
}>({ title: '', image_config_id: 0, video_config_id: 0, audio_config_id: 0 })
const propForm = ref<{ id?: number; name: string; type: string; description: string; prompt: string; reference_images: string }>({ name: '', type: '', description: '', prompt: '', reference_images: '' })
const characterForm = ref<{
  id?: number
  name: string
  role: string
  appearance: string
  description: string
  personality: string
  voice_style: string
  voice_provider: string
  reference_images: string
}>({ name: '', role: '', appearance: '', description: '', personality: '', voice_style: '', voice_provider: '', reference_images: '' })
const sceneForm = ref<{ id?: number; location: string; time: string; prompt: string }>({ location: '', time: '', prompt: '' })
const sceneTransfer = ref<{ scene: any; mode: 'copy' | 'move'; target_episode_id: number; move_storyboards: boolean } | null>(null)
const styleLabels: Record<string, string> = { realistic: '写实', anime: '动漫', cinematic: '电影感' }

// 参考图在后端以 JSON 数组存储，表单里按每行一个 URL 编辑。
function referenceImagesToText(value: any): string {
  const raw = typeof value === 'string' ? value.trim() : ''
  if (!raw) return ''
  if (raw.startsWith('[')) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        return parsed.map((item) => String(item ?? '').trim()).filter(Boolean).join('\n')
      }
    } catch {
      // 非法 JSON 时退回按行解析
    }
  }
  return raw.split(/\r?\n/).map((item) => item.trim()).filter(Boolean).join('\n')
}
function referenceImagesToPayload(text: string): string {
  const items = text.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
  return items.length ? JSON.stringify(items) : ''
}

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
const activeVoices = computed(() => voices.value.filter((voice) => voice.is_active !== false))
const projectStyle = computed(() => styleLabels[drama.value?.style || ''] || drama.value?.style || '未设置风格')
const primaryEpisode = computed(() => sortedEpisodes.value[0] || null)
const otherEpisodes = computed(() => sortedEpisodes.value)

function notify(text: string) {
  message.value = text
  if (messageTimer) window.clearTimeout(messageTimer)
  messageTimer = window.setTimeout(() => {
    if (message.value === text) message.value = ''
    messageTimer = null
  }, 2400)
}

function requirePrimaryEpisode(action: string) {
  if (!primaryEpisode.value?.id) {
    notify(`请先创建剧集后再${action}`)
    return null
  }
  return primaryEpisode.value
}

async function load() {
  const request = ++loadRequest
  const dramaID = id.value
  loading.value = true
  error.value = ''
  try {
    const [project, aiConfigs, voiceCatalog] = await Promise.all([
      dramaAPI.get(id.value),
      settingsAPI.aiConfigs(),
      settingsAPI.voices().catch(() => []),
    ])
    if (request !== loadRequest || id.value !== dramaID) return
    drama.value = project
    configs.value = aiConfigs
    voices.value = Array.isArray(voiceCatalog) ? voiceCatalog : []
    let loadedProps: any[]
    try { loadedProps = await propAPI.list(dramaID) } catch { loadedProps = project.props || [] }
    if (request !== loadRequest || id.value !== dramaID) return
    propsList.value = loadedProps
    if (!episodeForm.value.id) {
      episodeForm.value.image_config_id = imageConfigs.value[0]?.id || 0
      episodeForm.value.video_config_id = videoConfigs.value[0]?.id || 0
      episodeForm.value.audio_config_id = audioConfigs.value[0]?.id || 0
    }
  } catch (reason) {
    if (request !== loadRequest) return
    error.value = errorMessage(reason, '项目加载失败')
  } finally {
    if (request === loadRequest) loading.value = false
  }
}

async function openEpisodeDialog(episode?: any) {
  episodeError.value = ''
  if (episode) {
    episodeForm.value = {
      id: episode.id,
      title: episode.title || '',
      image_config_id: episode.image_config_id || imageConfigs.value[0]?.id || 0,
      video_config_id: episode.video_config_id || videoConfigs.value[0]?.id || 0,
      audio_config_id: episode.audio_config_id || audioConfigs.value[0]?.id || 0,
    }
  } else {
    episodeForm.value = {
      title: '',
      image_config_id: imageConfigs.value[0]?.id || 0,
      video_config_id: videoConfigs.value[0]?.id || 0,
      audio_config_id: audioConfigs.value[0]?.id || 0,
    }
  }
  showEpisodeDialog.value = true
  await nextTick()
  episodeTitleInput.value?.focus()
}

function closeEpisodeDialog() {
  showEpisodeDialog.value = false
  episodeError.value = ''
  episodeForm.value = {
    title: '',
    image_config_id: imageConfigs.value[0]?.id || 0,
    video_config_id: videoConfigs.value[0]?.id || 0,
    audio_config_id: audioConfigs.value[0]?.id || 0,
  }
}

async function saveEpisode() {
  episodeError.value = ''
  if (!episodeForm.value.image_config_id || !episodeForm.value.video_config_id || !episodeForm.value.audio_config_id) {
    episodeError.value = '请先选择图片、视频和音频服务'
    return
  }
  busy.value = 'episode'
  try {
    if (episodeForm.value.id) {
      const title = episodeForm.value.title.trim()
      await episodeAPI.update(episodeForm.value.id, {
        ...(title ? { title } : {}),
        image_config_id: episodeForm.value.image_config_id,
        video_config_id: episodeForm.value.video_config_id,
        audio_config_id: episodeForm.value.audio_config_id,
      })
      notify('剧集已更新')
    } else {
      await episodeAPI.create({
        drama_id: id.value,
        title: episodeForm.value.title.trim() || undefined,
        image_config_id: episodeForm.value.image_config_id,
        video_config_id: episodeForm.value.video_config_id,
        audio_config_id: episodeForm.value.audio_config_id,
      })
      notify('剧集已创建')
    }
    closeEpisodeDialog()
    await load()
  } catch (reason) {
    episodeError.value = errorMessage(reason, (episodeForm.value.id ? '更新剧集失败' : '创建剧集失败'))
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
        type: prop.type || '',
        description: prop.description || '',
        prompt: prop.prompt || '',
        reference_images: referenceImagesToText(prop.reference_images),
      }
    : { name: '', type: '', description: '', prompt: '', reference_images: '' }
  showPropDialog.value = true
  await nextTick()
  propNameInput.value?.focus()
}

function closePropDialog() {
  showPropDialog.value = false
  propError.value = ''
  propForm.value = { name: '', type: '', description: '', prompt: '', reference_images: '' }
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
      type: propForm.value.type.trim(),
      description: propForm.value.description.trim(),
      prompt: propForm.value.prompt.trim(),
      reference_images: referenceImagesToPayload(propForm.value.reference_images),
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
    propError.value = errorMessage(reason, (propForm.value.id ? '更新道具失败' : '添加道具失败'))
  } finally {
    busy.value = ''
  }
}

async function generatePropImage(prop: any) {
  busy.value = `prop-${prop.id}`
  try {
    const episode = requirePrimaryEpisode('生成道具图片')
    if (!episode) return
    await propAPI.generateImage(prop.id, episode.id)
    notify('道具图片任务已提交')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '生成道具图片失败'))
  } finally {
    busy.value = ''
  }
}

async function removeProp(prop: any) {
  if (!await confirmAction({
    title: '删除道具',
    message: `确定删除道具「${prop.name}」？`,
    detail: '该道具已生成的图片将不再关联到本项目。',
    confirmText: '删除道具',
    tone: 'danger',
  })) return
  busy.value = `prop-del-${prop.id}`
  try {
    await propAPI.del(prop.id)
    notify('道具已删除')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '删除道具失败'))
  } finally {
    busy.value = ''
  }
}

async function openCharacterDialog(character?: any) {
  characterError.value = ''
  characterForm.value = character
    ? {
        id: character.id,
        name: character.name || '',
        role: character.role || '',
        appearance: character.appearance || '',
        description: character.description || '',
        personality: character.personality || '',
        voice_style: character.voice_style || '',
        voice_provider: character.voice_provider || '',
        reference_images: referenceImagesToText(character.reference_images),
      }
    : { name: '', role: '', appearance: '', description: '', personality: '', voice_style: '', voice_provider: '', reference_images: '' }
  showCharacterDialog.value = true
  await nextTick()
  characterNameInput.value?.focus()
}

function closeCharacterDialog() {
  showCharacterDialog.value = false
  characterError.value = ''
  characterForm.value = { name: '', role: '', appearance: '', description: '', personality: '', voice_style: '', voice_provider: '', reference_images: '' }
}

async function saveCharacter() {
  characterError.value = ''
  const name = characterForm.value.name.trim()
  if (!name) {
    characterError.value = '请填写角色名称'
    characterNameInput.value?.focus()
    return
  }
  busy.value = 'character'
  try {
    const payload = {
      name,
      role: characterForm.value.role.trim(),
      appearance: characterForm.value.appearance.trim(),
      description: characterForm.value.description.trim(),
      personality: characterForm.value.personality.trim(),
      voice_style: characterForm.value.voice_style.trim(),
      voice_provider: characterForm.value.voice_provider.trim(),
      reference_images: referenceImagesToPayload(characterForm.value.reference_images),
    }
    if (characterForm.value.id) {
      await characterAPI.update(characterForm.value.id, payload)
      notify('角色已更新')
    } else {
      await characterAPI.create({ drama_id: id.value, ...payload })
      notify('角色已添加')
    }
    closeCharacterDialog()
    await load()
  } catch (reason) {
    characterError.value = errorMessage(reason, (characterForm.value.id ? '更新角色失败' : '添加角色失败'))
  } finally {
    busy.value = ''
  }
}

async function generateCharacterImage(character: any) {
  const episode = requirePrimaryEpisode('生成角色形象')
  if (!episode) return
  busy.value = `char-img-${character.id}`
  try {
    await characterAPI.generateImage(character.id, episode.id)
    notify('角色形象任务已提交')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '生成角色形象失败'))
  } finally {
    busy.value = ''
  }
}

async function sampleCharacterVoice(character: any) {
  const episode = requirePrimaryEpisode('试听角色音色')
  if (!episode) return
  busy.value = `char-voice-${character.id}`
  try {
    await characterAPI.voiceSample(character.id, episode.id)
    notify('试听已生成')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '生成试听失败'))
  } finally {
    busy.value = ''
  }
}

async function saveCharacterToLibrary(character: any) {
  busy.value = `char-library-${character.id}`
  try {
    await characterAPI.saveToLibrary(character.id)
    notify(`已将「${character.name}」保存到角色库`)
  } catch (reason) {
    notify(errorMessage(reason, '保存到角色库失败'))
  } finally {
    busy.value = ''
  }
}

async function removeCharacter(character: any) {
  if (!await confirmAction({
    title: '删除角色',
    message: `确定删除角色「${character.name}」？`,
    detail: '该角色的形象设定与已生成的图片将不再可用。',
    confirmText: '删除角色',
    tone: 'danger',
  })) return
  busy.value = `char-del-${character.id}`
  try {
    await characterAPI.del(character.id)
    notify('角色已删除')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '删除角色失败'))
  } finally {
    busy.value = ''
  }
}

async function openSceneDialog(scene?: any) {
  sceneError.value = ''
  sceneForm.value = scene
    ? {
        id: scene.id,
        location: scene.location || '',
        time: scene.time || '',
        prompt: scene.prompt || scene.description || '',
      }
    : { location: '', time: '', prompt: '' }
  showSceneDialog.value = true
  await nextTick()
  sceneLocationInput.value?.focus()
}

function closeSceneDialog() {
  showSceneDialog.value = false
  sceneError.value = ''
  sceneForm.value = { location: '', time: '', prompt: '' }
}

async function saveScene() {
  sceneError.value = ''
  const location = sceneForm.value.location.trim()
  if (!location) {
    sceneError.value = '请填写场景地点'
    sceneLocationInput.value?.focus()
    return
  }
  busy.value = 'scene'
  try {
    const payload = {
      location,
      time: sceneForm.value.time.trim(),
      prompt: sceneForm.value.prompt.trim(),
    }
    if (sceneForm.value.id) {
      await sceneAPI.update(sceneForm.value.id, payload)
      notify('场景已更新')
    } else {
      await sceneAPI.create({ drama_id: id.value, ...payload })
      notify('场景已添加')
    }
    closeSceneDialog()
    await load()
  } catch (reason) {
    sceneError.value = errorMessage(reason, (sceneForm.value.id ? '更新场景失败' : '添加场景失败'))
  } finally {
    busy.value = ''
  }
}

async function generateSceneImage(scene: any) {
  const episode = requirePrimaryEpisode('生成场景图')
  if (!episode) return
  busy.value = `scene-img-${scene.id}`
  try {
    await sceneAPI.generateImage(scene.id, episode.id)
    notify('场景图片任务已提交')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '生成场景图片失败'))
  } finally {
    busy.value = ''
  }
}

async function removeScene(scene: any) {
  if (!await confirmAction({
    title: '删除场景',
    message: `确定删除场景「${scene.location}」？`,
    detail: '引用该场景的分镜需要重新指定场景。',
    confirmText: '删除场景',
    tone: 'danger',
  })) return
  busy.value = `scene-del-${scene.id}`
  try {
    await sceneAPI.del(scene.id)
    notify('场景已删除')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '删除场景失败'))
  } finally {
    busy.value = ''
  }
}

function openSceneTransfer(scene: any, mode: 'copy' | 'move') {
  transferError.value = ''
  const target = sortedEpisodes.value[0]
  sceneTransfer.value = {
    scene,
    mode,
    target_episode_id: target?.id || 0,
    move_storyboards: true,
  }
  showSceneTransfer.value = true
}

function closeSceneTransfer() {
  showSceneTransfer.value = false
  transferError.value = ''
  sceneTransfer.value = null
}

async function confirmSceneTransfer() {
  const transfer = sceneTransfer.value
  if (!transfer) return
  if (!transfer.target_episode_id) {
    transferError.value = '请选择目标剧集'
    return
  }
  busy.value = `scene-${transfer.mode}`
  transferError.value = ''
  try {
    if (transfer.mode === 'copy') {
      await sceneAPI.copy(transfer.scene.id, transfer.target_episode_id)
      notify('场景已复制')
    } else {
      await sceneAPI.move(transfer.scene.id, transfer.target_episode_id, { move_storyboards: transfer.move_storyboards })
      notify('场景已迁移')
    }
    closeSceneTransfer()
    await load()
  } catch (reason) {
    transferError.value = errorMessage(reason, (transfer.mode === 'copy' ? '复制场景失败' : '迁移场景失败'))
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
  if (!await confirmAction({
    title: '删除剧集',
    message: `确定删除剧集「${title}」？`,
    detail: '相关分镜、专属场景与制作记录将不再可用。',
    confirmText: '删除剧集',
    tone: 'danger',
  })) return
  busy.value = `episode-del-${episode.id}`
  try {
    await episodeAPI.del(episode.id)
    notify('剧集已删除')
    await load()
  } catch (reason) {
    notify(errorMessage(reason, '删除剧集失败'))
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
    notify(errorMessage(reason, '复制剧集失败'))
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
    notify(errorMessage(reason, '调整顺序失败'))
  } finally {
    busy.value = ''
  }
}

function statusText(status?: string) {
  return ({ draft: '草稿', processing: '制作中', completed: '已完成', failed: '失败' } as Record<string, string>)[status || ''] || status || '草稿'
}

function openEditEpisode(episode: any, event?: Event) {
  event?.stopPropagation()
  event?.preventDefault()
  openEpisodeDialog(episode)
}

onMounted(load)
watch(id, load)
onUnmounted(() => {
  loadRequest += 1
  if (messageTimer) window.clearTimeout(messageTimer)
})

  return { route, router, drama, configs, voices, propsList, activeView, assetView, loading, busy, error, message, showEpisodeDialog, showPropDialog, showCharacterDialog, showSceneDialog, showSceneTransfer, episodeError, propError, characterError, sceneError, transferError, messageTimer, loadRequest, episodeTitleInput, propNameInput, characterNameInput, sceneLocationInput, episodeForm, propForm, characterForm, sceneForm, sceneTransfer, styleLabels, id, canEdit, sortedEpisodes, projectSummary, imageConfigs, videoConfigs, audioConfigs, activeVoices, projectStyle, primaryEpisode, otherEpisodes, referenceImagesToText, referenceImagesToPayload, notify, requirePrimaryEpisode, load, openEpisodeDialog, closeEpisodeDialog, saveEpisode, openPropDialog, closePropDialog, saveProp, generatePropImage, removeProp, openCharacterDialog, closeCharacterDialog, saveCharacter, generateCharacterImage, sampleCharacterVoice, saveCharacterToLibrary, removeCharacter, openSceneDialog, closeSceneDialog, saveScene, generateSceneImage, removeScene, openSceneTransfer, closeSceneTransfer, confirmSceneTransfer, openEpisode, delEpisode, copyEpisode, moveEpisode, statusText, openEditEpisode }
}
