<script setup lang="ts">
/**
 * Picker for importing a character-library template into the current episode.
 *
 * `templates` is the already-filtered list; `totalCount` is the unfiltered
 * size, which the empty state needs to tell "no matches" apart from "library
 * is empty".
 */
defineProps<{
  query: string
  loading: boolean
  templates: any[]
  totalCount: number
  error: string
  busy: string
}>()

const emit = defineEmits<{
  'update:query': [value: string]
  close: []
  import: [template: any]
}>()
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <div
      class="modal settings-modal settings-modal-wide"
      role="dialog"
      aria-modal="true"
      aria-labelledby="import-character-library-title"
      @keydown.esc="emit('close')"
    >
      <h3 id="import-character-library-title">从角色库导入</h3>
      <p class="muted character-import-note">导入后会创建本集独立角色副本，并链接到当前剧集。</p>
      <label class="library-search">
        <span class="sr-only">搜索角色模板</span>
        <input
          :value="query"
          type="search"
          aria-label="搜索角色模板"
          placeholder="搜索名称、定位或音色"
          @input="emit('update:query', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <div v-if="loading" class="empty">正在加载角色库…</div>
      <div v-else-if="templates.length" class="list character-library-picker">
        <div v-for="template in templates" :key="template.id" class="list-item">
          <div class="row between">
            <div class="stack">
              <h4>{{ template.name }} <span class="muted">{{ template.role || '未设置定位' }}</span></h4>
              <p class="muted sm">{{ template.appearance || template.personality || '暂无设定' }}</p>
              <p class="muted sm mt-6">音色：{{ template.voice_style || '未绑定' }}</p>
            </div>
            <div class="row column-end">
              <img v-if="template.image_url" class="thumb" :src="template.image_url" :alt="`${template.name} 角色形象`" />
              <button class="btn btn-primary" type="button" :disabled="!!busy" @click="emit('import', template)">导入本集</button>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="empty">{{ totalCount ? '没有匹配的角色模板' : '角色库还是空的，可先把现有角色存入角色库' }}</div>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="btn" type="button" @click="emit('close')">关闭</button></div>
    </div>
  </div>
</template>
