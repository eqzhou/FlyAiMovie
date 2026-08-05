<script setup lang="ts">
/**
 * Prompt editing dialog for a storyboard field or the grid prompt.
 *
 * `editor` is the live editor object owned by the parent. The template select
 * and textarea bind straight to it, matching the previous inline markup: the
 * parent already reads editor.value / editor.template_id back when applying a
 * template or saving.
 */
defineProps<{
  editor: any
  templates: any[]
  busy: string
}>()

const emit = defineEmits<{
  close: []
  apply: []
  save: []
}>()
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <div
      class="modal settings-modal settings-modal-wide prompt-editor-modal"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="`prompt-editor-${editor.field}`"
      @keydown.esc="emit('close')"
    >
      <h3 :id="`prompt-editor-${editor.field}`">编辑{{ editor.label }}</h3>
      <div v-if="editor.category && templates.length" class="prompt-editor-template-row">
        <div class="field">
          <label for="workbench-prompt-template">提示词模板</label>
          <select id="workbench-prompt-template" v-model.number="editor.template_id">
            <option v-for="template in templates" :key="template.id" :value="template.id">{{ template.name }} · v{{ template.version }}</option>
          </select>
        </div>
        <button class="btn" type="button" :disabled="!!busy || !templates.length" @click="emit('apply')">套用模板</button>
      </div>
      <div class="field">
        <label for="workbench-prompt-value">{{ editor.label }}</label>
        <textarea id="workbench-prompt-value" v-model="editor.value" rows="10" maxlength="20000" />
      </div>
      <p v-if="editor.error" class="form-error" role="alert">{{ editor.error }}</p>
      <div class="modal-actions">
        <button class="btn" type="button" @click="emit('close')">取消</button>
        <button class="btn btn-primary" type="button" :disabled="!!busy" @click="emit('save')">{{ editor.target === 'grid' ? '应用' : '保存' }}</button>
      </div>
    </div>
  </div>
</template>
