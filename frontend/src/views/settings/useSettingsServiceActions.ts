import { nextTick, watch } from 'vue'
import { settingsAPI } from '../../api'
import { confirmAction } from '../../composables/useConfirm'
import { errorMessage } from '../../utils/errorMessage'

export function createSettingsServiceActions(deps: Record<string, any>) {
  const { form, serviceError, serviceTestResult, editingConfigID, originalServiceIdentity, hydratingServiceForm, savingService, testingDraftService, testingConfigID, availableProviders, showServiceModal, load, show, humanizeError, syncingVoices, voiceCatalog, voicePreviewText, previewingVoiceID, voicePreviewURLs, agentForm, promptForm, promptDraftRequestID, promptFormError, promptDraftPreview, previewingPromptDraft } = deps

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
    serviceError.value = humanizeError(error, 'AI 服务保存失败')
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
    show(humanizeError(error, '连接失败'), 4200)
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
    serviceError.value = humanizeError(error, '连接失败')
  } finally {
    testingDraftService.value = false
  }
}

watch(() => form.value.service_type, () => {
  const first = availableProviders.value[0]
  if (!availableProviders.value.some((item: any) => item.value === form.value.provider) && first) {
    form.value.provider = first.value
    form.value.base_url = first.base
  }
})

watch(() => form.value.provider, (provider) => {
  if (hydratingServiceForm.value) return
  const selected = availableProviders.value.find((item: any) => item.value === provider)
  if (selected) form.value.base_url = selected.base
})

watch(() => form.value.is_active, (active) => {
  if (!active) form.value = { ...form.value, is_default: false }
})

watch(form, () => {
  if (!hydratingServiceForm.value && !testingDraftService.value) serviceTestResult.value = ''
}, { deep: true })

watch(() => promptForm.value?.content, () => {
  promptDraftRequestID.value += 1
  promptFormError.value = ''
  promptDraftPreview.value = ''
  previewingPromptDraft.value = false
})

async function remove(id: number) {
  if (!await confirmAction({
    title: '删除 AI 服务',
    message: '确定删除该 AI 服务配置？',
    detail: '正在使用该配置的功能需要重新选择服务。',
    confirmText: '删除配置',
    tone: 'danger',
  })) return
  try {
    await settingsAPI.deleteAIConfig(id)
    await load()
    show('AI 服务已删除')
  } catch (error) {
    show(errorMessage(error, '删除 AI 服务失败'))
  }
}

async function syncVoices() {
  syncingVoices.value = true
  try {
    const res = await settingsAPI.syncVoices()
    voiceCatalog.value = await settingsAPI.voices(true)
    show(`已同步 ${res.count ?? voiceCatalog.value.length} 个音色`)
  } catch (error) {
    show(`同步失败：${errorMessage(error, '未知错误')}`)
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
    show(`试听失败：${errorMessage(error, '未知错误')}`)
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

  return { emptyServiceForm, emptyServiceIdentity, serviceValidationMessage, openCreateService, editService, saveService, testService, testDraftService, remove, syncVoices, previewVoice, editAgent }
}
