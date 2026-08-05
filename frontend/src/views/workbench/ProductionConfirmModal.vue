<script setup lang="ts">
/**
 * Confirmation dialog for starting the automatic end-to-end episode build.
 *
 * Shows the pipeline stages and which service each type resolves to, so a
 * missing binding is visible before the run starts. The parent decides
 * readiness; this dialog only reports it.
 */
const stageLabels = ['剧本', '角色场景', '分镜', '首帧', '视频', '配音', '合成', '成片']

defineProps<{
  services: { type: string; label: string; config?: { name?: string; provider?: string } | null }[]
  usesExternalService: boolean
  ready: boolean
  error: string
  busy: string
}>()

const emit = defineEmits<{
  close: []
  start: []
}>()
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <div
      class="modal production-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="production-modal-title"
      @keydown.esc="emit('close')"
    >
      <h3 id="production-modal-title">自动制作本集</h3>
      <div class="production-modal-flow" aria-label="自动制作阶段">
        <span v-for="label in stageLabels" :key="label">{{ label }}</span>
      </div>
      <div class="production-service-list" aria-label="本次使用服务">
        <span v-for="service in services" :key="service.type" :class="{ missing: !service.config }"><small>{{ service.label }}</small><strong>{{ service.config?.name || '未绑定' }}</strong><em>{{ service.config?.provider || '请先配置' }}</em></span>
      </div>
      <p v-if="usesExternalService" class="production-cost-note">包含外部 AI 服务，可能产生厂商费用。</p>
      <p class="muted">将使用本集绑定的图片、视频和音频服务。制作期间仍可返回手动调整，失败后可从当前阶段重试。</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions">
        <button class="btn" type="button" @click="emit('close')">取消</button>
        <button class="btn btn-primary" type="button" :disabled="!!busy || !ready" @click="emit('start')">{{ busy === 'production-start' ? '正在启动…' : '开始制作' }}</button>
      </div>
    </div>
  </div>
</template>
