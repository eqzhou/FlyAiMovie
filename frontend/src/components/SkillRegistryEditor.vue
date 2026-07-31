<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Archive, BookOpen, RotateCcw, Send, Upload } from 'lucide-vue-next'
import { skillRegistryAPI, type SkillDetail, type SkillRegistryItem, type SkillVersion } from '../api'
import { confirmAction } from '../composables/useConfirm'

const props = defineProps<{ canManage: boolean; authEnabled: boolean }>()

const agentLabels: Record<string, string> = {
  script_rewriter: '剧本改写',
  extractor: '角色场景提取',
  storyboard_breaker: '分镜拆解',
  voice_assigner: '音色分配',
  grid_prompt_generator: '宫格提示词',
}
const skills = ref<SkillRegistryItem[]>([])
const selectedAgent = ref('')
const detail = ref<SkillDetail | null>(null)
const mainMarkdown = ref('')
const referencesJSON = ref('{}')
const loading = ref(true)
const detailLoading = ref(false)
const saving = ref(false)
const changingVersionID = ref<number | null>(null)
const archiving = ref(false)
const error = ref('')
const formError = ref('')
const notice = ref('')

const selectedLabel = computed(() => agentLabels[selectedAgent.value] || selectedAgent.value)
const sortedVersions = computed(() => [...(detail.value?.versions || [])].sort((left, right) => right.version - left.version))
const publishedVersionID = computed(() => detail.value?.published_version_id ?? detail.value?.published_version?.id)
const isArchived = computed(() => Boolean(detail.value?.archived_at))

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error && cause.message.trim() ? cause.message : fallback
}

async function loadSkills() {
  loading.value = true
  error.value = ''
  try {
    skills.value = await skillRegistryAPI.list()
  } catch (cause) {
    error.value = errorMessage(cause, 'Skills 加载失败')
  } finally {
    loading.value = false
  }
}

async function selectSkill(agentType: string) {
  selectedAgent.value = agentType
  detail.value = null
  await loadDetail()
}

async function loadDetail() {
  if (!selectedAgent.value) return
  const requestedAgent = selectedAgent.value
  detailLoading.value = true
  error.value = ''
  try {
    const result = await skillRegistryAPI.get(requestedAgent)
    if (selectedAgent.value !== requestedAgent) return
    detail.value = { ...result, versions: [...(result.versions || [])], publications: [...(result.publications || [])] }
  } catch (cause) {
    if (selectedAgent.value === requestedAgent) {
      detail.value = null
      error.value = errorMessage(cause, 'Skill 详情加载失败')
    }
  } finally {
    if (selectedAgent.value === requestedAgent) detailLoading.value = false
  }
}

function parseReferences(): Record<string, string> | null {
  let value: unknown
  try {
    value = JSON.parse(referencesJSON.value || '{}')
  } catch {
    formError.value = 'References JSON 格式无效'
    return null
  }
  if (!value || Array.isArray(value) || typeof value !== 'object') {
    formError.value = 'References 必须是文件路径到内容的 JSON 对象'
    return null
  }
  const entries = Object.entries(value as Record<string, unknown>)
  for (const [path, content] of entries) {
    if (!/^references\/[\w./-]+\.md$/i.test(path) || path.includes('..')) {
      formError.value = `Reference 路径无效：${path}`
      return null
    }
    if (typeof content !== 'string') {
      formError.value = `Reference 内容必须是字符串：${path}`
      return null
    }
  }
  return Object.fromEntries(entries) as Record<string, string>
}

async function createVersion() {
  if (!selectedAgent.value || saving.value) return
  formError.value = ''
  if (!mainMarkdown.value.trim()) {
    formError.value = '请填写主文档内容'
    return
  }
  const references = parseReferences()
  if (!references) return
  saving.value = true
  try {
    await skillRegistryAPI.createVersion(selectedAgent.value, { main_markdown: mainMarkdown.value, references })
    mainMarkdown.value = ''
    referencesJSON.value = '{}'
    notice.value = 'Skill 新版本已创建'
    await loadDetail()
    await loadSkills()
  } catch (cause) {
    formError.value = errorMessage(cause, '创建 Skill 版本失败')
  } finally {
    saving.value = false
  }
}

