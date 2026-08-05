<script setup lang="ts">
import { ref } from 'vue'

/**
 * Create/edit dialog for a storyboard (shot).
 *
 * The parent owns `form` and the option lists; character selection is emitted
 * rather than mutated here so the parent keeps its normalisation in one place.
 * focus() is exposed for the post-open focus the parent performs.
 */
defineProps<{
  form: any
  sceneOptions: { id: number; label: string }[]
  characterOptions: { id: number; label: string }[]
  error: string
  busy: string
}>()

const emit = defineEmits<{
  close: []
  submit: []
  toggleCharacter: [characterID: number]
}>()

const titleInput = ref<HTMLInputElement | null>(null)

defineExpose({ focus: () => titleInput.value?.focus() })
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <form
      class="modal settings-modal settings-modal-wide storyboard-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-storyboard-title"
      @keydown.esc="emit('close')"
      @submit.prevent="emit('submit')"
    >
      <h3 id="add-storyboard-title">{{ form.id ? '编辑镜头' : '添加分镜' }}</h3>
      <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
      <p class="storyboard-form-group">基础信息</p>
      <div class="field-grid">
        <div class="field"><label for="storyboard-title">分镜标题 <span class="required-mark">*</span></label><input id="storyboard-title" ref="titleInput" v-model="form.title" maxlength="200" required /></div>
        <div class="field"><label for="storyboard-duration">时长（秒）</label><input id="storyboard-duration" v-model.number="form.duration" type="number" min="1" max="3600" /></div>
        <div class="field settings-span">
          <label for="storyboard-scene">所属场景</label>
          <select id="storyboard-scene" v-model.number="form.scene_id">
            <option :value="0">不绑定场景</option>
            <option v-for="option in sceneOptions" :key="option.id" :value="option.id">{{ option.label }}</option>
          </select>
        </div>
      </div>
      <p class="storyboard-form-group">镜头语言</p>
      <div class="field-grid">
        <div class="field"><label for="storyboard-shot-type">景别</label><input id="storyboard-shot-type" v-model="form.shot_type" maxlength="200" placeholder="如：中景" /></div>
        <div class="field"><label for="storyboard-angle">机位角度</label><input id="storyboard-angle" v-model="form.angle" maxlength="200" placeholder="如：平视" /></div>
        <div class="field settings-span"><label for="storyboard-movement">运镜</label><input id="storyboard-movement" v-model="form.movement" maxlength="200" placeholder="如：固定、推轨" /></div>
      </div>
      <p class="storyboard-form-group">场景信息</p>
      <div class="field-grid">
        <div class="field"><label for="storyboard-location">地点</label><input id="storyboard-location" v-model="form.location" maxlength="200" placeholder="如：老城车站站台" /></div>
        <div class="field"><label for="storyboard-time">时间</label><input id="storyboard-time" v-model="form.time" maxlength="200" placeholder="如：黄昏" /></div>
        <div class="field settings-span"><label for="storyboard-atmosphere">氛围</label><input id="storyboard-atmosphere" v-model="form.atmosphere" maxlength="200" placeholder="如：克制而伤感" /></div>
      </div>
      <p class="storyboard-form-group">出场角色</p>
      <div v-if="characterOptions.length" class="storyboard-character-picker" role="group" aria-label="出场角色">
        <label v-for="option in characterOptions" :key="option.id" class="storyboard-character-option">
          <input type="checkbox" :value="option.id" :checked="form.character_ids.includes(option.id)" @change="emit('toggleCharacter', option.id)" />
          <span>{{ option.label }}</span>
        </label>
      </div>
      <p v-else class="muted sm">本集暂无角色，可先在角色阶段新增或用 AI 提取。</p>
      <p class="storyboard-form-group">镜头内容</p>
      <div class="field-grid">
        <div class="field"><label for="storyboard-action">动作</label><textarea id="storyboard-action" v-model="form.action" rows="3" maxlength="10000" /></div>
        <div class="field"><label for="storyboard-result">结果</label><textarea id="storyboard-result" v-model="form.result" rows="3" maxlength="10000" /></div>
        <div class="field settings-span"><label for="storyboard-dialogue">对白</label><textarea id="storyboard-dialogue" v-model="form.dialogue" rows="2" maxlength="10000" /></div>
        <div class="field settings-span"><label for="storyboard-description">镜头描述</label><textarea id="storyboard-description" v-model="form.description" rows="3" maxlength="10000" /></div>
      </div>
      <p class="storyboard-form-group">生成提示词</p>
      <div class="field-grid">
        <div class="field"><label for="storyboard-image-prompt">图片提示词</label><textarea id="storyboard-image-prompt" v-model="form.image_prompt" rows="3" maxlength="10000" /></div>
        <div class="field"><label for="storyboard-video-prompt">视频提示词</label><textarea id="storyboard-video-prompt" v-model="form.video_prompt" rows="3" maxlength="10000" /></div>
        <div class="field"><label for="storyboard-bgm-prompt">背景音乐提示词</label><textarea id="storyboard-bgm-prompt" v-model="form.bgm_prompt" rows="3" maxlength="10000" /></div>
        <div class="field"><label for="storyboard-sound-effect">音效</label><textarea id="storyboard-sound-effect" v-model="form.sound_effect" rows="3" maxlength="10000" /></div>
        <div class="field settings-span"><label for="storyboard-reference-images">多参考图 URL（每行一个，最多 8 张）</label><textarea id="storyboard-reference-images" v-model="form.reference_images" rows="3" maxlength="10000" /></div>
      </div>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="btn" type="button" @click="emit('close')">取消</button><button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'storyboard-save' ? '保存中…' : '保存分镜' }}</button></div>
    </form>
  </div>
</template>
