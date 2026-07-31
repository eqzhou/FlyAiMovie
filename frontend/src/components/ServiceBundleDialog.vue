<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Boxes, ShieldCheck } from 'lucide-vue-next'
import {
  serviceBundleAPI,
  type AIServiceBundle,
  type AIServiceType,
  type BundleAgentType,
  type ServiceBundleDraft,
  type ServiceBundlePreview,
  type ServiceBundleTestResult,
} from '../api'
import { confirmAction } from '../composables/useConfirm'

const props = defineProps<{ canManage: boolean }>()
const emit = defineEmits<{ applied: [] }>()

const serviceTypes: AIServiceType[] = ['text', 'image', 'video', 'audio']
const serviceLabels: Record<AIServiceType, string> = { text: '文本', image: '图片', video: '视频', audio: '音频' }
const agentLabels: Record<BundleAgentType, string> = {
  script_rewriter: '剧本改写',
  extractor: '角色场景提取',
  storyboard_breaker: '分镜拆解',
  voice_assigner: '音色分配',
  grid_prompt_generator: '宫格提示词',
}
const bundles = ref<AIServiceBundle[]>([])
const selected = ref<AIServiceBundle | null>(null)
const credentials = ref<Record<AIServiceType, string>>({ text: '', image: '', video: '', audio: '' })
const applyAgentDefaults = ref(true)
const preview = ref<ServiceBundlePreview | null>(null)
const loading = ref(true)
const previewing = ref(false)
const testing = ref(false)
const applying = ref(false)
const testResults = ref<ServiceBundleTestResult[]>([])
const error = ref('')
const notice = ref('')

const selectedServices = computed(() => serviceTypes.map((type) => selected.value?.services.find((service) => service.service_type === type)).filter(Boolean))

function errorMessage(cause: unknown, fallback: string) {
  const message = cause instanceof Error ? cause.message : fallback
  if (/stale preview/i.test(message)) return '服务配置已发生变化，请重新预览后再应用'
  return message
}

async function loadBundles() {
  loading.value = true
  error.value = ''
  try {
    bundles.value = await serviceBundleAPI.list()
  } catch (cause) {
    error.value = errorMessage(cause, '服务组合加载失败')
  } finally {
    loading.value = false
  }
}

function openBundle(bundle: AIServiceBundle) {
  if (!props.canManage) return
  selected.value = bundle
  credentials.value = { text: '', image: '', video: '', audio: '' }
  applyAgentDefaults.value = true
  preview.value = null
  testResults.value = []
  error.value = ''
}

function closeBundle() {
  if (previewing.value || testing.value || applying.value) return
  selected.value = null
  credentials.value = { text: '', image: '', video: '', audio: '' }
  preview.value = null
  testResults.value = []
  error.value = ''
}

function draft(): ServiceBundleDraft {
  return {
    ...(selected.value && selected.value.id > 0 ? { bundle_id: selected.value.id } : { bundle_key: selected.value?.key }),
    apply_agent_defaults: applyAgentDefaults.value,
    credentials: serviceTypes.reduce<Partial<Record<AIServiceType, string>>>((result, type) => ({ ...result, [type]: credentials.value[type] }), {}),
  }
}

async function previewBundle() {
  if (!selected.value || previewing.value) return
  previewing.value = true
  error.value = ''
  try {
    preview.value = await serviceBundleAPI.preview(draft())
  } catch (cause) {
    error.value = errorMessage(cause, '服务组合预览失败')
  } finally {
    previewing.value = false
  }
}

async function testBundle() {
  if (!selected.value || testing.value) return
  testing.value = true
  error.value = ''
  testResults.value = []
  try {
    const result = await serviceBundleAPI.test(draft())
    testResults.value = [...result.results]
  } catch (cause) {
    error.value = errorMessage(cause, '服务组合测试失败')
  } finally {
    testing.value = false
  }
}

async function applyBundle() {
  if (!selected.value || !preview.value?.preview_token || applying.value) return
  const accepted = await confirmAction({
    title: '应用服务组合',
    message: applyAgentDefaults.value
      ? `将一次性应用 ${preview.value.items.length} 个服务配置和 ${preview.value.agents.length} 个 Agent 默认模型。`
      : `将一次性应用 ${preview.value.items.length} 个服务配置。`,
    detail: preview.value.conflicts.length ? `其中 ${preview.value.conflicts.length} 项会复用或替换现有配置。` : '预览中的变更将以事务方式提交。',
    confirmText: '应用组合',
  })
  if (!accepted || !preview.value) return
  applying.value = true
  error.value = ''
  try {
    await serviceBundleAPI.apply({ ...draft(), preview_token: preview.value.preview_token })
    closeBundleAfterSuccess()
    notice.value = '服务组合已应用，AI 服务列表已刷新'
    emit('applied')
  } catch (cause) {
    error.value = errorMessage(cause, '服务组合应用失败')
    if (/重新预览/.test(error.value)) preview.value = null
  } finally {
    applying.value = false
  }
}

function closeBundleAfterSuccess() {
  selected.value = null
  credentials.value = { text: '', image: '', video: '', audio: '' }
  preview.value = null
  testResults.value = []
}

function actionLabel(action: string) {
  return action === 'reuse' ? '复用现有配置' : '创建新配置'
}

function agentActionLabel(action: string) {
  if (action === 'reuse') return '复用现有 Agent'
  if (action === 'update') return '更新现有 Agent'
  return '创建新 Agent'
}

function conflictLabel(kind: string, type: AIServiceType) {
  if (kind === 'default_replaced') return `将替换当前默认${serviceLabels[type]}服务`
  if (kind === 'reused') return `将复用现有${serviceLabels[type]}服务`
  return `${serviceLabels[type]}服务存在配置冲突`
}