async function publishVersion(version: SkillVersion) {
  const accepted = await confirmAction({ title: '发布 Skill 版本', message: `确定发布 ${selectedLabel.value} v${version.version}？`, detail: '后续 Agent 运行将使用此版本。', confirmText: '发布版本' })
  if (!accepted) return
  await changePublication(version, false)
}

async function rollbackVersion(version: SkillVersion) {
  const accepted = await confirmAction({ title: '回滚 Skill', message: `确定回滚到 ${selectedLabel.value} v${version.version}？`, detail: '该历史版本会重新成为当前发布版本。', confirmText: '确认回滚' })
  if (!accepted) return
  await changePublication(version, true)
}

async function restoreVersion(version: SkillVersion) {
  const accepted = await confirmAction({ title: '恢复 Skill', message: `确定恢复 ${selectedLabel.value} v${version.version}？`, detail: '该版本会重新发布，后续 Agent 运行将恢复使用组织 Skill。', confirmText: '恢复版本' })
  if (!accepted) return
  await changePublication(version, false)
}

async function changePublication(version: SkillVersion, rollback: boolean) {
  if (!selectedAgent.value || changingVersionID.value !== null) return
  changingVersionID.value = version.id
  error.value = ''
  try {
    if (rollback) await skillRegistryAPI.rollback(selectedAgent.value, version.id)
    else await skillRegistryAPI.publish(selectedAgent.value, version.id)
    notice.value = rollback ? `已回滚到 v${version.version}` : `v${version.version} 已发布`
    await loadDetail()
    await loadSkills()
  } catch (cause) {
    error.value = errorMessage(cause, rollback ? 'Skill 回滚失败' : 'Skill 发布失败')
  } finally {
    changingVersionID.value = null
  }
}

async function archiveSkill() {
  if (!selectedAgent.value || archiving.value) return
  const accepted = await confirmAction({ title: '归档 Skill', message: `确定归档 ${selectedLabel.value}？`, detail: '发布版本会被停用，Agent 将回退到内置 Skill。', confirmText: '确认归档', tone: 'danger' })
  if (!accepted) return
  archiving.value = true
  error.value = ''
  try {
    await skillRegistryAPI.archive(selectedAgent.value)
    notice.value = 'Skill 已归档'
    await loadDetail()
    await loadSkills()
  } catch (cause) {
    error.value = errorMessage(cause, 'Skill 归档失败')
  } finally {
    archiving.value = false
  }
}

