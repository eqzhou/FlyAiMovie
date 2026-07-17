<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { memberAPI, organizationDataAPI, quotaAPI, settingsAPI } from '../api'
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
const quota = ref({ daily_job_limit: 200, max_active_jobs: 10, daily_jobs_used: 0, active_jobs: 0 })
const canManageQuota = computed(() => !authStore.state.enabled || ['owner', 'admin'].includes(authStore.state.actor?.role || ''))
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
  if (canManageQuota.value && authStore.state.enabled) {
    members.value = await memberAPI.list()
    invitations.value = await memberAPI.invitations()
  }
}

async function addMember() {
  await memberAPI.create(memberForm.value)
  memberForm.value = { email: '', display_name: '', password: '', role: 'editor' }
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
  await quotaAPI.update({ daily_job_limit: quota.value.daily_job_limit, max_active_jobs: quota.value.max_active_jobs })
  show('生成配额已保存')
  quota.value = await quotaAPI.get()
}

async function create() {
  const keyOptional = ['mock', 'openai_local'].includes(form.value.provider)
  if (!form.value.name || (!keyOptional && !form.value.api_key)) {
    show(keyOptional ? '名称必填' : '名称和 API Key 必填')
    return
  }
  await settingsAPI.createAIConfig(form.value)
  form.value.api_key = ''
  show('已添加')
  await load()
}

watch(() => form.value.service_type, () => {
  const first = availableProviders.value[0]
  if (!availableProviders.value.some((item) => item.value === form.value.provider) && first) {
    form.value.provider = first.value
    form.value.base_url = first.base
  }
})

