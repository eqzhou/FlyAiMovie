import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  cacheAPI, memberAPI, quotaAPI, settingsAPI,
} from '../../api'
import { authStore } from '../../auth'
import { errorMessage } from '../../utils/errorMessage'
import { createSettingsOrganizationActions } from './useSettingsOrganizationActions'
import { createSettingsPromptActions } from './useSettingsPromptActions'
import { createSettingsServiceActions } from './useSettingsServiceActions'

export function useSettings() {
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
let toastTimer: number | null = null
const loading = ref(true)
// 首次加载完成后置为 true：后续刷新只在原位更新，不再整页回到骨架态。
const loaded = ref(false)
const loadError = ref('')
const serviceError = ref('')
const savingService = ref(false)
const testingDraftService = ref(false)
const serviceTestResult = ref('')
const agentForm = ref<any | null>(null)
const activeSection = ref<'services' | 'bundles' | 'agents' | 'skills' | 'prompts' | 'voices' | 'organization' | 'security'>('services')
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
const previewingTemplate = ref(false)
const previewRequestID = ref(0)
const revisionTemplate = ref<any | null>(null)
const promptRevisions = ref<any[]>([])
const restoringRevision = ref<number | null>(null)
const promptHistoryLoading = ref(false)
const promptHistoryError = ref('')
const promptActionNotice = ref('')
const promptHistoryRequestID = ref(0)
const promptDraftRequestID = ref(0)
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
const canManageSkills = computed(() => !authStore.state.enabled || ['owner', 'admin'].includes(authStore.state.actor?.role || ''))
const canPreviewVoices = computed(() => !authStore.state.enabled || authStore.state.actor?.role !== 'viewer')
const members = ref<any[]>([])
const memberForm = ref({ email: '', display_name: '', password: '', role: 'editor' })
const inviteForm = ref({ email: '', role: 'editor', ttl_hours: 72 })
const inviteLink = ref('')
const inviteEmailSent = ref<boolean | null>(null)
const inviting = ref(false)
const copyingInvite = ref(false)
const savingMember = ref(false)
const changingPassword = ref(false)
const invitations = ref<any[]>([])
const passwordForm = ref({ current: '', next: '', confirm: '' })
const deleteForm = ref({ password: '', confirmation: '' })
const isPlatformAdmin = computed(() => !!authStore.state.actor?.user?.is_platform_admin)
const platformSettings = ref({ registration_enabled: true, require_email_verification: false })
const platformSettingsLoaded = ref(false)
const platformSettingsLoading = ref(false)
const platformSettingsError = ref('')
const savingPlatformSettings = ref(false)
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

function show(m: string, duration = 2800) {
  toast.value = m
  if (toastTimer) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => {
    if (toast.value === m) toast.value = ''
    toastTimer = null
  }, duration)
}

function humanizeError(error: unknown, fallback = '操作失败') {
  const raw = errorMessage(error, String(error || ''))
  const message = raw.trim() || fallback
  if (/连接失败|厂商|凭据|模型|超时|证书|域名/.test(message)) return message
  if (/provider connection failed/i.test(message)) return message.replace(/^provider connection failed:\s*/i, '连接失败：')
  if (/invalid body/i.test(message)) return '请求参数无效'
  if (/not found/i.test(message)) return '资源不存在或无权访问'
  if (/too long/i.test(message)) return '字段过长，请缩短后再试'
  if (/api_key is required/i.test(message)) return '更换厂商、类型或 Base URL 后需要重新填写 API Key'
  return message
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
    else failures.push(errorMessage(result.reason, `${requests[index].label}加载失败`))
  })
  loadError.value = failures.join('；')
  loading.value = false
  loaded.value = true
}

  const organizationActions = createSettingsOrganizationActions({
    show, savingMember, memberForm, showMemberModal, inviting, inviteForm, inviteLink,
    inviteEmailSent, showInviteModal, copyingInvite, members, invitations, load,
    isPlatformAdmin, platformSettingsLoading, platformSettingsError, platformSettings,
    platformSettingsLoaded, savingPlatformSettings, changingPassword, passwordForm,
    showPasswordModal, deleteForm, quota, cache, router,
  })
  const serviceActions = createSettingsServiceActions({
    form, serviceError, serviceTestResult, editingConfigID, originalServiceIdentity,
    hydratingServiceForm, savingService, testingDraftService, testingConfigID,
    availableProviders, showServiceModal, load, show, humanizeError, syncingVoices,
    voiceCatalog, voicePreviewText, previewingVoiceID, voicePreviewURLs, agentForm,
    promptForm, promptDraftRequestID, promptFormError, promptDraftPreview,
    previewingPromptDraft,
  })
  const promptActions = createSettingsPromptActions({
    promptForm, promptContentInput, promptDraftRequestID, previewingPromptDraft,
    promptFormError, promptDraftPreview, savingPrompt, promptTemplates,
    previewRequestID, previewTemplate, previewingTemplate, previewVariables,
    promptExampleValues, previewResult, previewError, revisionTemplate, promptRevisions,
    promptHistoryRequestID, promptHistoryError, promptHistoryLoading, restoringRevision,
    promptActionNotice, load, show, agentForm, builtInPromptKeys,
  })
  const actions = { ...organizationActions, ...serviceActions, ...promptActions }
  const { loadPlatformSettings } = organizationActions

watch(activeSection, (section) => {
  if (section === 'security' && isPlatformAdmin.value) {
    void loadPlatformSettings()
  }
})

watch(isPlatformAdmin, (admin) => {
  if (admin && activeSection.value === 'security') {
    void loadPlatformSettings()
  }
})

onMounted(load)
onUnmounted(() => {
  if (toastTimer) window.clearTimeout(toastTimer)
  promptHistoryRequestID.value += 1
  promptDraftRequestID.value += 1
  previewRequestID.value += 1
})

  return { configs, router, providers, agents, promptTemplates, voiceCatalog, form, toast, toastTimer, loading, loaded, loadError, serviceError, savingService, testingDraftService, serviceTestResult, agentForm, activeSection, showServiceModal, editingConfigID, originalServiceIdentity, hydratingServiceForm, testingConfigID, showMemberModal, showInviteModal, showPasswordModal, showDeleteModal, promptForm, promptContentInput, promptQuery, promptCategory, promptState, promptFormError, promptDraftPreview, previewingPromptDraft, savingPrompt, previewTemplate, previewingTemplate, previewRequestID, revisionTemplate, promptRevisions, restoringRevision, promptHistoryLoading, promptHistoryError, promptActionNotice, promptHistoryRequestID, promptDraftRequestID, voiceQuery, voicePreviewText, previewingVoiceID, syncingVoices, voicePreviewURLs, previewVariables, previewResult, previewError, builtInPromptKeys, promptCategoryLabels, promptVariableLabels, promptExampleValues, providerChoices, quota, cache, canManageQuota, canManageSettings, canManageSkills, canPreviewVoices, members, memberForm, inviteForm, inviteLink, inviteEmailSent, inviting, copyingInvite, savingMember, changingPassword, invitations, passwordForm, deleteForm, isPlatformAdmin, platformSettings, platformSettingsLoaded, platformSettingsLoading, platformSettingsError, savingPlatformSettings, availableProviders, filteredVoices, filteredPromptTemplates, serviceTypeLabels, agentTypeLabels, serviceTypeLabel, providerLabel, agentTypeLabel, show, humanizeError, load, ...actions }
}
