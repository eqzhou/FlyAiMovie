import { nextTick } from 'vue'
import { settingsAPI, storyboardAPI } from '../../api'
import { confirmAction } from '../../composables/useConfirm'
import { errorMessage } from '../../utils/errorMessage'
import { referenceImagesToText } from './labels'

export function createWorkbenchStoryboardActions(deps: Record<string, any>) {
  const { storyboardError, storyboardForm, storyboardFormModal, episode, drama, scenes, characters, selectedStoryboardId, selectedShotIds, busy, show, refreshAssets, promptTemplates, promptEditor, gridPrompt, gridRows, gridCols, gridMode, promptEditorTemplates } = deps

// 镜头与角色的绑定关系保存在后端 storyboard_characters 表，分镜列表接口会同时
// 返回 character_ids 与 characters。这里只把服务端返回值归一化成表单可用的数组，
// 不在前端另存一份状态。
function storyboardCharacterIDs(storyboard: any): number[] {
  const source = Array.isArray(storyboard?.character_ids)
    ? storyboard.character_ids
    : Array.isArray(storyboard?.characters)
      ? storyboard.characters.map((item: any) => item?.id)
      : []
  const ids = source.map((value: any) => Number(value)).filter((value: number) => Number.isInteger(value) && value > 0)
  return Array.from(new Set<number>(ids))
}

function toggleStoryboardCharacter(characterID: number) {
  const form = storyboardForm.value
  if (!form) return
  const current: number[] = Array.isArray(form.character_ids) ? form.character_ids : []
  form.character_ids = current.includes(characterID)
    ? current.filter((value) => value !== characterID)
    : [...current, characterID]
}

function emptyStoryboardForm() {
  return {
    id: 0,
    title: '',
    duration: 12,
    scene_id: 0,
    shot_type: '',
    angle: '',
    movement: '',
    location: '',
    time: '',
    atmosphere: '',
    action: '',
    result: '',
    dialogue: '',
    description: '',
    image_prompt: '',
    video_prompt: '',
    bgm_prompt: '',
    sound_effect: '',
    reference_images: '',
    character_ids: [] as number[],
  }
}

async function openStoryboardForm(storyboard?: any) {
  storyboardError.value = ''
  const blank = emptyStoryboardForm()
  storyboardForm.value = storyboard
    ? {
        ...blank,
        id: Number(storyboard.id) || 0,
        title: storyboard.title || '',
        duration: Number(storyboard.duration) > 0 ? Number(storyboard.duration) : 12,
        scene_id: Number(storyboard.scene_id) || 0,
        shot_type: storyboard.shot_type || '',
        angle: storyboard.angle || '',
        movement: storyboard.movement || '',
        location: storyboard.location || '',
        time: storyboard.time || '',
        atmosphere: storyboard.atmosphere || '',
        action: storyboard.action || '',
        result: storyboard.result || '',
        dialogue: storyboard.dialogue || '',
        description: storyboard.description || '',
        image_prompt: storyboard.image_prompt || '',
        video_prompt: storyboard.video_prompt || '',
        bgm_prompt: storyboard.bgm_prompt || '',
        sound_effect: storyboard.sound_effect || '',
        reference_images: referenceImagesToText(storyboard.reference_images),
        character_ids: storyboardCharacterIDs(storyboard),
      }
    : blank
  await nextTick()
  storyboardFormModal.value?.focus()
}

async function addStoryboard() {
  await openStoryboardForm()
}

async function editStoryboard(storyboard: any) {
  await openStoryboardForm(storyboard)
}

async function saveStoryboard() {
  const form = storyboardForm.value
  storyboardError.value = ''
  if (!form?.title?.trim()) {
    storyboardError.value = '请输入分镜标题'
    storyboardFormModal.value?.focus()
    return
  }
  const duration = Number(form.duration)
  if (!Number.isInteger(duration) || duration < 1 || duration > 3600) {
    storyboardError.value = '时长需为 1-3600 之间的整数（秒）'
    return
  }
  const references = String(form.reference_images || '')
    .split(/\r?\n/)
    .map((value: string) => value.trim())
    .filter(Boolean)
  if (references.length > 8) {
    storyboardError.value = '参考图最多 8 张'
    return
  }
  const sceneID = Number(form.scene_id) || 0
  const characterIDs = Array.isArray(form.character_ids)
    ? Array.from(new Set<number>(form.character_ids.map((value: any) => Number(value)).filter((value: number) => Number.isInteger(value) && value > 0)))
    : []
  const payload: Record<string, any> = {
    title: form.title.trim(),
    duration,
    shot_type: form.shot_type || '',
    angle: form.angle || '',
    movement: form.movement || '',
    location: form.location || '',
    time: form.time || '',
    atmosphere: form.atmosphere || '',
    action: form.action || '',
    result: form.result || '',
    dialogue: form.dialogue || '',
    description: form.description || '',
    image_prompt: form.image_prompt || '',
    video_prompt: form.video_prompt || '',
    bgm_prompt: form.bgm_prompt || '',
    sound_effect: form.sound_effect || '',
    reference_images: JSON.stringify(references),
    character_ids: characterIDs,
  }
  busy.value = 'storyboard-save'
  try {
    if (form.id) {
      await storyboardAPI.update(form.id, { ...payload, scene_id: sceneID > 0 ? sceneID : null })
    } else {
      await storyboardAPI.create({
        ...payload,
        ...(sceneID > 0 ? { scene_id: sceneID } : {}),
        episode_id: episode.value.id,
      })
    }
    const edited = Boolean(form.id)
    storyboardForm.value = null
    show(edited ? '分镜已更新' : '分镜已添加')
    await refreshAssets()
  } catch (error: any) {
    storyboardError.value = error.message || '分镜保存失败'
  } finally {
    busy.value = ''
  }
}

async function removeStoryboard(storyboard: any) {
  if (!await confirmAction({
    title: '删除分镜',
    message: `确定删除分镜「${storyboard.title || `#${storyboard.storyboard_number}`}」？`,
    detail: '该分镜已生成的图片、视频与配音将一并移除。',
    confirmText: '删除分镜',
    tone: 'danger',
  })) return
  busy.value = `storyboard-delete-${storyboard.id}`
  try {
    await storyboardAPI.del(storyboard.id)
    selectedShotIds.value = selectedShotIds.value.filter((id: number) => id !== storyboard.id)
    if (selectedStoryboardId.value === storyboard.id) selectedStoryboardId.value = null
    show('分镜已删除')
    await refreshAssets()
  } catch (error: any) {
    show(error.message)
  } finally {
    busy.value = ''
  }
}

async function copyStoryboard(storyboard: any) {
  busy.value = `storyboard-copy-${storyboard.id}`
  try {
    const copied = await storyboardAPI.copy(storyboard.id)
    show(`已复制镜头「${storyboard.title || ('#' + storyboard.storyboard_number)}」`)
    await refreshAssets()
    if (copied?.id) selectedStoryboardId.value = copied.id
  } catch (error: any) {
    show(error.message || '复制分镜失败')
  } finally {
    busy.value = ''
  }
}

async function moveStoryboard(storyboard: any, direction: 'up' | 'down') {
  busy.value = `storyboard-move-${storyboard.id}`
  try {
    await storyboardAPI.move(storyboard.id, direction)
    show(direction === 'up' ? '镜头已上移' : '镜头已下移')
    await refreshAssets()
  } catch (error: any) {
    show(error.message || '调整镜头顺序失败')
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
  const matching = promptTemplates.value.filter((template: any) => template.is_active !== false && template.category === category)
  promptEditor.value = {
    target: 'storyboard', storyboard: sb, field, label, category,
    value: sb[field] || '', template_id: matching[0]?.id || 0, error: '',
  }
}

function openGridPromptEditor() {
  const matching = promptTemplates.value.filter((template: any) => template.is_active !== false && template.category === 'grid')
  promptEditor.value = {
    target: 'grid', storyboard: null, field: 'grid_prompt', label: '宫格提示词', category: 'grid',
    value: gridPrompt.value, template_id: matching[0]?.id || 0, error: '',
  }
}

function promptRuntimeVariables(editor: any) {
  const storyboard = editor.storyboard || {}
  return {
    drama_title: drama.value?.title || '', episode_title: episode.value?.title || '',
    user_instruction: editor.value || '', character_names: characters.value.map((item: any) => item.name).join('、'),
    scene_names: scenes.value.map((item: any) => `${item.location}${item.time ? `·${item.time}` : ''}`).join('、'),
    shot_title: storyboard.title || '', shot_description: storyboard.description || storyboard.action || '',
    image_prompt: storyboard.image_prompt || '', video_prompt: storyboard.video_prompt || '',
    grid_rows: String(gridRows.value), grid_cols: String(gridCols.value), grid_mode: gridMode.value,
  }
}

function selectedPromptVariables(editor: any) {
  const template = promptTemplates.value.find((item: any) => Number(item.id) === Number(editor.template_id))
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
    editor.error = errorMessage(error, '模板应用失败')
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
    ? selectedShotIds.value.filter((item: number) => item !== id)
    : [...selectedShotIds.value, id]
}

  return { storyboardCharacterIDs, toggleStoryboardCharacter, emptyStoryboardForm, openStoryboardForm, addStoryboard, editStoryboard, saveStoryboard, removeStoryboard, copyStoryboard, moveStoryboard, saveShot, openPromptEditor, openGridPromptEditor, promptRuntimeVariables, selectedPromptVariables, applySelectedPromptTemplate, savePromptEditor, toggleShot }
}
