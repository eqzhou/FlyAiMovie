<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { cacheAPI, memberAPI, organizationDataAPI, quotaAPI, settingsAPI } from '../api'
import { authStore } from '../auth'

const configs = ref<any[]>([])
const router = useRouter()
const providers = ref<any[]>([])
const agents = ref<any[]>([])
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
const agentForm = ref<any | null>(null)
const activeSection = ref<'services' | 'agents' | 'organization' | 'security'>('services')
const showServiceModal = ref(false)
const editingConfigID = ref<number | null>(null)
const originalProvider = ref('')
const hydratingServiceForm = ref(false)
const testingConfigID = ref<number | null>(null)
const showMemberModal = ref(false)
const showInviteModal = ref(false)
const showPasswordModal = ref(false)
const showDeleteModal = ref(false)
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
const members = ref<any[]>([])
const memberForm = ref({ email: '', display_name: '', password: '', role: 'editor' })
const inviteForm = ref({ email: '', role: 'editor', ttl_hours: 72 })
const inviteLink = ref('')
const invitations = ref<any[]>([])
const passwordForm = ref({ current: '', next: '', confirm: '' })
const deleteForm = ref({ password: '', confirmation: '' })
const availableProviders = computed(() => providerChoices[form.value.service_type] || [])

function show(m: string) {
  toast.value = m
  setTimeout(() => (toast.value = ''), 2000)
}