function versionReferences(version: SkillVersion) {
  try {
    return JSON.stringify(JSON.parse(version.references_json || '{}'), null, 2)
  } catch {
    return version.references_json || '{}'
  }
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

onMounted(loadSkills)
</script>

<template>
  <section class="settings-section" aria-label="Skills">
    <div class="settings-section-head"><div><h2>Skills</h2><p class="muted">管理五类 Agent 的组织级 SKILL.md、References 与发布版本</p></div></div>
    <div v-if="!authEnabled" class="inline-alert skill-readonly-notice" role="status"><div><strong>本地工作区 Skills</strong><span>认证未启用；版本仅保存到当前本地工作区，可创建、发布、回滚或归档。</span></div></div>
    <div v-if="notice" class="inline-alert bundle-success" role="status"><div><strong>{{ notice }}</strong><span>Skill 注册表已刷新。</span></div><button type="button" class="btn" @click="notice = ''">关闭</button></div>
    <div v-if="error" class="inline-alert" role="alert"><div><strong>Skill 操作失败</strong><span>{{ error }}</span></div><button v-if="!skills.length" type="button" class="btn" @click="loadSkills">重试加载</button></div>
    <div v-if="loading" class="surface-empty empty" role="status">正在加载 Skills…</div>
    <div v-else class="skill-registry-layout">
      <nav class="panel skill-agent-list" aria-label="Agent Skills">
        <button v-for="skill in skills" :key="skill.agent_type" type="button" :class="{ active: selectedAgent === skill.agent_type }" :aria-pressed="selectedAgent === skill.agent_type" @click="selectSkill(skill.agent_type)">
          <span><BookOpen :size="16" aria-hidden="true" /><strong>{{ agentLabels[skill.agent_type] || skill.agent_type }}</strong></span>
          <small>{{ skill.registry?.archived_at ? '已归档' : skill.source === 'database' ? '组织版本' : '内置版本' }}</small>
        </button>
      </nav>
      <div class="skill-detail">
        <div v-if="!selectedAgent" class="panel surface-empty empty" role="status"><strong>选择一个 Agent Skill</strong><span class="muted">查看内容、历史版本和发布状态。</span></div>
        <div v-else-if="detailLoading" class="panel surface-empty empty" role="status">正在加载 {{ selectedLabel }}…</div>
        <template v-else-if="detail">
          <div class="panel skill-detail-head">
            <div><h3>{{ selectedLabel }}</h3><p class="muted">{{ detail.source === 'builtin' ? '当前使用内置 Skill' : '当前使用组织 Skill 注册表' }}</p></div>
            <span v-if="isArchived" class="job-status canceled">已归档</span><span v-else-if="detail.published_version" class="job-status succeeded">当前发布：v{{ detail.published_version.version }}</span><span v-else class="job-status queued">未发布</span>
          </div>

          <form v-if="canManage" class="panel skill-editor" @submit.prevent="createVersion">
            <h3>创建不可变版本</h3>
            <div class="field"><label for="skill-main-markdown">主文档</label><textarea id="skill-main-markdown" v-model="mainMarkdown" rows="10" maxlength="262144" placeholder="# SKILL.md" :disabled="saving || isArchived" /></div>
            <div class="field"><label for="skill-references-json">References JSON</label><textarea id="skill-references-json" v-model="referencesJSON" rows="6" spellcheck="false" :disabled="saving || isArchived" /></div>
            <p class="field-help muted">格式：{"references/rules.md":"规则内容"}；路径必须位于 references/ 且以 .md 结尾。</p>
            <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
            <div class="modal-actions"><button type="submit" class="btn btn-primary" :disabled="saving || isArchived"><Upload :size="15" aria-hidden="true" />{{ saving ? '创建中…' : '创建版本' }}</button></div>
          </form>

          <div v-else-if="detail.published_version || detail.content" class="panel skill-readonly">
            <h3>当前内容</h3>
            <pre>{{ detail.published_version?.main_markdown || detail.content }}</pre>
          </div>

          <div class="skill-version-head"><h3>版本历史</h3><button v-if="canManage && detail.versions.length && !isArchived" type="button" class="btn btn-danger" :disabled="archiving" @click="archiveSkill"><Archive :size="15" aria-hidden="true" />{{ archiving ? '归档中…' : '归档 Skill' }}</button></div>
          <div class="skill-version-list">
            <article v-for="version in sortedVersions" :key="version.id" class="panel skill-version">
              <div class="skill-version-title"><div><strong>v{{ version.version }}<template v-if="publishedVersionID === version.id"> · 已发布</template></strong><span class="muted">{{ formatTime(version.created_at) }}</span></div><div v-if="canManage" class="toolbar settings-table-actions"><button v-if="isArchived" type="button" class="btn" :disabled="changingVersionID !== null" :aria-label="`恢复 v${version.version}`" @click="restoreVersion(version)"><RotateCcw :size="14" aria-hidden="true" />恢复</button><template v-else><button v-if="publishedVersionID !== version.id" type="button" class="btn" :disabled="changingVersionID !== null" :aria-label="`发布 v${version.version}`" @click="publishVersion(version)"><Send :size="14" aria-hidden="true" />发布</button><button v-if="publishedVersionID && publishedVersionID !== version.id" type="button" class="btn" :disabled="changingVersionID !== null" :aria-label="`回滚到 v${version.version}`" @click="rollbackVersion(version)"><RotateCcw :size="14" aria-hidden="true" />回滚</button></template></div></div>
              <pre>{{ version.main_markdown }}</pre>
              <details v-if="version.references_json && version.references_json !== '{}' "><summary>References</summary><pre>{{ versionReferences(version) }}</pre></details>
            </article>
            <div v-if="!sortedVersions.length" class="panel surface-empty empty" role="status"><strong>暂无组织版本</strong><span class="muted">{{ canManage ? '创建首个版本后即可发布。' : '当前使用内置 Skill。' }}</span></div>
          </div>
        </template>
      </div>
    </div>
  </section>
</template>