watch(() => form.value.provider, (provider) => {
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
        <p class="page-desc">AI 服务、模型与 Agent 配置</p>
      </div>
      <button class="btn" @click="syncVoices">同步/种子音色</button>
    </div>

    <div class="row">
      <div class="col panel">
        <h3 style="margin-top:0">添加 AI 服务</h3>
        <div class="field"><label>类型</label>
          <select v-model="form.service_type">
            <option value="text">文本</option>
            <option value="image">图片</option>
            <option value="video">视频</option>
            <option value="audio">音频/TTS</option>
          </select>
        </div>
        <div class="field"><label>厂商</label>
          <select v-model="form.provider">
            <option v-for="provider in availableProviders" :key="provider.value" :value="provider.value">{{ provider.label }}</option>
          </select>
        </div>
        <div class="field"><label>名称</label><input v-model="form.name" placeholder="我的 GPT" /></div>
        <div class="field"><label>Base URL</label><input v-model="form.base_url" /></div>
        <div class="field"><label>API Key</label><input v-model="form.api_key" type="password" /></div>
        <div class="field"><label>模型</label><input v-model="form.model" placeholder="gpt-4o-mini" /></div>
        <button class="btn btn-primary" @click="create">保存配置</button>
      </div>

      <div class="col panel">
        <h3 style="margin-top:0">已配置服务</h3>
        <table class="table">
          <thead><tr><th>名称</th><th>类型</th><th>厂商</th><th>模型</th><th></th></tr></thead>
          <tbody>
            <tr v-for="c in configs" :key="c.id">
              <td>{{ c.name }}</td>
              <td>{{ c.service_type }}</td>
              <td>{{ c.provider }}</td>
              <td>{{ c.model }}</td>
              <td><button class="btn btn-danger" @click="remove(c.id)">删</button></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!configs.length" class="empty">尚未配置 AI 服务</div>
      </div>
    </div>

    <div class="panel" style="margin-top:16px">
      <h3 style="margin-top:0">Agent 预设</h3>
      <table class="table">
        <thead><tr><th>类型</th><th>名称</th><th>模型</th><th>状态</th><th></th></tr></thead>
        <tbody>
          <tr v-for="a in agents" :key="a.id">
            <td>{{ a.agent_type }}</td>
            <td>{{ a.name }}</td>
            <td>{{ a.model || '继承文本默认' }}</td>
            <td>{{ a.is_active ? '启用' : '停用' }}</td>
            <td><button class="btn" @click="editAgent(a)">编辑</button></td>
          </tr>
        </tbody>
      </table>
      <div v-if="agentForm" class="field-grid" style="margin-top:16px">
        <div class="field"><label>名称</label><input v-model="agentForm.name" /></div>
        <div class="field"><label>模型</label><input v-model="agentForm.model" placeholder="继承文本默认" /></div>
        <div class="field"><label>温度</label><input v-model.number="agentForm.temperature" type="number" min="0" max="2" step="0.1" /></div>
        <div class="field"><label>最大输出 token</label><input v-model.number="agentForm.max_tokens" type="number" min="1" max="128000" /></div>
        <div class="field"><label>最大模型迭代</label><input v-model.number="agentForm.max_iterations" type="number" min="1" max="2" /></div>
        <label class="row" style="align-items:center"><input v-model="agentForm.is_active" type="checkbox" /> 启用</label>
        <div class="field" style="grid-column:1/-1"><label>系统提示</label><textarea v-model="agentForm.system_prompt" rows="6" /></div>
        <div class="toolbar" style="grid-column:1/-1">
          <button class="btn btn-primary" @click="saveAgent">保存 Agent</button>
          <button class="btn" @click="agentForm=null">取消</button>
        </div>
      </div>
    </div>

    <div class="panel" style="margin-top:16px">
      <h3 style="margin-top:0">生成配额</h3>
      <div class="field-grid">
        <div class="field"><label>每日任务上限</label><input v-model.number="quota.daily_job_limit" type="number" min="1" max="100000" :disabled="!canManageQuota" /></div>
        <div class="field"><label>并发任务上限</label><input v-model.number="quota.max_active_jobs" type="number" min="1" max="1000" :disabled="!canManageQuota" /></div>
      </div>
      <div class="toolbar" style="align-items:center;margin-bottom:0">
        <button v-if="canManageQuota" class="btn btn-primary" @click="saveQuota">保存配额</button>
        <span class="muted">今日已用 {{ quota.daily_jobs_used }} · 当前活跃 {{ quota.active_jobs }}</span>
      </div>
    </div>

    <div v-if="canManageQuota && authStore.state.enabled" class="panel" style="margin-top:16px">
      <h3 style="margin-top:0">组织成员</h3>
      <div class="field-grid">
        <div class="field"><label>邮箱</label><input v-model="memberForm.email" type="email" /></div>
        <div class="field"><label>显示名称</label><input v-model="memberForm.display_name" /></div>
        <div class="field"><label>初始密码</label><input v-model="memberForm.password" type="password" placeholder="12-128 个字符" /></div>
        <div class="field"><label>角色</label><select v-model="memberForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div>
      </div>
      <button class="btn btn-primary" @click="addMember">添加成员</button>
      <div class="field-grid" style="margin-top:14px">
        <div class="field"><label>已有账号邀请邮箱</label><input v-model="inviteForm.email" type="email" /></div>
        <div class="field"><label>邀请角色</label><select v-model="inviteForm.role"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div>
        <div class="field"><label>有效小时</label><input v-model.number="inviteForm.ttl_hours" type="number" min="1" max="168" /></div>
      </div>
      <button class="btn" @click="inviteMember">创建安全邀请</button>
      <div v-if="inviteLink" class="field" style="margin-top:10px"><label>邀请链接</label><input :value="inviteLink" readonly /></div>
      <table v-if="invitations.length" class="table" style="margin-top:14px">
        <thead><tr><th>邀请邮箱</th><th>角色</th><th>状态</th><th>有效期</th><th></th></tr></thead>
        <tbody>
          <tr v-for="invitation in invitations" :key="invitation.id">
            <td>{{ invitation.email }}</td><td>{{ invitation.role }}</td><td>{{ invitation.status }}</td><td>{{ invitation.expires_at }}</td>
            <td><div v-if="invitation.status === 'pending' || invitation.status === 'expired'" class="toolbar" style="margin:0"><button class="btn" @click="resendInvitation(invitation)">重发</button><button v-if="invitation.status === 'pending'" class="btn btn-danger" @click="revokeInvitation(invitation)">撤销</button></div></td>
          </tr>
        </tbody>
      </table>
      <table class="table" style="margin-top:14px">
        <thead><tr><th>成员</th><th>邮箱</th><th>角色</th><th></th></tr></thead>
        <tbody>
          <tr v-for="member in members" :key="member.user_id">
            <td>{{ member.display_name }}</td><td>{{ member.email }}</td>
            <td><select v-if="member.role !== 'owner'" v-model="member.role" @change="changeMemberRole(member)"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select><span v-else>所有者</span></td>
            <td><div v-if="member.role !== 'owner' && member.user_id !== authStore.state.actor?.user.id" class="toolbar" style="margin:0"><button class="btn btn-danger" @click="removeMember(member)">移除</button></div></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="authStore.state.enabled" class="panel" style="margin-top:16px">
      <h3 style="margin-top:0">修改密码</h3>
      <div class="field-grid">
        <div class="field"><label>当前密码</label><input v-model="passwordForm.current" type="password" /></div>
        <div class="field"><label>新密码</label><input v-model="passwordForm.next" type="password" /></div>
        <div class="field"><label>确认新密码</label><input v-model="passwordForm.confirm" type="password" /></div>
      </div>
      <button class="btn btn-primary" @click="changePassword">更新密码</button>
    </div>

    <div v-if="authStore.state.actor?.role === 'owner'" class="panel data-control" style="margin-top:16px">
      <h3 style="margin-top:0">数据控制</h3>
      <div class="toolbar"><button class="btn" @click="exportOrganization">导出组织数据</button></div>
      <div class="field-grid">
        <div class="field"><label>当前密码</label><input v-model="deleteForm.password" type="password" /></div>
        <div class="field"><label>输入组织标识 {{ authStore.state.actor.organization.slug }}</label><input v-model="deleteForm.confirmation" /></div>
      </div>
      <button class="btn btn-danger" @click="deleteOrganization">永久删除组织</button>
    </div>

    <div class="panel" style="margin-top:16px">
      <h3 style="margin-top:0">内置厂商模板</h3>
      <p class="muted" style="font-size:13px">{{ providers.length }} 个预设，供配置参考</p>
    </div>

    <div v-if="toast" class="toast">{{ toast }}</div>
  </div>
</template>
