<script setup lang="ts">
import { ref } from 'vue'

/**
 * Create/edit dialog for an episode scene.
 *
 * Mirrors CharacterFormModal: `form` is the parent's live object and focus()
 * is exposed so the parent can focus the first field after opening.
 */
defineProps<{
  form: any
  error: string
  busy: string
}>()

const emit = defineEmits<{
  close: []
  submit: []
}>()

const locationInput = ref<HTMLInputElement | null>(null)

defineExpose({ focus: () => locationInput.value?.focus() })
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <form
      class="modal settings-modal"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="form.id ? 'edit-scene-title' : 'add-scene-title'"
      @keydown.esc="emit('close')"
      @submit.prevent="emit('submit')"
    >
      <h3 :id="form.id ? 'edit-scene-title' : 'add-scene-title'">{{ form.id ? '编辑场景' : '添加场景' }}</h3>
      <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
      <div class="field-grid">
        <div class="field"><label for="workbench-scene-location">地点 <span class="required-mark">*</span></label><input id="workbench-scene-location" ref="locationInput" v-model="form.location" maxlength="200" required /></div>
        <div class="field"><label for="workbench-scene-time">时间</label><input id="workbench-scene-time" v-model="form.time" maxlength="120" /></div>
        <div class="field settings-span"><label for="workbench-scene-prompt">画面提示词</label><textarea id="workbench-scene-prompt" v-model="form.prompt" rows="5" maxlength="10000" /></div>
      </div>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="btn" type="button" @click="emit('close')">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'scene-save' ? '保存中…' : '保存场景' }}</button></div>
    </form>
  </div>
</template>
