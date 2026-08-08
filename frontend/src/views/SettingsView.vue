<script setup lang="ts">
import { FlaskConical, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { authStore } from '../auth'
import ServiceBundleDialog from '../components/ServiceBundleDialog.vue'
import SkillRegistryEditor from '../components/SkillRegistryEditor.vue'
import { useSettings } from './settings/useSettings'

const { configs, providers, agents, promptTemplates, voiceCatalog, form, toast, loading, loaded, loadError, serviceError, savingService, testingDraftService, serviceTestResult, agentForm, activeSection, showServiceModal, editingConfigID, testingConfigID, showMemberModal, showInviteModal, showPasswordModal, showDeleteModal, promptForm, promptContentInput, promptQuery, promptCategory, promptState, promptFormError, promptDraftPreview, previewingPromptDraft, savingPrompt, previewTemplate, previewingTemplate, revisionTemplate, promptRevisions, restoringRevision, promptHistoryLoading, promptHistoryError, promptActionNotice, voiceQuery, voicePreviewText, previewingVoiceID, syncingVoices, voicePreviewURLs, previewVariables, previewResult, previewError, promptCategoryLabels, promptVariableLabels, quota, cache, canManageQuota, canManageSettings, canManageSkills, canPreviewVoices, members, memberForm, inviteForm, inviteLink, inviteEmailSent, inviting, copyingInvite, savingMember, changingPassword, invitations, passwordForm, deleteForm, isPlatformAdmin, platformSettings, platformSettingsLoaded, platformSettingsLoading, platformSettingsError, savingPlatformSettings, availableProviders, filteredVoices, filteredPromptTemplates, serviceTypeLabel, providerLabel, agentTypeLabel, load, addMember, inviteMember, revokeInvitation, resendInvitation, invitationStatusLabel, copyInviteLink, openInviteModal, closeInviteModal, changeMemberRole, removeMember, loadPlatformSettings, savePlatformSettings, changePassword, exportOrganization, deleteOrganization, saveQuota, purgeCache, formatBytes, openCreateService, editService, saveService, testService, testDraftService, remove, syncVoices, previewVoice, editAgent, promptVariables, promptToken, isBuiltInPrompt, openCreatePrompt, duplicatePrompt, editPrompt, previewPromptForm, insertPromptVariable, savePrompt, openPreview, closePreview, renderPreview, restorePrompt, openPromptHistory, closePromptHistory, restorePromptRevision, formatRevisionTime, removePrompt, saveAgent } = useSettings()
void promptContentInput
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
      <button role="tab" :aria-selected="activeSection === 'bundles'" :class="{ active: activeSection === 'bundles' }" @click="activeSection = 'bundles'">服务组合</button>
      <button role="tab" :aria-selected="activeSection === 'agents'" :class="{ active: activeSection === 'agents' }" @click="activeSection = 'agents'">Agent</button>
      <button role="tab" :aria-selected="activeSection === 'skills'" :class="{ active: activeSection === 'skills' }" @click="activeSection = 'skills'">Skills</button>
      <button role="tab" :aria-selected="activeSection === 'prompts'" :class="{ active: activeSection === 'prompts' }" @click="activeSection = 'prompts'">提示词</button>
      <button role="tab" :aria-selected="activeSection === 'voices'" :class="{ active: activeSection === 'voices' }" @click="activeSection = 'voices'">音色库</button>
      <button role="tab" :aria-selected="activeSection === 'organization'" :class="{ active: activeSection === 'organization' }" @click="activeSection = 'organization'">组织与权限</button>
      <button role="tab" :aria-selected="activeSection === 'security'" :class="{ active: activeSection === 'security' }" @click="activeSection = 'security'">安全与数据</button>
    </div>
    <div v-if="loadError" class="inline-alert" role="alert"><div><strong>部分设置暂未更新</strong><span>{{ loadError }}</span></div><button class="btn" type="button" @click="load">重试加载</button></div>
    <div v-else-if="loading && !loaded" class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>正在同步设置</strong>
        <p class="muted">加载 AI 服务、音色、提示词与组织信息…</p>
      </div>
    </div>

    <section v-if="(loaded || !loading) && activeSection === 'services'" class="settings-section" role="tabpanel">
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
        <div v-if="!configs.length" class="surface-empty empty" role="status"><strong>尚未配置 AI 服务</strong><span class="muted">添加文本、图片、视频或音频服务后即可在工作台调用。</span></div>
      </div>
    </section>

    <div v-if="(loaded || !loading) && activeSection === 'bundles'" role="tabpanel" aria-label="服务组合">
      <ServiceBundleDialog :can-manage="canManageSettings" @applied="load" />
    </div>

    <div v-if="(loaded || !loading) && activeSection === 'skills'" role="tabpanel" aria-label="Skills">
      <SkillRegistryEditor :can-manage="canManageSkills" :auth-enabled="authStore.state.enabled" />
    </div>

    <section v-if="(loaded || !loading) && activeSection === 'voices'" class="settings-section" role="tabpanel">
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
        <div v-else class="surface-empty empty" role="status"><strong>{{ voiceCatalog.length ? '没有匹配的音色' : '尚未同步音色' }}</strong><span class="muted">{{ voiceCatalog.length ? '试试调整搜索关键词' : '配置音频服务后点击同步音色' }}</span></div>
      </div>
    </section>

    <section v-if="(loaded || !loading) && activeSection === 'prompts'" class="settings-section" role="tabpanel">
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
        <div v-else class="surface-empty empty" role="status"><strong>{{ promptTemplates.length ? '没有匹配的提示词模板' : '尚未创建提示词模板' }}</strong><span class="muted">{{ promptTemplates.length ? '试试调整筛选条件' : '可新建模板或使用内置默认提示词' }}</span></div>
      </div>
    </section>

    <section v-if="(loaded || !loading) && activeSection === 'agents'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>Agent 预设</h2><p class="muted">为制作流程配置模型和执行边界</p></div></div>
      <div class="panel">
        <table class="table">
          <thead><tr><th>类型</th><th>名称</th><th>模型</th><th>状态</th><th></th></tr></thead>
          <tbody><tr v-for="a in agents" :key="a.id"><td>{{ agentTypeLabel(a.agent_type) }}</td><td>{{ a.name }}</td><td>{{ a.model || '继承文本默认' }}</td><td><span class="job-status" :class="a.is_active ? 'succeeded' : 'canceled'">{{ a.is_active ? '启用' : '停用' }}</span></td><td><button v-if="canManageSettings" class="btn" @click="editAgent(a)">编辑</button></td></tr></tbody>
        </table>
      </div>
    </section>

    <section v-if="(loaded || !loading) && activeSection === 'organization'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>组织与权限</h2><p class="muted">成员访问、生成额度与本地存储</p></div><div v-if="canManageQuota && authStore.state.enabled" class="toolbar"><button class="btn" @click="showMemberModal = true">添加成员</button><button class="btn btn-primary" @click="openInviteModal">创建邀请</button></div></div>
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
            <td>{{ invitation.email }}</td><td>{{ invitation.role }}</td><td><span class="invite-status" :data-status="invitation.status">{{ invitationStatusLabel(invitation.status) }}</span></td><td>{{ invitation.expires_at }}</td>
            <td><div v-if="invitation.status === 'pending' || invitation.status === 'expired'" class="toolbar" style="margin:0"><button class="btn" @click="resendInvitation(invitation)">重发</button><button v-if="invitation.status === 'pending'" class="btn btn-danger" @click="revokeInvitation(invitation)">撤销</button></div></td>
          </tr>
        </tbody>
        </table>
        <div v-else class="surface-empty empty" role="status"><strong>没有待处理邀请</strong><span class="muted">创建安全邀请后，成员可通过邮件或链接加入组织</span></div>
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

    <section v-if="(loaded || !loading) && activeSection === 'security'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>安全与数据</h2><p class="muted">账户凭据、组织数据导出与删除</p></div></div>
      <div class="settings-command-list">
        <div v-if="isPlatformAdmin" class="settings-command">
          <div>
            <strong>注册设置</strong>
            <p class="muted">控制全站公开注册与邮箱校验门禁。开启邮箱校验后，未验证账号无法登录；第一期不会自动发送验证邮件。</p>
            <div v-if="platformSettingsLoading && !platformSettingsLoaded" class="muted" role="status">正在加载注册设置…</div>
            <div v-else-if="platformSettingsError && !platformSettingsLoaded" class="inline-alert" role="alert">
              <div><strong>注册设置加载失败</strong><span>{{ platformSettingsError }}</span></div>
              <button class="btn" type="button" @click="loadPlatformSettings">重试加载</button>
            </div>
            <div v-else class="service-toggle-grid" style="margin-top:12px">
              <label class="settings-check"><input v-model="platformSettings.registration_enabled" type="checkbox" :disabled="savingPlatformSettings || platformSettingsLoading" /> 开放公开注册</label>
              <label class="settings-check"><input v-model="platformSettings.require_email_verification" type="checkbox" :disabled="savingPlatformSettings || platformSettingsLoading" /> 要求邮箱校验</label>
            </div>
            <p v-if="platformSettingsError && platformSettingsLoaded" class="form-error" role="alert">{{ platformSettingsError }}</p>
          </div>
          <button class="btn btn-primary" type="button" :disabled="savingPlatformSettings || platformSettingsLoading || !platformSettingsLoaded" @click="savePlatformSettings">{{ savingPlatformSettings ? '保存中…' : '保存注册设置' }}</button>
        </div>
        <div v-if="authStore.state.enabled" class="settings-command"><div><strong>登录密码</strong><p class="muted">更新当前账户的登录密码</p></div><button class="btn" @click="showPasswordModal = true">修改密码</button></div>
        <div v-if="authStore.state.actor?.role === 'owner'" class="settings-command"><div><strong>组织数据</strong><p class="muted">下载当前组织的完整 JSON 数据副本</p></div><button class="btn" @click="exportOrganization">导出组织数据</button></div>
        <div v-if="authStore.state.actor?.role === 'owner'" class="settings-command danger"><div><strong>永久删除组织</strong><p class="muted">删除组织、项目、任务和已缓存媒体，此操作无法撤销</p></div><button class="btn btn-danger" @click="showDeleteModal = true">永久删除组织</button></div>
      </div>
    </section>

    <div v-if="showServiceModal" class="modal-mask" @click.self="showServiceModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="service-modal-title" @keydown.esc="showServiceModal = false" @submit.prevent="saveService"><h3 id="service-modal-title">{{ editingConfigID ? '编辑 AI 服务' : '添加 AI 服务' }}</h3><div class="field"><label for="service-type">类型</label><select id="service-type" v-model="form.service_type" autofocus><option value="text">文本</option><option value="image">图片</option><option value="video">视频</option><option value="audio">音频/TTS</option></select></div><div class="field"><label for="service-provider">厂商</label><select id="service-provider" v-model="form.provider"><option v-for="provider in availableProviders" :key="provider.value" :value="provider.value">{{ provider.label }}</option></select></div><div class="field"><label for="service-name">名称</label><input id="service-name" v-model="form.name" placeholder="我的 GPT" /></div><div class="field"><label for="service-url">Base URL</label><input id="service-url" v-model="form.base_url" /></div><div class="field"><label for="service-key">API Key</label><input id="service-key" v-model="form.api_key" type="password" :placeholder="editingConfigID ? '留空保持原密钥' : ''" /></div><div class="field"><label for="service-model">模型</label><input id="service-model" v-model="form.model" placeholder="gpt-4o-mini" /></div><div class="service-toggle-grid"><label class="settings-check"><input v-model="form.is_active" type="checkbox" /> 启用服务</label><label class="settings-check" :class="{ disabled: !form.is_active }"><input v-model="form.is_default" type="checkbox" :disabled="!form.is_active" /> 设为默认</label></div><div class="service-test-row"><button type="button" class="btn" :disabled="savingService || testingDraftService" @click="testDraftService"><FlaskConical :size="14" aria-hidden="true" />{{ testingDraftService ? '测试中…' : '测试当前配置' }}</button><span v-if="serviceTestResult" class="service-test-ok" role="status">{{ serviceTestResult }}</span></div><p v-if="serviceError" class="form-error" role="alert">{{ serviceError }}</p><div class="modal-actions"><button type="button" class="btn" :disabled="savingService || testingDraftService" @click="showServiceModal = false">取消</button><button type="submit" class="btn btn-primary" :disabled="savingService || testingDraftService">{{ savingService ? '保存中…' : editingConfigID ? '保存修改' : '保存配置' }}</button></div></form></div>

    <div v-if="agentForm" class="modal-mask" @click.self="agentForm = null">
      <form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="agent-modal-title" @submit.prevent="saveAgent">
        <h3 id="agent-modal-title">编辑 Agent</h3>
        <div class="field-grid">
          <div class="field"><label for="agent-name">名称</label><input id="agent-name" v-model="agentForm.name" maxlength="200" /></div>
          <div class="field"><label for="agent-model">模型</label><input id="agent-model" v-model="agentForm.model" placeholder="继承文本默认" maxlength="2000" /></div>
          <div class="field settings-span"><label for="agent-description">说明</label><input id="agent-description" v-model="agentForm.description" maxlength="2000" /></div>
          <div class="field settings-span"><label for="agent-system-prompt">系统提示词</label><textarea id="agent-system-prompt" v-model="agentForm.system_prompt" rows="8" maxlength="20000" placeholder="留空时使用 Agent 内置提示词" /></div>
          <div class="field"><label for="agent-temperature">温度</label><input id="agent-temperature" v-model.number="agentForm.temperature" type="number" min="0" max="2" step="0.1" /></div>
          <div class="field"><label for="agent-tokens">最大输出 token</label><input id="agent-tokens" v-model.number="agentForm.max_tokens" type="number" min="1" max="128000" /></div>
          <div class="field"><label for="agent-iterations">最大模型迭代</label><input id="agent-iterations" v-model.number="agentForm.max_iterations" type="number" min="1" max="5" /><p class="field-help muted">复杂提取/分镜建议 3–5；越高耗时与费用越高。</p></div>
          <label class="settings-check"><input v-model="agentForm.is_active" type="checkbox" /> 启用</label>
        </div>
        <div class="modal-actions"><button type="button" class="btn" @click="agentForm = null">取消</button><button type="submit" class="btn btn-primary">保存 Agent</button></div>
      </form>
    </div>

    <div v-if="promptForm" class="modal-mask" @click.self="promptForm = null"><form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" :aria-labelledby="promptForm.id ? 'edit-prompt-title' : 'create-prompt-title'" @keydown.esc="promptForm = null" @submit.prevent="savePrompt"><h3 :id="promptForm.id ? 'edit-prompt-title' : 'create-prompt-title'">{{ promptForm.id ? '编辑提示词' : '新建提示词' }}</h3><div class="field-grid"><div class="field"><label for="prompt-name">名称 *</label><input id="prompt-name" v-model="promptForm.name" autofocus maxlength="200" /></div><div class="field"><label for="prompt-key">标识 *</label><input id="prompt-key" v-model="promptForm.key" :disabled="!!promptForm.id" pattern="[a-z][a-z0-9_]{1,63}" /></div><div class="field"><label for="prompt-category">分类 *</label><select id="prompt-category" v-model="promptForm.category"><option v-for="(label, value) in promptCategoryLabels" :key="value" :value="value">{{ label }}</option></select></div><label class="settings-check"><input v-model="promptForm.is_active" type="checkbox" /> 启用</label><div class="field settings-span"><label for="prompt-description">说明</label><input id="prompt-description" v-model="promptForm.description" maxlength="2000" /></div><div class="field settings-span"><label for="prompt-content">模板内容 *</label><textarea id="prompt-content" ref="promptContentInput" v-model="promptForm.content" rows="10" maxlength="20000" /></div><div class="prompt-token-picker settings-span"><span>插入变量</span><div><button v-for="(label, variable) in promptVariableLabels" :key="variable" type="button" :aria-label="`插入变量 ${label}`" :title="promptToken(variable)" @click="insertPromptVariable(variable)"><strong>{{ label }}</strong><code>{{ promptToken(variable) }}</code></button></div></div></div><p v-if="promptFormError" class="form-error" role="alert">{{ promptFormError }}</p><div v-if="promptDraftPreview" class="prompt-draft-preview" role="status"><strong>草稿预览</strong><div class="prompt-preview-result">{{ promptDraftPreview }}</div></div><div class="modal-actions"><button type="button" class="btn" :disabled="savingPrompt || previewingPromptDraft" @click="promptForm = null">取消</button><button type="button" class="btn" :disabled="savingPrompt || previewingPromptDraft || !promptForm.content.trim()" @click="previewPromptForm">{{ previewingPromptDraft ? '检查中…' : '检查并预览' }}</button><button type="submit" class="btn btn-primary" :disabled="savingPrompt || previewingPromptDraft">{{ savingPrompt ? '保存中…' : promptForm.id ? '保存修改' : '创建模板' }}</button></div></form></div>

    <div v-if="previewTemplate" class="modal-mask" @click.self="closePreview"><div class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="preview-prompt-title" @keydown.esc="closePreview"><h3 id="preview-prompt-title">预览提示词</h3><p class="prompt-preview-name">{{ previewTemplate.name }} · v{{ previewTemplate.version }}</p><div v-if="promptVariables(previewTemplate).length" class="field-grid"><div v-for="variable in promptVariables(previewTemplate)" :key="variable" class="field"><label :for="`preview-${variable}`">{{ promptVariableLabels[variable] || variable }}</label><input :id="`preview-${variable}`" v-model="previewVariables[variable]" :disabled="previewingTemplate" /></div></div><p v-if="previewError" class="form-error" role="alert">{{ previewError }}</p><div v-if="previewResult" class="prompt-preview-result" aria-live="polite">{{ previewResult }}</div><div class="modal-actions"><button type="button" class="btn" :disabled="previewingTemplate" @click="closePreview">关闭</button><button type="button" class="btn btn-primary" :disabled="previewingTemplate" @click="renderPreview">{{ previewingTemplate ? '生成中…' : '生成预览' }}</button></div></div></div>

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

    <div v-if="showMemberModal" class="modal-mask" @click.self="showMemberModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="member-modal-title" @keydown.esc="showMemberModal = false" @submit.prevent="addMember"><h3 id="member-modal-title">添加成员</h3><div class="field"><label for="member-email">邮箱</label><input id="member-email" v-model.trim="memberForm.email" type="email" maxlength="254" autocomplete="email" required :disabled="savingMember" /></div><div class="field"><label for="member-name">显示名称</label><input id="member-name" v-model.trim="memberForm.display_name" maxlength="100" autocomplete="name" :disabled="savingMember" /></div><div class="field"><label for="member-password">初始密码</label><input id="member-password" v-model="memberForm.password" type="password" minlength="8" maxlength="72" placeholder="8–72 字节" autocomplete="new-password" required :disabled="savingMember" /><p class="field-help muted">中文通常占 3 个字节。</p></div><div class="field"><label for="member-role">角色</label><select id="member-role" v-model="memberForm.role" :disabled="savingMember"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div><div class="modal-actions"><button type="button" class="btn" :disabled="savingMember" @click="showMemberModal = false">取消</button><button type="submit" class="btn btn-primary" :disabled="savingMember">{{ savingMember ? '添加中…' : '添加成员' }}</button></div></form></div>

    <div v-if="showInviteModal" class="modal-mask" @click.self="closeInviteModal">
      <form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="invite-modal-title" aria-describedby="invite-modal-help" @keydown.esc="closeInviteModal" @submit.prevent="inviteMember">
        <h3 id="invite-modal-title">{{ inviteLink ? '邀请已就绪' : '创建邀请' }}</h3>
        <p id="invite-modal-help" class="muted invite-modal-help">{{ inviteLink ? '将链接发给成员完成加入；若已配置 SMTP，系统也会尝试发送邮件。' : '生成一次性安全邀请链接。未配置邮件时，可手动复制链接发送。' }}</p>
        <template v-if="!inviteLink">
          <div class="field"><label for="invite-email">邀请邮箱</label><input id="invite-email" v-model="inviteForm.email" type="email" required autocomplete="email" :disabled="inviting" /></div>
          <div class="field"><label for="invite-role">邀请角色</label><select id="invite-role" v-model="inviteForm.role" :disabled="inviting"><option v-if="authStore.state.actor?.role === 'owner'" value="admin">管理员</option><option value="editor">编辑</option><option value="viewer">只读</option></select></div>
          <div class="field"><label for="invite-hours">有效小时</label><input id="invite-hours" v-model.number="inviteForm.ttl_hours" type="number" min="1" max="168" :disabled="inviting" /></div>
        </template>
        <div v-else class="invite-success">
          <div class="invite-delivery" role="status" :data-sent="inviteEmailSent ? 'true' : 'false'">
            <strong>{{ inviteEmailSent ? '邮件已尝试发送' : '请手动分享链接' }}</strong>
            <span class="muted">{{ inviteEmailSent ? '若收件箱未收到，可直接复制下方链接。' : '当前未成功发送邮件（未配置 SMTP 或发送失败）。' }}</span>
          </div>
          <div class="field"><label for="invite-link">邀请链接</label><input id="invite-link" :value="inviteLink" readonly /></div>
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" :disabled="inviting" @click="closeInviteModal">{{ inviteLink ? '完成' : '取消' }}</button>
          <button v-if="inviteLink" type="button" class="btn btn-primary" :disabled="copyingInvite" @click="copyInviteLink">{{ copyingInvite ? '复制中…' : '复制链接' }}</button>
          <button v-else type="submit" class="btn btn-primary" :disabled="inviting || !inviteForm.email.trim()">{{ inviting ? '创建中…' : '创建安全邀请' }}</button>
        </div>
      </form>
    </div>

    <div v-if="showPasswordModal" class="modal-mask" @click.self="showPasswordModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="password-modal-title" @keydown.esc="showPasswordModal = false" @submit.prevent="changePassword"><h3 id="password-modal-title">修改密码</h3><div class="field"><label for="current-password">当前密码</label><input id="current-password" v-model="passwordForm.current" type="password" maxlength="72" autocomplete="current-password" required :disabled="changingPassword" /></div><div class="field"><label for="new-password">新密码</label><input id="new-password" v-model="passwordForm.next" type="password" minlength="8" maxlength="72" autocomplete="new-password" required :disabled="changingPassword" /><p class="field-help muted">需为 8–72 字节，中文通常占 3 个字节。</p></div><div class="field"><label for="confirm-password">确认新密码</label><input id="confirm-password" v-model="passwordForm.confirm" type="password" minlength="8" maxlength="72" autocomplete="new-password" required :disabled="changingPassword" /></div><div class="modal-actions"><button type="button" class="btn" :disabled="changingPassword" @click="showPasswordModal = false">取消</button><button type="submit" class="btn btn-primary" :disabled="changingPassword">{{ changingPassword ? '更新中…' : '更新密码' }}</button></div></form></div>

    <div v-if="showDeleteModal" class="modal-mask" @click.self="showDeleteModal = false"><form class="modal settings-modal" role="dialog" aria-modal="true" aria-labelledby="delete-modal-title" @submit.prevent="deleteOrganization"><h3 id="delete-modal-title">永久删除组织</h3><p class="muted">此操作会删除组织及其全部数据，无法撤销。</p><div class="field"><label for="delete-password">当前密码</label><input id="delete-password" v-model="deleteForm.password" type="password" /></div><div class="field"><label for="delete-confirmation">输入组织标识 {{ authStore.state.actor?.organization.slug }}</label><input id="delete-confirmation" v-model="deleteForm.confirmation" /></div><div class="modal-actions"><button type="button" class="btn" @click="showDeleteModal = false">取消</button><button type="submit" class="btn btn-danger">确认永久删除</button></div></form></div>

    <div v-if="toast" class="toast" role="status">{{ toast }}</div>
  </div>
</template>
