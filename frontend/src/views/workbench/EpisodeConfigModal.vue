<script setup lang="ts">
import { computed } from 'vue'

/**
 * Binds this episode's image / video / audio generation services.
 *
 * The parent passes the full AI config list; the per-type options are derived
 * here so the template stays declarative.
 */
const props = defineProps<{
  form: any
  configs: any[]
  busy: string
}>()

const emit = defineEmits<{
  close: []
  save: []
}>()

const imageConfigs = computed(() => props.configs.filter((row) => row.service_type === 'image'))
const videoConfigs = computed(() => props.configs.filter((row) => row.service_type === 'video'))
const audioConfigs = computed(() => props.configs.filter((row) => row.service_type === 'audio'))
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <div class="modal" role="dialog" aria-modal="true" aria-labelledby="episode-config-title">
      <h3 id="episode-config-title">剧集生成配置</h3>
      <div class="field">
        <label for="episode-image-config">图片配置</label>
        <select id="episode-image-config" v-model.number="form.image_config_id">
          <option :value="0">请选择</option>
          <option v-for="item in imageConfigs" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
      </div>
      <div class="field">
        <label for="episode-video-config">视频配置</label>
        <select id="episode-video-config" v-model.number="form.video_config_id">
          <option :value="0">请选择</option>
          <option v-for="item in videoConfigs" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
      </div>
      <div class="field">
        <label for="episode-audio-config">音频配置</label>
        <select id="episode-audio-config" v-model.number="form.audio_config_id">
          <option :value="0">请选择</option>
          <option v-for="item in audioConfigs" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
      </div>
      <div class="modal-actions">
        <button class="btn" @click="emit('close')">取消</button>
        <button class="btn btn-primary" :disabled="!!busy" @click="emit('save')">保存配置</button>
      </div>
    </div>
  </div>
</template>