async function load() {
  configs.value = await settingsAPI.aiConfigs()
  providers.value = await settingsAPI.providers()
  agents.value = await settingsAPI.agentConfigs()
  quota.value = await quotaAPI.get()
  cache.value = await cacheAPI.stats()
  if (canManageQuota.value && authStore.state.enabled) {
    members.value = await memberAPI.list()
    invitations.value = await memberAPI.invitations()
  }
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

function openCreateService() {
  editingConfigID.value = null
  originalProvider.value = ''
  form.value = emptyServiceForm()
  showServiceModal.value = true
}

async function editService(config: any) {
  hydratingServiceForm.value = true
  editingConfigID.value = config.id
  originalProvider.value = config.provider
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
  const keyOptional = ['mock', 'openai_local'].includes(form.value.provider)
  const providerChanged = Boolean(editingConfigID.value && originalProvider.value !== form.value.provider)
  if (!form.value.name || ((!editingConfigID.value || providerChanged) && !keyOptional && !form.value.api_key)) {
    show(keyOptional ? '名称必填' : '名称和 API Key 必填')
    return
  }
  if (editingConfigID.value) {
    await settingsAPI.updateAIConfig(editingConfigID.value, form.value)
  } else {
    await settingsAPI.createAIConfig(form.value)
  }
  form.value.api_key = ''
  showServiceModal.value = false
  show(editingConfigID.value ? 'AI 服务已更新' : 'AI 服务已添加')
  editingConfigID.value = null
  originalProvider.value = ''
  await load()
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

async function remove(id: number) {
  if (!confirm('删除该配置？')) return
  await settingsAPI.deleteAIConfig(id)
  await load()
}

async function syncVoices() {
  const res = await settingsAPI.syncVoices()
  show(res.message || 'synced')
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
      <button role="tab" :aria-selected="activeSection === 'organization'" :class="{ active: activeSection === 'organization' }" @click="activeSection = 'organization'">组织与权限</button>
      <button role="tab" :aria-selected="activeSection === 'security'" :class="{ active: activeSection === 'security' }" @click="activeSection = 'security'">安全与数据</button>
    </div>

    <section v-if="activeSection === 'services'" class="settings-section" role="tabpanel">
      <div class="settings-section-head">
        <div><h2>AI 服务</h2><p class="muted">{{ configs.length }} 个已配置服务 · {{ providers.length }} 个内置厂商模板</p></div>
        <div v-if="canManageSettings" class="toolbar"><button class="btn" @click="syncVoices">同步音色</button><button class="btn btn-primary" @click="openCreateService">添加 AI 服务</button></div>
      </div>
      <div class="panel">
        <table class="table">
          <thead><tr><th>名称</th><th>类型</th><th>厂商</th><th>模型</th><th></th></tr></thead>
          <tbody>
            <tr v-for="c in configs" :key="c.id">
              <td>{{ c.name }}</td>
              <td>{{ c.service_type }}</td>
              <td>{{ c.provider }}</td>
              <td>{{ c.model }}</td>
              <td><div v-if="canManageSettings" class="toolbar settings-table-actions"><button class="btn" @click="editService(c)">编辑</button><button class="btn" :disabled="testingConfigID !== null" @click="testService(c)">{{ testingConfigID === c.id ? '测试中…' : '测试连接' }}</button><button class="btn btn-danger" @click="remove(c.id)">删</button></div></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!configs.length" class="empty">尚未配置 AI 服务</div>
      </div>
    </section>

    <section v-if="activeSection === 'agents'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>Agent 预设</h2><p class="muted">为制作流程配置模型和执行边界</p></div></div>
      <div class="panel">
        <table class="table">
          <thead><tr><th>类型</th><th>名称</th><th>模型</th><th>状态</th><th></th></tr></thead>
          <tbody><tr v-for="a in agents" :key="a.id"><td>{{ a.agent_type }}</td><td>{{ a.name }}</td><td>{{ a.model || '继承文本默认' }}</td><td>{{ a.is_active ? '启用' : '停用' }}</td><td><button v-if="canManageSettings" class="btn" @click="editAgent(a)">编辑</button></td></tr></tbody>
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

    <div v-if="showServiceModal" class="modal-mask" @click.self="showServiceModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="service-modal-title" @keydown.esc="showServiceModal = false" @submit.prevent="saveService"><h3 id="service-modal-title">{{ editingConfigID ? '编辑 AI 服务' : '添加 AI 服务' }}</h3><div class="field"><label for="service-type">类型</label><select id="service-type" v-model="form.service_type" autofocus><option value="text">文本</option><option value="image">图片</option><option value="video">视频</option><option value="audio">音频/TTS</option></select></div><div class="field"><label for="service-provider">厂商</label><select id="service-provider" v-model="form.provider"><option v-for="provider in availableProviders" :key="provider.value" :value="provider.value">{{ provider.label }}</option></select></div><div class="field"><label for="service-name">名称</label><input id="service-name" v-model="form.name" placeholder="我的 GPT" /></div><div class="field"><label for="service-url">Base URL</label><input id="service-url" v-model="form.base_url" /></div><div class="field"><label for="service-key">API Key</label><input id="service-key" v-model="form.api_key" type="password" :placeholder="editingConfigID ? '留空保持原密钥' : ''" /></div><div class="field"><label for="service-model">模型</label><input id="service-model" v-model="form.model" placeholder="gpt-4o-mini" /></div><div class="modal-actions"><button type="button" class="btn" @click="showServiceModal = false">取消</button><button type="submit" class="btn btn-primary">{{ editingConfigID ? '保存修改' : '保存配置' }}</button></div></form></div>

    <div v-if="agentForm" class="modal-mask" @click.self="agentForm = null"><form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="agent-modal-title" @submit.prevent="saveAgent"><h3 id="agent-modal-title">编辑 Agent</h3><div class="field-grid"><div class="field"><label for="agent-name">名称</label><input id="agent-name" v-model="agentForm.name" /></div><div class="field"><label for="agent-model">模型</label><input id="agent-model" v-model="agentForm.model" placeholder="继承文本默认" /></div><div class="field"><label for="agent-temperature">温度</label><input id="agent-temperature" v-model.number="agentForm.temperature" type="number" min="0" max="2" step="0.1" /></div><div class="field"><label for="agent-tokens">最大输出 token</label><input id="agent-tokens" v-model.number="agentForm.max_tokens" type="number" min="1" max="128000" /></div><div class="field"><label for="agent-iterations">最大模型迭代</label><input id="agent-iterations" v-model.number="agentForm.max_iterations" type="number" min="1" max="2" /></div><label class="settings-check"><input v-model="agentForm.is_active" type="checkbox" /> 启用</label><div class="field settings-span"><label for="agent-prompt">系统提示</label><textarea id="agent-prompt" v-model="agentForm.system_prompt" rows="6" /></div></div><div class="modal-actions"><button type="button" class="btn" @click="agentForm = null">取消</button><button type="submit" class="btn btn-primary">保存 Agent</button></div></form></div>

    <div v-if="showMemberModal" class="modal-mask" @click.self="showMemberModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="member-modal-title" @submit.prevent="addMember"><h3 id="member-modal-title">添加成员</h3><div class="field"><label for="member-email">邮箱</label><input id="member-email" v-model="memberForm.email" type="email" /></div><div class="field"><label for="member-name">显示名称</label><input id="member-name" v-model="memberForm.display_name" /></div><div class="field"><label for="member-password">初始密码</label><input id="member-password" v-model="memberForm.password" type="password" placeholder="12-128 个字符" /></div><div class="field"><label for="member-role">角色</label><select id="member-role" v-model="memberForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div><div class="modal-actions"><button type="button" class="btn" @click="showMemberModal = false">取消</button><button type="submit" class="btn btn-primary">添加成员</button></div></form></div>

    <div v-if="showInviteModal" class="modal-mask" @click.self="showInviteModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="invite-modal-title" @submit.prevent="inviteMember"><h3 id="invite-modal-title">创建邀请</h3><template v-if="!inviteLink"><div class="field"><label for="invite-email">邀请邮箱</label><input id="invite-email" v-model="inviteForm.email" type="email" /></div><div class="field"><label for="invite-role">邀请角色</label><select id="invite-role" v-model="inviteForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div><div class="field"><label for="invite-hours">有效小时</label><input id="invite-hours" v-model.number="inviteForm.ttl_hours" type="number" min="1" max="168" /></div></template><div v-else class="field"><label for="invite-link">邀请链接</label><input id="invite-link" :value="inviteLink" readonly /></div><div class="modal-actions"><button type="button" class="btn" @click="showInviteModal = false">{{ inviteLink ? '完成' : '取消' }}</button><button v-if="!inviteLink" type="submit" class="btn btn-primary">创建安全邀请</button></div></form></div>

    <div v-if="showPasswordModal" class="modal-mask" @click.self="showPasswordModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="password-modal-title" @submit.prevent="changePassword"><h3 id="password-modal-title">修改密码</h3><div class="field"><label for="current-password">当前密码</label><input id="current-password" v-model="passwordForm.current" type="password" /></div><div class="field"><label for="new-password">新密码</label><input id="new-password" v-model="passwordForm.next" type="password" /></div><div class="field"><label for="confirm-password">确认新密码</label><input id="confirm-password" v-model="passwordForm.confirm" type="password" /></div><div class="modal-actions"><button type="button" class="btn" @click="showPasswordModal = false">取消</button><button type="submit" class="btn btn-primary">更新密码</button></div></form></div>

    <div v-if="showDeleteModal" class="modal-mask" @click.self="showDeleteModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="delete-modal-title" @submit.prevent="deleteOrganization"><h3 id="delete-modal-title">永久删除组织</h3><p class="muted">此操作会删除组织及其全部数据，无法撤销。</p><div class="field"><label for="delete-password">当前密码</label><input id="delete-password" v-model="deleteForm.password" type="password" /></div><div class="field"><label for="delete-confirmation">输入组织标识 {{ authStore.state.actor?.organization.slug }}</label><input id="delete-confirmation" v-model="deleteForm.confirmation" /></div><div class="modal-actions"><button type="button" class="btn" @click="showDeleteModal = false">取消</button><button type="submit" class="btn btn-danger">确认永久删除</button></div></form></div>

    <div v-if="toast" class="toast">{{ toast }}</div>
  </div>
</template>