watch(credentials, () => {
  if (preview.value) preview.value = null
  if (testResults.value.length) testResults.value = []
}, { deep: true })

watch(applyAgentDefaults, () => {
  if (preview.value) preview.value = null
})

onMounted(loadBundles)
</script>

<template>
  <section class="settings-section" aria-label="服务组合">
    <div class="settings-section-head">
      <div><h2>服务组合</h2><p class="muted">一次配置文本、图片、视频与音频服务；密钥只随本次请求提交</p></div>
    </div>
    <div v-if="notice" class="inline-alert bundle-success" role="status"><div><strong>{{ notice }}</strong><span>临时输入的四类密钥已从页面清空。</span></div><button type="button" class="btn" @click="notice = ''">关闭</button></div>
    <div v-if="error && !selected" class="inline-alert" role="alert"><div><strong>服务组合加载失败</strong><span>{{ error }}</span></div><button type="button" class="btn" @click="loadBundles">重试加载</button></div>
    <div v-if="loading" class="surface-empty empty" role="status">正在加载服务组合…</div>
    <div v-else class="bundle-grid">
      <article v-for="bundle in bundles" :key="`${bundle.id}-${bundle.key}`" class="panel bundle-card">
        <div class="bundle-card-head"><span class="bundle-icon" aria-hidden="true"><Boxes :size="19" /></span><div><h3>{{ bundle.name }}</h3><p class="muted">{{ bundle.description }}</p></div></div>
        <ul class="bundle-services" aria-label="包含的服务">
          <li v-for="service in bundle.services" :key="service.service_type"><strong>{{ serviceLabels[service.service_type] }}</strong><span>{{ service.provider }} · {{ service.model || '厂商默认' }}</span></li>
        </ul>
        <button v-if="canManage" type="button" class="btn btn-primary bundle-open" :aria-label="`${bundle.name} · 配置并应用`" @click="openBundle(bundle)">配置并应用</button>
      </article>
      <div v-if="!bundles.length" class="panel surface-empty empty" role="status"><strong>暂无可用服务组合</strong><span class="muted">可继续逐项配置 AI 服务。</span></div>
    </div>

    <Teleport to="body">
      <div v-if="selected" class="modal-mask" @click.self="closeBundle">
        <form class="modal settings-modal settings-modal-wide" role="dialog" aria-modal="true" aria-labelledby="bundle-dialog-title" @keydown.esc="closeBundle" @submit.prevent="previewBundle">
          <h3 id="bundle-dialog-title">应用服务组合</h3>
          <p class="prompt-preview-name">{{ selected.name }} · 四项服务将一起预览和应用</p>
          <div class="bundle-service-editor">
            <article v-for="service in selectedServices" :key="service!.service_type" class="bundle-service-row">
              <div><strong>{{ serviceLabels[service!.service_type] }} · {{ service!.name }}</strong><span class="muted">{{ service!.provider }} / {{ service!.model || '厂商默认' }}</span></div>
              <div class="field"><label :for="`bundle-key-${service!.service_type}`">{{ serviceLabels[service!.service_type] }} API Key</label><input :id="`bundle-key-${service!.service_type}`" v-model="credentials[service!.service_type]" type="password" autocomplete="off" :disabled="previewing || testing || applying" /></div>
            </article>
          </div>
          <div class="bundle-privacy"><ShieldCheck :size="16" aria-hidden="true" /><span>密钥不会保存为模板，也不会在预览结果中回显。</span></div>
          <label class="settings-check"><input v-model="applyAgentDefaults" type="checkbox" :disabled="previewing || applying" /> 同时同步 5 个 Agent 默认模型</label>
          <p class="field-help muted">使用文本服务模型；已有 Agent 只更新模型和启用状态，保留自定义提示词与参数。</p>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <div v-if="testResults.length" class="bundle-test-results" aria-live="polite"><strong>连接测试</strong><ul><li v-for="result in testResults" :key="result.service_type"><span>{{ serviceLabels[result.service_type] }}</span><span :class="result.status === 'ok' ? 'ok' : 'failed'">{{ result.status === 'ok' ? '测试通过' : '测试失败' }} · {{ result.detail || result.message || '无详情' }}</span></li></ul></div>
          <div v-if="preview" class="bundle-preview" aria-live="polite">
            <h4>变更预览</h4>
            <ul><li v-for="item in preview.items" :key="item.service_type"><strong>{{ serviceLabels[item.service_type] }}</strong><span>{{ actionLabel(item.action) }}</span></li></ul>
            <template v-if="preview.agents.length"><h4>同步 Agent 默认模型</h4><ul><li v-for="agent in preview.agents" :key="agent.agent_type"><strong>{{ agentLabels[agent.agent_type] }}</strong><span>{{ agentActionLabel(agent.action) }} · {{ agent.model }}</span></li></ul></template>
            <div v-if="preview.conflicts.length" class="bundle-conflicts"><strong>需要注意</strong><p v-for="conflict in preview.conflicts" :key="`${conflict.service_type}-${conflict.kind}-${conflict.config_id}`">{{ conflictLabel(conflict.kind, conflict.service_type) }}</p></div>
          </div>
          <div class="modal-actions"><button type="button" class="btn" :disabled="previewing || testing || applying" @click="closeBundle">取消</button><button type="button" class="btn" :disabled="previewing || testing || applying" @click="testBundle">{{ testing ? '测试中…' : '测试全部' }}</button><button type="submit" class="btn" :disabled="previewing || testing || applying">{{ previewing ? '预览中…' : '预览变更' }}</button><button v-if="preview" type="button" class="btn btn-primary" :disabled="testing || applying" @click="applyBundle">{{ applying ? '应用中…' : '确认应用' }}</button></div>
        </form>
      </div>
    </Teleport>
  </section>
</template>
