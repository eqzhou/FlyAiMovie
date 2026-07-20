<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { FlaskConical, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { cacheAPI, memberAPI, organizationDataAPI, quotaAPI, settingsAPI } from '../api'
import { authStore } from '../auth'

const configs = ref<any[]>([])
const router = useRouter()
const providers = ref<any[]>([])
const agents = ref<any[]>([])
const promptTemplates = ref<any[]>([])
const voiceCatalog = ref<any[]>([])
const form = ref({
  service_type: 'text',
  provider: 'openai',
  name: '',
  base_url: 'https://api.openai.com',
  api_key: '',
  model: '',
  is_default: true,
  is_active: true,
})
const toast = ref('')
const loading = ref(true)
const loadError = ref('')
const serviceError = ref('')
const savingService = ref(false)
const testingDraftService = ref(false)
const serviceTestResult = ref('')
const agentForm = ref<any | null>(null)
const activeSection = ref<'services' | 'agents' | 'prompts' | 'voices' | 'organization' | 'security'>('services')
const showServiceModal = ref(false)
const editingConfigID = ref<number | null>(null)
const originalServiceIdentity = ref({ service_type: '', provider: '', base_url: '', api_key_set: false })
const hydratingServiceForm = ref(false)
const testingConfigID = ref<number | null>(null)
const showMemberModal = ref(false)
const showInviteModal = ref(false)
const showPasswordModal = ref(false)
const showDeleteModal = ref(false)
const promptForm = ref<any | null>(null)
const promptContentInput = ref<HTMLTextAreaElement | null>(null)
const promptQuery = ref('')
const promptCategory = ref('all')
const promptState = ref('all')
const promptFormError = ref('')
const promptDraftPreview = ref('')
const previewingPromptDraft = ref(false)
const savingPrompt = ref(false)
const previewTemplate = ref<any | null>(null)
const revisionTemplate = ref<any | null>(null)
const promptRevisions = ref<any[]>([])
const restoringRevision = ref<number | null>(null)
const promptHistoryLoading = ref(false)
const promptHistoryError = ref('')
const promptActionNotice = ref('')
let promptHistoryRequestID = 0
let promptDraftRequestID = 0
const voiceQuery = ref('')
const voicePreviewText = ref('欢迎来到 FlyAiMovie，这是音色试听。')
const previewingVoiceID = ref('')
const syncingVoices = ref(false)
const voicePreviewURLs = ref<Record<string, string>>({})
const previewVariables = ref<Record<string, string>>({})
const previewResult = ref('')
const previewError = ref('')
const builtInPromptKeys = new Set(['script_rewriter', 'extractor', 'storyboard_breaker', 'voice_assigner', 'grid_prompt_generator', 'storyboard_image', 'storyboard_video', 'grid_composition', 'character_image', 'scene_image', 'prop_image'])
const promptCategoryLabels: Record<string, string> = { agent_system: 'Agent 系统', grid: '宫格', image: '图片', video: '视频', audio: '音频' }
const promptVariableLabels: Record<string, string> = {
  drama_title: '项目标题', episode_title: '剧集标题', user_instruction: '用户要求',
  character_names: '角色列表', scene_names: '场景列表', shot_title: '镜头标题',
  shot_description: '镜头描述', image_prompt: '图片提示词', video_prompt: '视频提示词',
  grid_rows: '宫格行数', grid_cols: '宫格列数', grid_mode: '宫格模式',
  character_name: '角色姓名', character_role: '角色定位', character_appearance: '角色外貌',
  character_description: '角色描述', character_personality: '角色性格',
  scene_location: '场景地点', scene_time: '场景时间', scene_prompt: '场景提示词',
  prop_name: '道具名称', prop_type: '道具类型', prop_description: '道具描述', prop_prompt: '道具提示词',
}
const promptExampleValues: Record<string, string> = {
  drama_title: '示例短剧', episode_title: '第一集', user_instruction: '保持人物与画面连续',
  character_names: '林夏、周远', scene_names: '雨夜车站、旧城区', shot_title: '雨中重逢',
  shot_description: '主角穿过站台，在列车灯光中停下', image_prompt: '电影感雨夜站台，人物清晰',
  video_prompt: '缓慢推镜，雨水与衣摆自然运动', grid_rows: '3', grid_cols: '3', grid_mode: '首帧',
  character_name: '林夏', character_role: '女主角', character_appearance: '黑色短发，风衣，冷静眼神',
  character_description: '前记者，擅长观察细节', character_personality: '沉稳、敏锐',
  scene_location: '雨夜车站', scene_time: '夜', scene_prompt: '潮湿站台，列车灯光扫过地面',
  prop_name: '旧怀表', prop_type: '信物', prop_description: '黄铜外壳，表盘有裂痕', prop_prompt: '静物特写，暖光',
}
const providerChoices: Record<string, Array<{ value: string; label: string; base: string }>> = {
  text: [
    { value: 'openai', label: 'OpenAI / Compatible', base: 'https://api.openai.com' },
    { value: 'openai_local', label: '本地 OpenAI Compatible', base: 'http://host.docker.internal:11434' },
    { value: 'chatfire', label: 'Chatfire Gateway', base: '' },
    { value: 'mock', label: 'Mock (离线演示)', base: 'http://localhost' },
  ],
  image: [
    { value: 'openai', label: 'OpenAI Image', base: 'https://api.openai.com' },
    { value: 'gemini', label: 'Gemini Image', base: 'https://generativelanguage.googleapis.com' },
    { value: 'minimax', label: 'MiniMax Image', base: 'https://api.minimax.chat' },
    { value: 'volcengine', label: 'Volcengine Seedream', base: 'https://ark.cn-beijing.volces.com' },
    { value: 'ali', label: 'Aliyun DashScope', base: 'https://dashscope.aliyuncs.com' },
    { value: 'chatfire', label: 'Chatfire Gateway', base: '' },
    { value: 'mock', label: 'Mock (离线演示)', base: 'http://localhost' },
  ],
  video: [
    { value: 'openai', label: 'OpenAI Sora', base: 'https://api.openai.com' },
    { value: 'minimax', label: 'MiniMax Video', base: 'https://api.minimax.chat' },
    { value: 'volcengine', label: 'Volcengine Seedance', base: 'https://ark.cn-beijing.volces.com' },
    { value: 'vidu', label: 'Vidu', base: 'https://api.vidu.com' },
    { value: 'ali', label: 'Aliyun DashScope', base: 'https://dashscope.aliyuncs.com' },
    { value: 'mock', label: 'Mock (离线演示)', base: 'http://localhost' },
  ],
  audio: [
    { value: 'minimax', label: 'MiniMax TTS', base: 'https://api.minimax.chat' },
    { value: 'mock', label: 'Mock (离线演示)', base: 'http://localhost' },
  ],
}
const quota = ref({ daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 0, active_jobs: 0, daily_budget_cny: 0, budget_warning_percent: 80, budget_used_cny: 0, budget_warning: false })
const cache = ref({ objects: 0, references: 0, bytes: 0, orphaned: 0 })
const canManageQuota = computed(() => !authStore.state.enabled || ['owner', 'admin'].includes(authStore.state.actor?.role || ''))
const canManageSettings = computed(() => !authStore.state.enabled || ['owner', 'admin'].includes(authStore.state.actor?.role || ''))
const canPreviewVoices = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const members = ref<any[]>([])
const memberForm = ref({ email: '', display_name: '', password: '', role: 'editor' })
const inviteForm = ref({ email: '', role: 'editor', ttl_hours: 72 })
const inviteLink = ref('')
const invitations = ref<any[]>([])
const passwordForm = ref({ current: '', next: '', confirm: '' })
const deleteForm = ref({ password: '', confirmation: '' })
const availableProviders = computed(() => providerChoices[form.value.service_type] || [])
const filteredVoices = computed(() => {
  const keyword = voiceQuery.value.trim().toLocaleLowerCase('zh-CN')
  if (!keyword) return voiceCatalog.value
  return voiceCatalog.value.filter((voice) => [voice.voice_name, voice.voice_id, voice.language, voice.provider, voice.capabilities]
    .some((value) => String(value || '').toLocaleLowerCase('zh-CN').includes(keyword)))
})
const filteredPromptTemplates = computed(() => {
  const keyword = promptQuery.value.trim().toLocaleLowerCase('zh-CN')
  return promptTemplates.value.filter((template) => {
    const matchesQuery = !keyword || [template.name, template.key, template.description, template.content]
      .some((value) => String(value || '').toLocaleLowerCase('zh-CN').includes(keyword))
    const matchesCategory = promptCategory.value === 'all' || template.category === promptCategory.value
    const matchesState = promptState.value === 'all' || (promptState.value === 'active' ? template.is_active !== false : template.is_active === false)
    return matchesQuery && matchesCategory && matchesState
  })
})
const serviceTypeLabels: Record<string, string> = { text: '文本', image: '图片', video: '视频', audio: '音频 / TTS' }
const agentTypeLabels: Record<string, string> = {
  script_rewriter: '剧本改写', extractor: '角色场景提取', storyboard_breaker: '分镜拆解',
  voice_assigner: '音色分配', grid_prompt_generator: '宫格提示词',
}

function serviceTypeLabel(value: string) { return serviceTypeLabels[value] || value }
function providerLabel(config: any) {
  return providerChoices[config.service_type]?.find((choice) => choice.value === config.provider)?.label || config.provider
}
function agentTypeLabel(value: string) { return agentTypeLabels[value] || value }

function show(m: string) {
  toast.value = m
  setTimeout(() => (toast.value = ''), 2000)
}

async function load() {
  loading.value = true
  loadError.value = ''
  const requests: Array<{ label: string; request: Promise<any>; apply: (value: any) => void }> = [
    { label: 'AI 服务', request: settingsAPI.aiConfigs(), apply: (value: any) => { configs.value = value } },
    { label: '厂商目录', request: settingsAPI.providers(), apply: (value: any) => { providers.value = value } },
    { label: 'Agent', request: settingsAPI.agentConfigs(), apply: (value: any) => { agents.value = value } },
    { label: '提示词', request: settingsAPI.promptTemplates(), apply: (value: any) => { promptTemplates.value = value } },
    { label: '音色', request: settingsAPI.voices(true), apply: (value: any) => { voiceCatalog.value = value } },
    { label: '配额', request: quotaAPI.get(), apply: (value: any) => { quota.value = value } },
    { label: '缓存', request: cacheAPI.stats(), apply: (value: any) => { cache.value = value } },
  ]
  if (canManageQuota.value && authStore.state.enabled) {
    requests.push(
      { label: '成员', request: memberAPI.list(), apply: (value: any) => { members.value = value } },
      { label: '邀请', request: memberAPI.invitations(), apply: (value: any) => { invitations.value = value } },
    )
  }
  const results = await Promise.allSettled(requests.map((item) => item.request))
  const failures: string[] = []
  results.forEach((result, index) => {
    if (result.status === 'fulfilled') requests[index].apply(result.value)
    else failures.push(result.reason instanceof Error ? result.reason.message : `${requests[index].label}加载失败`)
  })
  loadError.value = failures.join('；')
  loading.value = false
}

async function addMember() {
  await memberAPI.create(memberForm.value)
  memberForm.value = { email: '', display_name: '', password: '', role: 'editor' }
  showMemberModal.value = false
  show('成员已添加')
  members.value = await memberAPI.list()
  await authStore.refreshOrganizations()
}

async function inviteMember() {
  const result = await memberAPI.invite(inviteForm.value)
  inviteLink.value = `${window.location.origin}/invite/${encodeURIComponent(result.token)}`
  inviteForm.value = { email: '', role: 'editor', ttl_hours: 72 }
  show('邀请已创建，请复制链接发送给成员')
  invitations.value = await memberAPI.invitations()
}

async function revokeInvitation(invitation: any) {
  if (!confirm(`撤销发往 ${invitation.email} 的邀请？`)) return
  await memberAPI.revokeInvitation(invitation.id)
  invitations.value = await memberAPI.invitations()
}

async function resendInvitation(invitation: any) {
  const result = await memberAPI.resendInvitation(invitation.id)
  inviteLink.value = `${window.location.origin}/invite/${encodeURIComponent(result.token)}`
  showInviteModal.value = true
  invitations.value = await memberAPI.invitations()
  show('邀请已重发')
}

async function changeMemberRole(member: any) {
  await memberAPI.update(member.user_id, member.role)
  show('角色已更新')
}

async function removeMember(member: any) {
  if (!confirm(`移除 ${member.email}？`)) return
  await memberAPI.remove(member.user_id)
  members.value = await memberAPI.list()
}

async function changePassword() {
  if (passwordForm.value.next !== passwordForm.value.confirm) { show('两次新密码不一致'); return }
  await authStore.changePassword(passwordForm.value.current, passwordForm.value.next)
  passwordForm.value = { current: '', next: '', confirm: '' }
  showPasswordModal.value = false
  show('密码已更新')
}

async function exportOrganization() {
  const data = await organizationDataAPI.export()
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `${authStore.state.actor?.organization.slug || 'organization'}-export.json`
  anchor.click()
  URL.revokeObjectURL(url)
}

async function deleteOrganization() {
  if (!confirm('永久删除组织及其全部数据？此操作无法撤销。')) return
  await organizationDataAPI.remove(deleteForm.value.password, deleteForm.value.confirmation)
  await authStore.logout()
  await router.replace('/login')
}

async function saveQuota() {
  await quotaAPI.update({ daily_job_limit: quota.value.daily_job_limit, max_active_jobs: quota.value.max_active_jobs, daily_budget_cny: quota.value.daily_budget_cny, budget_warning_percent: quota.value.budget_warning_percent })
  show('生成配额已保存')
  quota.value = await quotaAPI.get()
}

async function purgeCache() {
  if (!confirm('清理当前组织的过期缓存？')) return
  await cacheAPI.purge()
  cache.value = await cacheAPI.stats()
  show('过期缓存已清理')
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

function emptyServiceForm() {
  return { service_type: 'text', provider: 'openai', name: '', base_url: 'https://api.openai.com', api_key: '', model: '', is_default: true, is_active: true }
}

function emptyServiceIdentity() {
  return { service_type: '', provider: '', base_url: '', api_key_set: false }
}

function serviceValidationMessage() {
  if (!form.value.name.trim()) return '名称必填'
  const original = originalServiceIdentity.value
  const identityChanged = Boolean(editingConfigID.value && (
    original.service_type !== form.value.service_type
    || original.provider !== form.value.provider
    || original.base_url !== form.value.base_url.trim()
  ))
  if (identityChanged && original.api_key_set && !form.value.api_key) {
    return '更改类型、厂商或 Base URL 后，请重新填写 API Key'
  }
  const keyOptional = ['mock', 'openai_local'].includes(form.value.provider)
  const canReuseStoredKey = Boolean(editingConfigID.value && original.api_key_set && !identityChanged)
  if (!keyOptional && !canReuseStoredKey && !form.value.api_key) return 'API Key 必填'
  return ''
}

function openCreateService() {
  serviceError.value = ''
  serviceTestResult.value = ''
  editingConfigID.value = null
  originalServiceIdentity.value = emptyServiceIdentity()
  form.value = emptyServiceForm()
  showServiceModal.value = true
}

async function editService(config: any) {
  serviceError.value = ''
  serviceTestResult.value = ''
  hydratingServiceForm.value = true
  editingConfigID.value = config.id
  originalServiceIdentity.value = {
    service_type: config.service_type,
    provider: config.provider,
    base_url: (config.base_url || '').trim(),
    api_key_set: Boolean(config.api_key_set),
  }
  form.value = {
    service_type: config.service_type,
    provider: config.provider,
    name: config.name,
    base_url: config.base_url || '',
    api_key: '',
    model: config.model || '',
    is_default: Boolean(config.is_default),
    is_active: config.is_active !== false,
  }
  showServiceModal.value = true
  await nextTick()
  hydratingServiceForm.value = false
}

async function saveService() {
  serviceError.value = ''
  const validationMessage = serviceValidationMessage()
  if (validationMessage) {
    serviceError.value = validationMessage
    return
  }
  savingService.value = true
  try {
    if (editingConfigID.value) {
      await settingsAPI.updateAIConfig(editingConfigID.value, form.value)
    } else {
      await settingsAPI.createAIConfig(form.value)
    }
    form.value = { ...form.value, api_key: '' }
    showServiceModal.value = false
    show(editingConfigID.value ? 'AI 服务已更新' : 'AI 服务已添加')
    editingConfigID.value = null
    originalServiceIdentity.value = emptyServiceIdentity()
    await load()
  } catch (error) {
    serviceError.value = error instanceof Error ? error.message : 'AI 服务保存失败'
  } finally {
    savingService.value = false
  }
}

async function testService(config: any) {
  testingConfigID.value = config.id
  try {
    const result = await settingsAPI.testAIConfig(config.id)
    show(`${result.detail} · ${result.latency_ms} ms`)
  } catch (error) {
    show(`连接失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    testingConfigID.value = null
  }
}

async function testDraftService() {
  serviceError.value = ''
  serviceTestResult.value = ''
  const validationMessage = serviceValidationMessage()
  if (validationMessage) {
    serviceError.value = validationMessage
    return
  }
  testingDraftService.value = true
  try {
    const result = await settingsAPI.testAIConfigDraft({ id: editingConfigID.value || undefined, ...form.value })
    serviceTestResult.value = `${result.detail} · ${result.latency_ms} ms`
  } catch (error) {
    serviceError.value = `连接失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    testingDraftService.value = false
  }
}

watch(() => form.value.service_type, () => {
  const first = availableProviders.value[0]
  if (!availableProviders.value.some((item) => item.value === form.value.provider) && first) {
    form.value.provider = first.value
    form.value.base_url = first.base
  }
})

watch(() => form.value.provider, (provider) => {
  if (hydratingServiceForm.value) return
  const selected = availableProviders.value.find((item) => item.value === provider)
  if (selected) form.value.base_url = selected.base
})

watch(() => form.value.is_active, (active) => {
  if (!active) form.value = { ...form.value, is_default: false }
})

watch(form, () => {
  if (!hydratingServiceForm.value && !testingDraftService.value) serviceTestResult.value = ''
}, { deep: true })

watch(() => promptForm.value?.content, () => {
  promptDraftRequestID += 1
  promptFormError.value = ''
  promptDraftPreview.value = ''
  previewingPromptDraft.value = false
})

async function remove(id: number) {
  if (!confirm('删除该配置？')) return
  await settingsAPI.deleteAIConfig(id)
  await load()
}

async function syncVoices() {
  syncingVoices.value = true
  try {
    const res = await settingsAPI.syncVoices()
    voiceCatalog.value = await settingsAPI.voices(true)
    show(`已同步 ${res.count ?? voiceCatalog.value.length} 个音色`)
  } catch (error) {
    show(`同步失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    syncingVoices.value = false
  }
}

async function previewVoice(voice: any) {
  const text = voicePreviewText.value.trim()
  if (!text) { show('请填写试听文本'); return }
  previewingVoiceID.value = voice.voice_id
  try {
    const result = await settingsAPI.previewVoice(voice.voice_id, text)
    voicePreviewURLs.value = { ...voicePreviewURLs.value, [voice.voice_id]: result.audio_url }
    show('试听已生成')
  } catch (error) {
    show(`试听失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    previewingVoiceID.value = ''
  }
}

function editAgent(agent: any) {
  agentForm.value = {
    id: agent.id,
    agent_type: agent.agent_type,
    name: agent.name,
    description: agent.description || '',
    model: agent.model || '',
    system_prompt: agent.system_prompt || '',
    temperature: agent.temperature ?? 0.4,
    max_tokens: agent.max_tokens ?? 4096,
    max_iterations: agent.max_iterations ?? 2,
    is_active: agent.is_active,
  }
}

function promptVariables(template: any): string[] {
  try { return JSON.parse(template.variables_json || '[]') } catch { return [] }
}

function promptToken(variable: string) { return `{{${variable}}}` }
function isBuiltInPrompt(template: any) { return builtInPromptKeys.has(template.key) }

function resetPromptDraftFeedback() {
  promptDraftRequestID += 1
  promptFormError.value = ''
  promptDraftPreview.value = ''
  previewingPromptDraft.value = false
}

function openCreatePrompt() {
  resetPromptDraftFeedback()
  promptForm.value = { id: null, key: '', name: '', category: 'image', description: '', content: '', is_active: true }
}

function duplicatePrompt(template: any) {
  resetPromptDraftFeedback()
  promptForm.value = {
    id: null,
    key: `${String(template.key || 'prompt').slice(0, 59)}_copy`,
    name: `${template.name}副本`,
    category: template.category,
    description: template.description || '',
    content: template.content,
    is_active: template.is_active !== false,
  }
}

function editPrompt(template: any) {
  resetPromptDraftFeedback()
  promptForm.value = { id: template.id, key: template.key, name: template.name, category: template.category, description: template.description || '', content: template.content, is_active: template.is_active !== false }
}

function promptDraftVariables(content: string) {
  const names: string[] = []
  const seen = new Set<string>()
  for (const match of content.matchAll(/\{\{([a-z][a-z0-9_]*)\}\}/g)) {
    if (!seen.has(match[1])) {
      seen.add(match[1])
      names.push(match[1])
    }
  }
  return names
}

async function previewPromptForm() {
  if (!promptForm.value || !String(promptForm.value.content || '').trim()) {
    promptFormError.value = '请先填写模板内容'
    return
  }
  const requestID = ++promptDraftRequestID
  previewingPromptDraft.value = true
  promptFormError.value = ''
  promptDraftPreview.value = ''
  const content = String(promptForm.value.content)
  const variables = Object.fromEntries(promptDraftVariables(content).map((name) => [name, promptExampleValues[name] || '示例内容']))
  try {
    const result = await settingsAPI.previewPromptDraft(content, variables)
    if (requestID !== promptDraftRequestID || String(promptForm.value?.content || '') !== content) return
    promptDraftPreview.value = result.rendered
  } catch (error) {
    if (requestID !== promptDraftRequestID || String(promptForm.value?.content || '') !== content) return
    promptFormError.value = error instanceof Error ? error.message : '模板检查失败'
  } finally {
    if (requestID === promptDraftRequestID) previewingPromptDraft.value = false
  }
}

async function insertPromptVariable(variable: string) {
  if (!promptForm.value) return
  const token = promptToken(variable)
  const input = promptContentInput.value
  const content = String(promptForm.value.content || '')
  const start = input?.selectionStart ?? content.length
  const end = input?.selectionEnd ?? start
  promptForm.value = { ...promptForm.value, content: `${content.slice(0, start)}${token}${content.slice(end)}` }
  await nextTick()
  promptContentInput.value?.focus()
  promptContentInput.value?.setSelectionRange(start + token.length, start + token.length)
}

async function savePrompt() {
  if (!promptForm.value) return
  const data = { key: promptForm.value.key, name: promptForm.value.name, category: promptForm.value.category, description: promptForm.value.description, content: promptForm.value.content, is_active: promptForm.value.is_active }
  if (!data.key.trim() || !data.name.trim() || !data.content.trim()) { promptFormError.value = '标识、名称和模板内容必填'; return }
  savingPrompt.value = true
  promptFormError.value = ''
  try {
    if (promptForm.value.id) await settingsAPI.updatePromptTemplate(promptForm.value.id, data)
    else await settingsAPI.createPromptTemplate(data)
    show(promptForm.value.id ? '提示词已更新' : '提示词已创建')
    promptForm.value = null
    promptTemplates.value = await settingsAPI.promptTemplates()
  } catch (error) {
    promptFormError.value = error instanceof Error ? error.message : '保存失败'
  } finally {
    savingPrompt.value = false
  }
}

function openPreview(template: any) {
  previewTemplate.value = template
  previewVariables.value = Object.fromEntries(promptVariables(template).map((name) => [name, promptExampleValues[name] || '示例内容']))
  previewResult.value = ''
  previewError.value = ''
}

async function renderPreview() {
  if (!previewTemplate.value) return
  previewError.value = ''
  try {
    const result = await settingsAPI.previewPromptTemplate(previewTemplate.value.id, previewVariables.value)
    previewResult.value = result.rendered
  } catch (error) {
    previewResult.value = ''
    previewError.value = error instanceof Error ? error.message : '预览失败'
  }
}

async function restorePrompt(template: any) {
  if (!confirm(`恢复「${template.name}」的内置模板？`)) return
  await settingsAPI.restorePromptTemplate(template.id)
  promptTemplates.value = await settingsAPI.promptTemplates()
  show('已恢复内置模板')
}

async function openPromptHistory(template: any) {
  const requestID = ++promptHistoryRequestID
  const templateID = Number(template.id)
  revisionTemplate.value = { ...template }
  promptRevisions.value = []
  promptHistoryError.value = ''
  promptHistoryLoading.value = true
  try {
    const revisions = await settingsAPI.promptTemplateRevisions(templateID)
    if (requestID !== promptHistoryRequestID || Number(revisionTemplate.value?.id) !== templateID) return
    promptRevisions.value = revisions
  } catch (error) {
    if (requestID !== promptHistoryRequestID || Number(revisionTemplate.value?.id) !== templateID) return
    promptHistoryError.value = error instanceof Error ? error.message : '版本历史加载失败'
  } finally {
    if (requestID === promptHistoryRequestID && Number(revisionTemplate.value?.id) === templateID) promptHistoryLoading.value = false
  }
}

function closePromptHistory() {
  promptHistoryRequestID += 1
  revisionTemplate.value = null
  promptRevisions.value = []
  promptHistoryError.value = ''
  promptHistoryLoading.value = false
}

async function restorePromptRevision(revision: any) {
  if (!revisionTemplate.value || revision.version === revisionTemplate.value.version) return
  const templateID = Number(revisionTemplate.value.id)
  if (revision.template_id && Number(revision.template_id) !== templateID) {
    promptHistoryError.value = '版本记录与当前模板不匹配，请重新打开版本历史'
    return
  }
  restoringRevision.value = revision.version
  promptHistoryError.value = ''
  try {
    await settingsAPI.restorePromptTemplateRevision(templateID, revision.version)
    closePromptHistory()
    await load()
    promptActionNotice.value = '已恢复为新版本'
    show('已恢复为新版本')
  } catch (error) {
    promptHistoryError.value = error instanceof Error ? error.message : '版本恢复失败'
  } finally {
    restoringRevision.value = null
  }
}

function formatRevisionTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

async function removePrompt(template: any) {
  if (!confirm(`删除「${template.name}」？`)) return
  await settingsAPI.deletePromptTemplate(template.id)
  promptTemplates.value = await settingsAPI.promptTemplates()
  show('提示词已删除')
}

async function saveAgent() {
  if (!agentForm.value) return
  await settingsAPI.upsertAgentConfig(agentForm.value)
  show('Agent 配置已保存')
  agentForm.value = null
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">设置</h1>
        <p class="page-desc">管理制作空间的服务、自动化与访问权限</p>
      </div>
    </div>

    <div class="settings-tabs" role="tablist" aria-label="设置分类">
      <button role="tab" :aria-selected="activeSection === 'services'" :class="{ active: activeSection === 'services' }" @click="activeSection = 'services'">AI 服务</button>
      <button role="tab" :aria-selected="activeSection === 'agents'" :class="{ active: activeSection === 'agents' }" @click="activeSection = 'agents'">Agent</button>
      <button role="tab" :aria-selected="activeSection === 'prompts'" :class="{ active: activeSection === 'prompts' }" @click="activeSection = 'prompts'">提示词</button>
      <button role="tab" :aria-selected="activeSection === 'voices'" :class="{ active: activeSection === 'voices' }" @click="activeSection = 'voices'">音色库</button>
      <button role="tab" :aria-selected="activeSection === 'organization'" :class="{ active: activeSection === 'organization' }" @click="activeSection = 'organization'">组织与权限</button>
      <button role="tab" :aria-selected="activeSection === 'security'" :class="{ active: activeSection === 'security' }" @click="activeSection = 'security'">安全与数据</button>
    </div>
    <div v-if="loadError" class="inline-alert" role="alert"><div><strong>部分设置暂未更新</strong><span>{{ loadError }}</span></div><button class="btn" type="button" @click="load">重试加载</button></div>
    <div v-else-if="loading" class="settings-loading" role="status">正在同步设置…</div>

    <section v-if="activeSection === 'services'" class="settings-section" role="tabpanel">
      <div class="settings-section-head">
        <div><h2>AI 服务</h2><p class="muted">{{ configs.length }} 个已配置服务 · {{ providers.length }} 个内置厂商模板</p></div>
        <div v-if="canManageSettings" class="toolbar"><button class="btn btn-primary" @click="openCreateService"><Plus :size="16" aria-hidden="true" />添加 AI 服务</button></div>
      </div>
      <div class="panel">
        <table class="table">
          <thead><tr><th>名称</th><th>类型</th><th>厂商</th><th>模型</th><th>状态</th><th></th></tr></thead>
          <tbody>
            <tr v-for="c in configs" :key="c.id">
              <td>{{ c.name }}</td>
              <td>{{ serviceTypeLabel(c.service_type) }}</td>
              <td>{{ providerLabel(c) }}</td>
              <td>{{ c.model || '厂商默认' }}</td>
              <td><div class="service-status"><span :class="c.is_active ? 'ok' : 'off'">{{ c.is_active ? '启用' : '停用' }}</span><span v-if="c.is_default" class="default">默认</span></div></td>
              <td><div v-if="canManageSettings" class="toolbar settings-table-actions"><button class="btn" @click="editService(c)"><Pencil :size="14" aria-hidden="true" />编辑</button><button class="btn" :disabled="testingConfigID !== null" @click="testService(c)"><FlaskConical :size="14" aria-hidden="true" />{{ testingConfigID === c.id ? '测试中…' : '测试连接' }}</button><button class="btn btn-danger" :aria-label="`删除 ${c.name}`" title="删除服务" @click="remove(c.id)"><Trash2 :size="14" aria-hidden="true" /></button></div></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!configs.length" class="empty">尚未配置 AI 服务</div>
      </div>
    </section>

    <section v-if="activeSection === 'voices'" class="settings-section" role="tabpanel">
      <div class="settings-section-head">
        <div><h2>音色库</h2><p class="muted">{{ voiceCatalog.length }} 个音色 · {{ voiceCatalog.filter((voice) => voice.is_active).length }} 个可用</p></div>
        <button v-if="canManageSettings" class="btn btn-primary" :disabled="syncingVoices" @click="syncVoices"><RefreshCw :size="15" aria-hidden="true" />{{ syncingVoices ? '同步中…' : '同步音色' }}</button>
      </div>
      <div class="voice-catalog-toolbar">
        <label class="library-search"><span class="sr-only">搜索音色</span><input v-model="voiceQuery" type="search" aria-label="搜索音色" placeholder="搜索名称、语言或 Voice ID" /></label>
        <div v-if="canPreviewVoices" class="field voice-preview-text"><label for="voice-preview-text">试听文本</label><input id="voice-preview-text" v-model="voicePreviewText" maxlength="200" /></div>
      </div>
      <div class="panel voice-catalog-panel">
        <table v-if="filteredVoices.length" class="table voice-catalog-table">
          <thead><tr><th>音色</th><th>Voice ID</th><th>厂商</th><th>语言</th><th>类型</th><th>状态</th><th></th></tr></thead>
          <tbody><tr v-for="voice in filteredVoices" :key="`${voice.provider}-${voice.voice_id}`"><td><strong>{{ voice.voice_name || voice.voice_id }}</strong><audio v-if="voicePreviewURLs[voice.voice_id]" :aria-label="`${voice.voice_name || voice.voice_id}试听音频`" :src="voicePreviewURLs[voice.voice_id]" controls preload="metadata" /></td><td><code>{{ voice.voice_id }}</code></td><td>{{ voice.provider }}</td><td>{{ voice.language || '未标注' }}</td><td>{{ voice.capabilities || '通用' }}</td><td><span class="job-status" :class="voice.is_active ? 'succeeded' : 'canceled'">{{ voice.is_active ? '启用' : '失效' }}</span></td><td><button v-if="canPreviewVoices && voice.is_active" class="btn" :disabled="!!previewingVoiceID || !voicePreviewText.trim()" :aria-label="`试听${voice.voice_name || voice.voice_id}`" @click="previewVoice(voice)">{{ previewingVoiceID === voice.voice_id ? '生成中…' : '试听' }}</button></td></tr></tbody>
        </table>
        <div v-else class="empty">{{ voiceCatalog.length ? '没有匹配的音色' : '尚未同步音色' }}</div>
      </div>
    </section>

    <section v-if="activeSection === 'prompts'" class="settings-section" role="tabpanel">
      <div class="settings-section-head">
        <div><h2>提示词模板</h2><p class="muted">{{ promptTemplates.length }} 个模板 · 组织内生效</p></div>
        <button v-if="canManageSettings" class="btn btn-primary" @click="openCreatePrompt">新建提示词</button>
      </div>
      <div v-if="promptActionNotice" class="inline-alert" role="status"><div><strong>{{ promptActionNotice }}</strong><span>提示词列表已刷新，新版本已生效。</span></div><button class="btn" type="button" aria-label="关闭提示词操作提示" @click="promptActionNotice = ''">关闭</button></div>
      <div class="prompt-template-toolbar">
        <label class="library-search"><span class="sr-only">搜索提示词</span><input v-model="promptQuery" type="search" aria-label="搜索提示词" placeholder="搜索名称、标识或内容" /></label>
        <label><span class="sr-only">提示词分类</span><select v-model="promptCategory" aria-label="提示词分类"><option value="all">全部分类</option><option v-for="(label, value) in promptCategoryLabels" :key="value" :value="value">{{ label }}</option></select></label>
        <label><span class="sr-only">提示词状态</span><select v-model="promptState" aria-label="提示词状态"><option value="all">全部状态</option><option value="active">启用</option><option value="inactive">停用</option></select></label>
      </div>
      <div class="panel prompt-template-panel">
        <table v-if="filteredPromptTemplates.length" class="table prompt-template-table">
          <thead><tr><th>名称</th><th>分类</th><th>变量</th><th>版本</th><th>状态</th><th></th></tr></thead>
          <tbody>
            <tr v-for="template in filteredPromptTemplates" :key="template.id">
              <td><strong>{{ template.name }}</strong><p class="muted prompt-template-description">{{ template.description || template.key }}</p></td>
              <td>{{ promptCategoryLabels[template.category] || template.category }}</td>
              <td><div class="prompt-variable-list"><code v-for="variable in promptVariables(template)" :key="variable">{{ variable }}</code><span v-if="!promptVariables(template).length" class="muted">无</span></div></td>
              <td>v{{ template.version }}</td><td>{{ template.is_active ? '启用' : '停用' }}</td>
              <td><div class="toolbar settings-table-actions"><button class="btn" @click="openPreview(template)">预览</button><button class="btn" @click="openPromptHistory(template)">版本历史</button><button v-if="canManageSettings" class="btn" @click="duplicatePrompt(template)">复制</button><button v-if="canManageSettings" class="btn" @click="editPrompt(template)">编辑</button><button v-if="canManageSettings && isBuiltInPrompt(template)" class="btn" @click="restorePrompt(template)">恢复默认</button><button v-if="canManageSettings && !isBuiltInPrompt(template)" class="btn btn-danger" @click="removePrompt(template)">删除</button></div></td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">{{ promptTemplates.length ? '没有匹配的提示词模板' : '尚未创建提示词模板' }}</div>
      </div>
    </section>

    <section v-if="activeSection === 'agents'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>Agent 预设</h2><p class="muted">为制作流程配置模型和执行边界</p></div></div>
      <div class="panel">
        <table class="table">
          <thead><tr><th>类型</th><th>名称</th><th>模型</th><th>状态</th><th></th></tr></thead>
          <tbody><tr v-for="a in agents" :key="a.id"><td>{{ agentTypeLabel(a.agent_type) }}</td><td>{{ a.name }}</td><td>{{ a.model || '继承文本默认' }}</td><td><span class="job-status" :class="a.is_active ? 'succeeded' : 'canceled'">{{ a.is_active ? '启用' : '停用' }}</span></td><td><button v-if="canManageSettings" class="btn" @click="editAgent(a)">编辑</button></td></tr></tbody>
        </table>
      </div>
    </section>

    <section v-if="activeSection === 'organization'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>组织与权限</h2><p class="muted">成员访问、生成额度与本地存储</p></div><div v-if="canManageQuota && authStore.state.enabled" class="toolbar"><button class="btn" @click="showMemberModal = true">添加成员</button><button class="btn btn-primary" @click="showInviteModal = true; inviteLink = ''">创建邀请</button></div></div>
      <div class="panel">
        <h3>生成配额</h3>
        <div class="field-grid"><div class="field"><label>每日任务上限</label><input v-model.number="quota.daily_job_limit" type="number" min="1" max="100000" :disabled="!canManageQuota" /></div><div class="field"><label>并发任务上限</label><input v-model.number="quota.max_active_jobs" type="number" min="1" max="1000" :disabled="!canManageQuota" /></div><div class="field"><label>每日预算（CNY，0 为不限）</label><input v-model.number="quota.daily_budget_cny" type="number" min="0" step="0.01" :disabled="!canManageQuota" /></div><div class="field"><label>预算预警阈值（%）</label><input v-model.number="quota.budget_warning_percent" type="number" min="1" max="100" :disabled="!canManageQuota" /></div></div>
        <div class="toolbar settings-panel-actions"><button v-if="canManageQuota" class="btn btn-primary" @click="saveQuota">保存配额</button><span class="muted">今日任务 {{ quota.daily_jobs_used }} · 当前活跃 {{ quota.active_jobs }} · 已计预算 ¥{{ quota.budget_used_cny.toFixed(2) }}</span></div>
      </div>
      <div class="panel">
        <h3>本地缓存</h3>
        <div class="toolbar settings-panel-actions"><span>对象 {{ cache.objects }}</span><span>引用 {{ cache.references }}</span><span>容量 {{ formatBytes(cache.bytes) }}</span><span>待回收 {{ cache.orphaned }}</span><button v-if="canManageQuota" class="btn" @click="purgeCache">清理过期缓存</button></div>
      </div>
      <div v-if="canManageQuota && authStore.state.enabled" class="panel">
        <h3>待处理邀请</h3>
        <table v-if="invitations.length" class="table">
        <thead><tr><th>邀请邮箱</th><th>角色</th><th>状态</th><th>有效期</th><th></th></tr></thead>
        <tbody>
          <tr v-for="invitation in invitations" :key="invitation.id">
            <td>{{ invitation.email }}</td><td>{{ invitation.role }}</td><td>{{ invitation.status }}</td><td>{{ invitation.expires_at }}</td>
            <td><div v-if="invitation.status === 'pending' || invitation.status === 'expired'" class="toolbar" style="margin:0"><button class="btn" @click="resendInvitation(invitation)">重发</button><button v-if="invitation.status === 'pending'" class="btn btn-danger" @click="revokeInvitation(invitation)">撤销</button></div></td>
          </tr>
        </tbody>
        </table>
        <div v-else class="empty">没有待处理邀请</div>
      </div>
      <div v-if="canManageQuota && authStore.state.enabled" class="panel"><h3>组织成员</h3><table class="table">
        <thead><tr><th>成员</th><th>邮箱</th><th>角色</th><th></th></tr></thead>
        <tbody>
          <tr v-for="member in members" :key="member.user_id">
            <td>{{ member.display_name }}</td><td>{{ member.email }}</td>
            <td><select v-if="member.role !== 'owner'" v-model="member.role" @change="changeMemberRole(member)"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select><span v-else>所有者</span></td>
            <td><div v-if="member.role !== 'owner' && member.user_id !== authStore.state.actor?.user.id" class="toolbar" style="margin:0"><button class="btn btn-danger" @click="removeMember(member)">移除</button></div></td>
          </tr>
        </tbody>
      </table></div>
    </section>

    <section v-if="activeSection === 'security'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>安全与数据</h2><p class="muted">账户凭据、组织数据导出与删除</p></div></div>
      <div class="settings-command-list">
        <div v-if="authStore.state.enabled" class="settings-command"><div><strong>登录密码</strong><p class="muted">更新当前账户的登录密码</p></div><button class="btn" @click="showPasswordModal = true">修改密码</button></div>
        <div v-if="authStore.state.actor?.role === 'owner'" class="settings-command"><div><strong>组织数据</strong><p class="muted">下载当前组织的完整 JSON 数据副本</p></div><button class="btn" @click="exportOrganization">导出组织数据</button></div>
        <div v-if="authStore.state.actor?.role === 'owner'" class="settings-command danger"><div><strong>永久删除组织</strong><p class="muted">删除组织、项目、任务和已缓存媒体，此操作无法撤销</p></div><button class="btn btn-danger" @click="showDeleteModal = true">永久删除组织</button></div>
      </div>
    </section>

    <div v-if="showServiceModal" class="modal-mask" @click.self="showServiceModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="service-modal-title" @keydown.esc="showServiceModal = false" @submit.prevent="saveService"><h3 id="service-modal-title">{{ editingConfigID ? '编辑 AI 服务' : '添加 AI 服务' }}</h3><div class="field"><label for="service-type">类型</label><select id="service-type" v-model="form.service_type" autofocus><option value="text">文本</option><option value="image">图片</option><option value="video">视频</option><option value="audio">音频/TTS</option></select></div><div class="field"><label for="service-provider">厂商</label><select id="service-provider" v-model="form.provider"><option v-for="provider in availableProviders" :key="provider.value" :value="provider.value">{{ provider.label }}</option></select></div><div class="field"><label for="service-name">名称</label><input id="service-name" v-model="form.name" placeholder="我的 GPT" /></div><div class="field"><label for="service-url">Base URL</label><input id="service-url" v-model="form.base_url" /></div><div class="field"><label for="service-key">API Key</label><input id="service-key" v-model="form.api_key" type="password" :placeholder="editingConfigID ? '留空保持原密钥' : ''" /></div><div class="field"><label for="service-model">模型</label><input id="service-model" v-model="form.model" placeholder="gpt-4o-mini" /></div><div class="service-toggle-grid"><label class="settings-check"><input v-model="form.is_active" type="checkbox" /> 启用服务</label><label class="settings-check" :class="{ disabled: !form.is_active }"><input v-model="form.is_default" type="checkbox" :disabled="!form.is_active" /> 设为默认</label></div><div class="service-test-row"><button type="button" class="btn" :disabled="savingService || testingDraftService" @click="testDraftService"><FlaskConical :size="14" aria-hidden="true" />{{ testingDraftService ? '测试中…' : '测试当前配置' }}</button><span v-if="serviceTestResult" role="status">{{ serviceTestResult }}</span></div><p v-if="serviceError" class="form-error" role="alert">{{ serviceError }}</p><div class="modal-actions"><button type="button" class="btn" :disabled="savingService || testingDraftService" @click="showServiceModal = false">取消</button><button type="submit" class="btn btn-primary" :disabled="savingService || testingDraftService">{{ savingService ? '保存中…' : editingConfigID ? '保存修改' : '保存配置' }}</button></div></form></div>

    <div v-if="agentForm" class="modal-mask" @click.self="agentForm = null"><form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="agent-modal-title" @submit.prevent="saveAgent"><h3 id="agent-modal-title">编辑 Agent</h3><div class="field-grid"><div class="field"><label for="agent-name">名称</label><input id="agent-name" v-model="agentForm.name" /></div><div class="field"><label for="agent-model">模型</label><input id="agent-model" v-model="agentForm.model" placeholder="继承文本默认" /></div><div class="field"><label for="agent-temperature">温度</label><input id="agent-temperature" v-model.number="agentForm.temperature" type="number" min="0" max="2" step="0.1" /></div><div class="field"><label for="agent-tokens">最大输出 token</label><input id="agent-tokens" v-model.number="agentForm.max_tokens" type="number" min="1" max="128000" /></div><div class="field"><label for="agent-iterations">最大模型迭代</label><input id="agent-iterations" v-model.number="agentForm.max_iterations" type="number" min="1" max="2" /></div><label class="settings-check"><input v-model="agentForm.is_active" type="checkbox" /> 启用</label></div><div class="modal-actions"><button type="button" class="btn" @click="agentForm = null">取消</button><button type="submit" class="btn btn-primary">保存 Agent</button></div></form></div>

    <div v-if="promptForm" class="modal-mask" @click.self="promptForm = null"><form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" :aria-labelledby="promptForm.id ? 'edit-prompt-title' : 'create-prompt-title'" @keydown.esc="promptForm = null" @submit.prevent="savePrompt"><h3 :id="promptForm.id ? 'edit-prompt-title' : 'create-prompt-title'">{{ promptForm.id ? '编辑提示词' : '新建提示词' }}</h3><div class="field-grid"><div class="field"><label for="prompt-name">名称 *</label><input id="prompt-name" v-model="promptForm.name" autofocus maxlength="200" /></div><div class="field"><label for="prompt-key">标识 *</label><input id="prompt-key" v-model="promptForm.key" :disabled="!!promptForm.id" pattern="[a-z][a-z0-9_]{1,63}" /></div><div class="field"><label for="prompt-category">分类 *</label><select id="prompt-category" v-model="promptForm.category"><option v-for="(label, value) in promptCategoryLabels" :key="value" :value="value">{{ label }}</option></select></div><label class="settings-check"><input v-model="promptForm.is_active" type="checkbox" /> 启用</label><div class="field settings-span"><label for="prompt-description">说明</label><input id="prompt-description" v-model="promptForm.description" maxlength="2000" /></div><div class="field settings-span"><label for="prompt-content">模板内容 *</label><textarea id="prompt-content" ref="promptContentInput" v-model="promptForm.content" rows="10" maxlength="20000" /></div><div class="prompt-token-picker settings-span"><span>插入变量</span><div><button v-for="(label, variable) in promptVariableLabels" :key="variable" type="button" :aria-label="`插入变量 ${label}`" :title="promptToken(variable)" @click="insertPromptVariable(variable)"><strong>{{ label }}</strong><code>{{ promptToken(variable) }}</code></button></div></div></div><p v-if="promptFormError" class="form-error" role="alert">{{ promptFormError }}</p><div v-if="promptDraftPreview" class="prompt-draft-preview" role="status"><strong>草稿预览</strong><div class="prompt-preview-result">{{ promptDraftPreview }}</div></div><div class="modal-actions"><button type="button" class="btn" :disabled="savingPrompt || previewingPromptDraft" @click="promptForm = null">取消</button><button type="button" class="btn" :disabled="savingPrompt || previewingPromptDraft || !promptForm.content.trim()" @click="previewPromptForm">{{ previewingPromptDraft ? '检查中…' : '检查并预览' }}</button><button type="submit" class="btn btn-primary" :disabled="savingPrompt || previewingPromptDraft">{{ savingPrompt ? '保存中…' : promptForm.id ? '保存修改' : '创建模板' }}</button></div></form></div>

    <div v-if="previewTemplate" class="modal-mask" @click.self="previewTemplate = null"><div class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="preview-prompt-title" @keydown.esc="previewTemplate = null"><h3 id="preview-prompt-title">预览提示词</h3><p class="prompt-preview-name">{{ previewTemplate.name }} · v{{ previewTemplate.version }}</p><div v-if="promptVariables(previewTemplate).length" class="field-grid"><div v-for="variable in promptVariables(previewTemplate)" :key="variable" class="field"><label :for="`preview-${variable}`">{{ promptVariableLabels[variable] || variable }}</label><input :id="`preview-${variable}`" v-model="previewVariables[variable]" /></div></div><p v-if="previewError" class="form-error" role="alert">{{ previewError }}</p><div v-if="previewResult" class="prompt-preview-result" aria-live="polite">{{ previewResult }}</div><div class="modal-actions"><button type="button" class="btn" @click="previewTemplate = null">关闭</button><button type="button" class="btn btn-primary" @click="renderPreview">生成预览</button></div></div></div>

    <div v-if="revisionTemplate" class="modal-mask" @click.self="closePromptHistory">
      <div class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="prompt-history-title" @keydown.esc="closePromptHistory">
        <h3 id="prompt-history-title">提示词版本历史</h3>
        <p class="prompt-preview-name">{{ revisionTemplate.name }} · 当前 v{{ revisionTemplate.version }}</p>
        <div class="prompt-revision-list">
          <div v-if="promptHistoryLoading" class="empty" role="status">正在加载版本历史…</div>
          <div v-else-if="promptHistoryError" class="inline-alert" role="alert"><div><strong>版本历史加载失败</strong><span>{{ promptHistoryError }}</span></div><button class="btn" type="button" @click="openPromptHistory(revisionTemplate)">重试加载</button></div>
          <article v-for="revision in promptRevisions" :key="revision.id" class="prompt-revision">
            <div class="prompt-revision-head">
              <div><strong>v{{ revision.version }}</strong><span v-if="revision.version === revisionTemplate.version" class="service-status"><span class="default">当前</span></span><span class="muted">{{ formatRevisionTime(revision.created_at) }}</span></div>
              <button v-if="canManageSettings && revision.version !== revisionTemplate.version" type="button" class="btn" :disabled="restoringRevision !== null" :aria-label="`恢复 v${revision.version}`" @click="restorePromptRevision(revision)">{{ restoringRevision === revision.version ? '恢复中…' : '恢复此版本' }}</button>
            </div>
            <pre>{{ revision.content }}</pre>
          </article>
          <div v-if="!promptHistoryLoading && !promptHistoryError && !promptRevisions.length" class="empty">暂无版本记录</div>
        </div>
        <div class="modal-actions"><button type="button" class="btn" @click="closePromptHistory">关闭</button></div>
      </div>
    </div>

    <div v-if="showMemberModal" class="modal-mask" @click.self="showMemberModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="member-modal-title" @submit.prevent="addMember"><h3 id="member-modal-title">添加成员</h3><div class="field"><label for="member-email">邮箱</label><input id="member-email" v-model="memberForm.email" type="email" /></div><div class="field"><label for="member-name">显示名称</label><input id="member-name" v-model="memberForm.display_name" /></div><div class="field"><label for="member-password">初始密码</label><input id="member-password" v-model="memberForm.password" type="password" placeholder="12-128 个字符" /></div><div class="field"><label for="member-role">角色</label><select id="member-role" v-model="memberForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div><div class="modal-actions"><button type="button" class="btn" @click="showMemberModal = false">取消</button><button type="submit" class="btn btn-primary">添加成员</button></div></form></div>

    <div v-if="showInviteModal" class="modal-mask" @click.self="showInviteModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="invite-modal-title" @submit.prevent="inviteMember"><h3 id="invite-modal-title">创建邀请</h3><template v-if="!inviteLink"><div class="field"><label for="invite-email">邀请邮箱</label><input id="invite-email" v-model="inviteForm.email" type="email" /></div><div class="field"><label for="invite-role">邀请角色</label><select id="invite-role" v-model="inviteForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div><div class="field"><label for="invite-hours">有效小时</label><input id="invite-hours" v-model.number="inviteForm.ttl_hours" type="number" min="1" max="168" /></div></template><div v-else class="field"><label for="invite-link">邀请链接</label><input id="invite-link" :value="inviteLink" readonly /></div><div class="modal-actions"><button type="button" class="btn" @click="showInviteModal = false">{{ inviteLink ? '完成' : '取消' }}</button><button v-if="!inviteLink" type="submit" class="btn btn-primary">创建安全邀请</button></div></form></div>

    <div v-if="showPasswordModal" class="modal-mask" @click.self="showPasswordModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="password-modal-title" @submit.prevent="changePassword"><h3 id="password-modal-title">修改密码</h3><div class="field"><label for="current-password">当前密码</label><input id="current-password" v-model="passwordForm.current" type="password" /></div><div class="field"><label for="new-password">新密码</label><input id="new-password" v-model="passwordForm.next" type="password" /></div><div class="field"><label for="confirm-password">确认新密码</label><input id="confirm-password" v-model="passwordForm.confirm" type="password" /></div><div class="modal-actions"><button type="button" class="btn" @click="showPasswordModal = false">取消</button><button type="submit" class="btn btn-primary">更新密码</button></div></form></div>

    <div v-if="showDeleteModal" class="modal-mask" @click.self="showDeleteModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="delete-modal-title" @submit.prevent="deleteOrganization"><h3 id="delete-modal-title">永久删除组织</h3><p class="muted">此操作会删除组织及其全部数据，无法撤销。</p><div class="field"><label for="delete-password">当前密码</label><input id="delete-password" v-model="deleteForm.password" type="password" /></div><div class="field"><label for="delete-confirmation">输入组织标识 {{ authStore.state.actor?.organization.slug }}</label><input id="delete-confirmation" v-model="deleteForm.confirmation" /></div><div class="modal-actions"><button type="button" class="btn" @click="showDeleteModal = false">取消</button><button type="submit" class="btn btn-danger">确认永久删除</button></div></form></div>

    <div v-if="toast" class="toast">{{ toast }}</div>
  </div>
</template>
