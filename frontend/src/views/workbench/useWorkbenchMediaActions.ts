import { nextTick } from 'vue'
import {
  agentAPI, characterAPI, characterLibraryAPI, composeAPI, episodeAPI, jobsAPI,
  mergeAPI, sceneAPI, storyboardAPI, uploadAPI, assetAPI, productionAPI,
} from '../../api'
import { confirmAction } from '../../composables/useConfirm'
import { errorMessage } from '../../utils/errorMessage'
import { listOf } from './grid'

export function createWorkbenchMediaActions(deps: Record<string, any>) {
  const { busy, episode, rawContent, show, load, configs, episodeConfigForm, productionError, productionReady, missingProductionServices, showProductionModal, dramaId, productions, currentProduction, startPoll, log, refreshAssets, characters, characterForm, characterError, characterFormModal, showCharacterLibraryImport, characterLibraryError, characterLibraryQuery, characterLibraryLoading, characterLibraryTemplates, sceneForm, sceneError, sceneFormModal, sceneTransfer, drama, assignCharId, selectedShotIds, pendingJobActionIDs, assetTargetShot, assetTargetFrame } = deps

async function saveContent() {
  busy.value = 'save-content'
  try {
    await episodeAPI.update(episode.value.id, { content: rawContent.value })
    show('已保存原文')
    await load()
  } catch (error: any) {
    show(error.message || '保存原文失败')
  } finally {
    busy.value = ''
  }
}

function openEpisodeConfig() {
  const firstConfig = (serviceType: string) => configs.value.find((item: any) => item.service_type === serviceType)?.id || 0
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
  productionError.value = productionReady.value ? '' : `请先绑定${missingProductionServices.value.map((item: any) => item.label).join('、')}服务`
  showProductionModal.value = true
}

async function startProduction() {
  if (!productionReady.value) {
    productionError.value = `请先绑定${missingProductionServices.value.map((item: any) => item.label).join('、')}服务`
    return
  }
  busy.value = 'production-start'
  productionError.value = ''
  try {
    const run = await productionAPI.create(dramaId.value, episode.value.id)
    productions.value = [run, ...productions.value.filter((item: any) => item.id !== run.id)]
    showProductionModal.value = false
    show('自动制作已开始')
    startPoll()
  } catch (error) {
    productionError.value = errorMessage(error, '自动制作启动失败')
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
    productions.value = [updated, ...productions.value.filter((item: any) => item.id !== updated.id)]
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
    productions.value = [updated, ...productions.value.filter((item: any) => item.id !== updated.id)]
    show('自动制作已重新排队')
    startPoll()
  } catch (error: any) {
    show(error.message || '重试失败')
  } finally {
    busy.value = ''
  }
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
    const ids = characters.value.map((c: any) => c.id)
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
  characterFormModal.value?.focus()
}

async function saveCharacter() {
  const form = characterForm.value
  characterError.value = ''
  if (!form?.name?.trim()) {
    characterError.value = '请输入角色名'
    characterFormModal.value?.focus()
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
  if (!await confirmAction({
    title: '删除角色',
    message: `确定删除角色「${character.name}」？`,
    detail: '该角色的形象设定与已生成的图片将不再可用。',
    confirmText: '删除角色',
    tone: 'danger',
  })) return
  try {
    await characterAPI.del(character.id)
    show('角色已删除')
    await refreshAssets()
  } catch (e: any) { show(e.message) }
}

async function saveCharacterToLibrary(character: any) {
  busy.value = `char-library-${character.id}`
  try {
    await characterAPI.saveToLibrary(character.id)
    show(`已将「${character.name}」保存到角色库`)
  } catch (e: any) {
    show(e.message || '保存到角色库失败')
  } finally {
    busy.value = ''
  }
}

async function openCharacterLibraryImport() {
  showCharacterLibraryImport.value = true
  characterLibraryError.value = ''
  characterLibraryQuery.value = ''
  characterLibraryLoading.value = true
  try {
    characterLibraryTemplates.value = listOf(await characterLibraryAPI.list())
  } catch (e: any) {
    characterLibraryTemplates.value = []
    characterLibraryError.value = e.message || '角色库加载失败'
  } finally {
    characterLibraryLoading.value = false
  }
}

function closeCharacterLibraryImport() {
  showCharacterLibraryImport.value = false
  characterLibraryError.value = ''
  characterLibraryQuery.value = ''
}

async function importCharacterFromLibrary(template: any) {
  if (!episode.value?.id) return
  busy.value = `char-import-${template.id}`
  characterLibraryError.value = ''
  try {
    await characterLibraryAPI.import(template.id, dramaId.value, episode.value.id)
    show(`已从角色库导入「${template.name}」`)
    closeCharacterLibraryImport()
    await refreshAssets()
  } catch (e: any) {
    characterLibraryError.value = e.message || '导入角色失败'
  } finally {
    busy.value = ''
  }
}

async function editScene(scene?: any) {
  sceneError.value = ''
  sceneForm.value = scene
    ? { id: scene.id, location: scene.location, time: scene.time || '', prompt: scene.prompt || '' }
    : { location: '', time: '', prompt: '' }
  await nextTick()
  sceneFormModal.value?.focus()
}

async function saveScene() {
  const form = sceneForm.value
  sceneError.value = ''
  if (!form?.location?.trim()) {
    sceneError.value = '请输入场景地点'
    sceneFormModal.value?.focus()
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
  if (!await confirmAction({
    title: '删除场景',
    message: `确定删除场景「${scene.location}」？`,
    detail: '引用该场景的分镜需要重新指定场景。',
    confirmText: '删除场景',
    tone: 'danger',
  })) return
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
  try {
    await characterAPI.update(c.id, { voice_style: voiceId, voice_provider: provider })
    show(`已分配音色 ${voiceId}`)
    assignCharId.value = null
    await refreshAssets()
  } catch (e: any) {
    show(e.message || '分配音色失败')
  }
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
  if (pendingJobActionIDs.value.includes(job.id)) return
  pendingJobActionIDs.value = [...pendingJobActionIDs.value, job.id]
  try {
    await jobsAPI.cancel(job.id)
    show('任务已取消')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    pendingJobActionIDs.value = pendingJobActionIDs.value.filter((id: number) => id !== job.id)
  }
}

async function retryJob(job: any) {
  if (pendingJobActionIDs.value.includes(job.id)) return
  pendingJobActionIDs.value = [...pendingJobActionIDs.value, job.id]
  try {
    await jobsAPI.retry(job.id)
    show('任务已重新排队')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    pendingJobActionIDs.value = pendingJobActionIDs.value.filter((id: number) => id !== job.id)
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

  return { saveContent, openEpisodeConfig, saveEpisodeConfig, openProduction, startProduction, cancelProduction, retryProduction, runAgent, genCharImage, batchCharImages, genSceneImage, uploadBoundImage, editCharacter, saveCharacter, removeCharacter, saveCharacterToLibrary, openCharacterLibraryImport, closeCharacterLibraryImport, importCharacterFromLibrary, editScene, saveScene, removeScene, transferScene, confirmSceneTransfer, voiceSample, assignVoice, genFrame, batchFrames, genVideo, batchVideos, genTTS, batchTTS, composeShot, composeAll, mergeAll, cancelJob, retryJob, applyAsset }
}
