<script setup lang="ts">
import { ref } from 'vue'

/**
 * Create/edit dialog for an episode character.
 *
 * `form` is the parent's live form object; fields bind straight to it. The
 * parent focuses the name input after opening, so focus() is exposed rather
 * than reaching for a child ref.
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

const nameInput = ref<HTMLInputElement | null>(null)

defineExpose({ focus: () => nameInput.value?.focus() })
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <form
      class="modal settings-modal settings-modal-wide"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="form.id ? 'edit-character-title' : 'add-character-title'"
      @keydown.esc="emit('close')"
      @submit.prevent="emit('submit')"
    >
      <h3 :id="form.id ? 'edit-character-title' : 'add-character-title'">{{ form.id ? '编辑角色' : '添加角色' }}</h3>
      <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
      <div class="field-grid">
        <div class="field"><label for="workbench-character-name">角色名 <span class="required-mark">*</span></label><input id="workbench-character-name" ref="nameInput" v-model="form.name" maxlength="120" required /></div>
        <div class="field"><label for="workbench-character-role">定位</label><input id="workbench-character-role" v-model="form.role" maxlength="120" /></div>
        <div class="field"><label for="workbench-character-appearance">外貌</label><textarea id="workbench-character-appearance" v-model="form.appearance" rows="3" maxlength="4000" /></div>
        <div class="field"><label for="workbench-character-personality">性格</label><textarea id="workbench-character-personality" v-model="form.personality" rows="3" maxlength="4000" /></div>
        <div class="field settings-span"><label for="workbench-character-description">说明</label><textarea id="workbench-character-description" v-model="form.description" rows="3" maxlength="4000" /></div>
      </div>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="btn" type="button" @click="emit('close')">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'character-save' ? '保存中…' : '保存角色' }}</button></div>
    </form>
  </div>
</template>
