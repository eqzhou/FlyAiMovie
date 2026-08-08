import { nextTick } from 'vue'
import { settingsAPI } from '../../api'
import { confirmAction } from '../../composables/useConfirm'
import { errorMessage } from '../../utils/errorMessage'

export function createSettingsPromptActions(deps: Record<string, any>) {
  const { promptForm, promptContentInput, promptDraftRequestID, previewingPromptDraft, promptFormError, promptDraftPreview, savingPrompt, promptTemplates, previewRequestID, previewTemplate, previewingTemplate, previewVariables, promptExampleValues, previewResult, previewError, revisionTemplate, promptRevisions, promptHistoryRequestID, promptHistoryError, promptHistoryLoading, restoringRevision, promptActionNotice, load, show, agentForm, builtInPromptKeys } = deps

function promptVariables(template: any): string[] {
  try { return JSON.parse(template.variables_json || '[]') } catch { return [] }
}
function promptToken(variable: string) { return `{{${variable}}}` }
function isBuiltInPrompt(template: any) { return builtInPromptKeys.has(template.key) }

function resetPromptDraftFeedback() {
  promptDraftRequestID.value += 1
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
  const requestID = ++promptDraftRequestID.value
  previewingPromptDraft.value = true
  promptFormError.value = ''
  promptDraftPreview.value = ''
  const content = String(promptForm.value.content)
  const variables = Object.fromEntries(promptDraftVariables(content).map((name) => [name, promptExampleValues[name] || '示例内容']))
  try {
    const result = await settingsAPI.previewPromptDraft(content, variables)
    if (requestID !== promptDraftRequestID.value || String(promptForm.value?.content || '') !== content) return
    promptDraftPreview.value = result.rendered
  } catch (error) {
    if (requestID !== promptDraftRequestID.value || String(promptForm.value?.content || '') !== content) return
    promptFormError.value = errorMessage(error, '模板检查失败')
  } finally {
    if (requestID === promptDraftRequestID.value) previewingPromptDraft.value = false
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
  } catch (error) {
    promptFormError.value = errorMessage(error, '保存失败')
    savingPrompt.value = false
    return
  }
  const successMessage = promptForm.value.id ? '提示词已更新' : '提示词已创建'
  promptForm.value = null
  show(successMessage)
  try {
    promptTemplates.value = await settingsAPI.promptTemplates()
  } catch {
    show(`${successMessage}，但模板列表暂未刷新`)
  } finally {
    savingPrompt.value = false
  }
}

function openPreview(template: any) {
  previewRequestID.value += 1
  previewTemplate.value = template
  previewVariables.value = Object.fromEntries(promptVariables(template).map((name) => [name, promptExampleValues[name] || '示例内容']))
  previewResult.value = ''
  previewError.value = ''
}

function closePreview() {
  previewRequestID.value += 1
  previewingTemplate.value = false
  previewTemplate.value = null
}

async function renderPreview() {
  if (!previewTemplate.value || previewingTemplate.value) return
  const requestID = ++previewRequestID.value
  const templateID = Number(previewTemplate.value.id)
  const variables = { ...previewVariables.value }
  previewingTemplate.value = true
  previewError.value = ''
  try {
    const result = await settingsAPI.previewPromptTemplate(templateID, variables)
    if (requestID !== previewRequestID.value || Number(previewTemplate.value?.id) !== templateID) return
    previewResult.value = result.rendered
  } catch (error) {
    if (requestID !== previewRequestID.value || Number(previewTemplate.value?.id) !== templateID) return
    previewResult.value = ''
    previewError.value = errorMessage(error, '预览失败')
  } finally {
    if (requestID === previewRequestID.value) previewingTemplate.value = false
  }
}

async function restorePrompt(template: any) {
  if (!await confirmAction({
    title: '恢复内置模板',
    message: `确定将「${template.name}」恢复为内置模板内容？`,
    detail: '当前的自定义修改会被内置版本覆盖。',
    confirmText: '恢复模板',
  })) return
  try {
    await settingsAPI.restorePromptTemplate(template.id)
    promptTemplates.value = await settingsAPI.promptTemplates()
    show('已恢复内置模板')
  } catch (error) {
    show(errorMessage(error, '恢复模板失败'))
  }
}

async function openPromptHistory(template: any) {
  const requestID = ++promptHistoryRequestID.value
  const templateID = Number(template.id)
  revisionTemplate.value = { ...template }
  promptRevisions.value = []
  promptHistoryError.value = ''
  promptHistoryLoading.value = true
  try {
    const revisions = await settingsAPI.promptTemplateRevisions(templateID)
    if (requestID !== promptHistoryRequestID.value || Number(revisionTemplate.value?.id) !== templateID) return
    promptRevisions.value = revisions
  } catch (error) {
    if (requestID !== promptHistoryRequestID.value || Number(revisionTemplate.value?.id) !== templateID) return
    promptHistoryError.value = errorMessage(error, '版本历史加载失败')
  } finally {
    if (requestID === promptHistoryRequestID.value && Number(revisionTemplate.value?.id) === templateID) promptHistoryLoading.value = false
  }
}

function closePromptHistory() {
  promptHistoryRequestID.value += 1
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
    promptHistoryError.value = errorMessage(error, '版本恢复失败')
  } finally {
    restoringRevision.value = null
  }
}

function formatRevisionTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

async function removePrompt(template: any) {
  if (!await confirmAction({
    title: '删除提示词',
    message: `确定删除提示词模板「${template.name}」？`,
    detail: '该模板的历史版本将一并删除。',
    confirmText: '删除模板',
    tone: 'danger',
  })) return
  try {
    await settingsAPI.deletePromptTemplate(template.id)
    promptTemplates.value = await settingsAPI.promptTemplates()
    show('提示词已删除')
  } catch (error) {
    show(errorMessage(error, '删除模板失败'))
  }
}

async function saveAgent() {
  if (!agentForm.value) return
  try {
    await settingsAPI.upsertAgentConfig(agentForm.value)
    show('Agent 配置已保存')
    agentForm.value = null
    await load()
  } catch (error) {
    show(errorMessage(error, '保存 Agent 配置失败'))
  }
}

  return { promptVariables, promptToken, isBuiltInPrompt, resetPromptDraftFeedback, openCreatePrompt, duplicatePrompt, editPrompt, promptDraftVariables, previewPromptForm, insertPromptVariable, savePrompt, openPreview, closePreview, renderPreview, restorePrompt, openPromptHistory, closePromptHistory, restorePromptRevision, formatRevisionTime, removePrompt, saveAgent }
}
