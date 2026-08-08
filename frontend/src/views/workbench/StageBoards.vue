<script setup lang="ts">
import { Plus } from 'lucide-vue-next'
import { useWorkbenchContext } from './context'

const context = useWorkbenchContext()
</script>

<template>
  <div id="workbench-stage-panel-boards" class="panel wb-stage-panel" role="tabpanel" aria-labelledby="workbench-stage-boards">
    <div v-if="context.canEdit" class="toolbar">
      <button class="btn btn-primary" :disabled="!!context.busy" @click="context.runAgent('storyboard_breaker', '请根据当前剧本完整拆解分镜并保存')">AI 拆解分镜</button>
      <button class="btn" :disabled="!!context.busy" @click="context.addStoryboard"><Plus :size="15" aria-hidden="true" />添加分镜</button>
      <button class="btn" :disabled="!!context.busy || !context.selectedShotIds.length" @click="context.batchFrames('first_frame')">批量首帧</button>
      <button class="btn" :disabled="!!context.busy || !context.selectedShotIds.length" @click="context.batchFrames('last_frame')">批量尾帧</button>
      <button class="btn" :disabled="!!context.busy || !context.selectedShotIds.length" @click="context.batchFrames('composed')">批量分镜板</button>
      <button class="btn" :disabled="!!context.busy || !context.selectedShotIds.length" @click="context.batchVideos">批量视频</button>
      <button class="btn" :disabled="!!context.busy || !context.selectedShotIds.length" @click="context.batchTTS">批量配音</button>
    </div>
    <div v-if="context.storyboards.length" class="storyboard-workspace">
      <aside class="storyboard-rail">
        <div class="storyboard-rail-head"><strong>镜头列表</strong><span>{{ context.storyboards.length }} 个镜头</span></div>
        <div class="storyboard-rail-list" role="list" aria-label="镜头列表">
          <div v-for="storyboard in context.storyboards" :key="storyboard.id" class="storyboard-rail-row" role="listitem">
            <button
              class="storyboard-rail-item"
              type="button"
              :class="{ active: context.selectedStoryboard?.id === storyboard.id }"
              :aria-current="context.selectedStoryboard?.id === storyboard.id ? 'true' : undefined"
              :aria-label="`镜头 ${storyboard.storyboard_number} ${storyboard.title || ''}`"
              @click="context.selectedStoryboardId = storyboard.id"
            >
              <span class="storyboard-rail-index"><i class="status-dot" :class="context.shotStatusDot(storyboard)"></i>#{{ storyboard.storyboard_number }}</span>
              <strong>{{ storyboard.title || '未命名镜头' }}</strong>
              <small>{{ storyboard.duration }}s · {{ storyboard.shot_type || '未设置景别' }}</small>
            </button>
            <label v-if="context.canEdit" class="storyboard-batch-check" :title="`选择镜头 ${storyboard.storyboard_number}`">
              <input type="checkbox" :checked="context.selectedShotIds.includes(storyboard.id)" :aria-label="`选择镜头 ${storyboard.storyboard_number}`" @change="context.toggleShot(storyboard.id)" />
            </label>
          </div>
        </div>
      </aside>

      <section v-if="context.selectedStoryboard" class="storyboard-inspector" role="region" aria-label="当前镜头">
        <div class="storyboard-inspector-head">
          <div><span class="muted">镜头 {{ context.selectedStoryboard.storyboard_number }}</span><h3>#{{ context.selectedStoryboard.storyboard_number }} {{ context.selectedStoryboard.title || '镜头' }}</h3></div>
          <span class="job-status" :class="context.selectedStoryboard.status">{{ context.selectedStoryboard.duration }}s · {{ context.storyboardStatusLabel(context.selectedStoryboard.status) }}</span>
        </div>
        <div class="storyboard-copy-grid">
          <div><span>镜头参数</span><p>{{ context.selectedStoryboard.shot_type || '未设置' }} / {{ context.selectedStoryboard.angle || '未设置' }} / {{ context.selectedStoryboard.movement || '未设置' }}</p></div>
          <div><span>镜头描述</span><p>{{ context.selectedStoryboard.description || context.selectedStoryboard.action || '暂无描述' }}</p></div>
          <div class="storyboard-dialogue"><span>对白</span><p>{{ context.selectedStoryboard.dialogue || '（无）' }}</p></div>
        </div>
        <dl v-if="context.selectedStoryboardFacts.length" class="storyboard-fact-row">
          <div v-for="fact in context.selectedStoryboardFacts" :key="fact.label"><dt>{{ fact.label }}</dt><dd>{{ fact.value }}</dd></div>
        </dl>
        <div class="storyboard-media-grid">
          <div class="storyboard-media-cell"><span>首帧</span><img v-if="context.selectedStoryboard.first_frame_image" :src="context.selectedStoryboard.first_frame_image" :alt="`镜头 ${context.selectedStoryboard.storyboard_number} 首帧`" /><div v-else class="media-placeholder">未生成</div></div>
          <div class="storyboard-media-cell"><span>尾帧</span><img v-if="context.selectedStoryboard.last_frame_image" :src="context.selectedStoryboard.last_frame_image" :alt="`镜头 ${context.selectedStoryboard.storyboard_number} 尾帧`" /><div v-else class="media-placeholder">未生成</div></div>
          <div class="storyboard-media-cell"><span>分镜板</span><img v-if="context.selectedStoryboard.composed_image" :src="context.selectedStoryboard.composed_image" :alt="`镜头 ${context.selectedStoryboard.storyboard_number} 分镜板`" /><div v-else class="media-placeholder">未生成</div></div>
          <div class="storyboard-media-cell storyboard-video-cell"><span>视频</span><video v-if="context.selectedStoryboard.video_url" :src="context.selectedStoryboard.video_url" controls /><div v-else class="media-placeholder">未生成</div></div>
        </div>
        <audio v-if="context.selectedStoryboard.tts_audio_url" class="storyboard-audio" :src="context.selectedStoryboard.tts_audio_url" controls />
        <div v-if="context.canEdit" class="storyboard-primary-actions">
          <button class="btn" :disabled="!!context.busy" @click="context.genFrame(context.selectedStoryboard, 'first_frame')">生成首帧</button>
          <button class="btn" :disabled="!!context.busy" @click="context.genFrame(context.selectedStoryboard, 'last_frame')">生成尾帧</button>
          <button class="btn" :disabled="!!context.busy" @click="context.genFrame(context.selectedStoryboard, 'composed')">生成分镜板</button>
          <button class="btn btn-primary" :disabled="!!context.busy" @click="context.genVideo(context.selectedStoryboard)">生成视频</button>
          <button class="btn" :disabled="!!context.busy" @click="context.genTTS(context.selectedStoryboard)">生成配音</button>
          <button class="btn" :disabled="!!context.busy" @click="context.composeShot(context.selectedStoryboard)">合成镜头</button>
        </div>
        <div v-if="context.canEdit" class="storyboard-secondary-actions">
          <button class="btn btn-ghost" :disabled="!!context.busy" @click="context.editStoryboard(context.selectedStoryboard)">编辑镜头</button>
          <button class="btn btn-ghost" :disabled="!!context.busy" @click="context.openPromptEditor(context.selectedStoryboard, 'image_prompt', '图片提示词')">改图词</button>
          <button class="btn btn-ghost" :disabled="!!context.busy" @click="context.openPromptEditor(context.selectedStoryboard, 'video_prompt', '视频提示词')">改视频词</button>
          <button class="btn btn-ghost" :disabled="!!context.busy" @click="context.openPromptEditor(context.selectedStoryboard, 'dialogue', '对白')">改对白</button>
          <button class="btn btn-ghost" :disabled="!!context.busy || context.storyboards.findIndex((item) => item.id === context.selectedStoryboard?.id) === 0" @click="context.moveStoryboard(context.selectedStoryboard, 'up')">上移</button>
          <button class="btn btn-ghost" :disabled="!!context.busy || context.storyboards.findIndex((item) => item.id === context.selectedStoryboard?.id) === context.storyboards.length - 1" @click="context.moveStoryboard(context.selectedStoryboard, 'down')">下移</button>
          <button class="btn btn-ghost" :disabled="!!context.busy" @click="context.copyStoryboard(context.selectedStoryboard)">复制镜头</button>
          <button class="btn btn-danger" :disabled="!!context.busy" @click="context.removeStoryboard(context.selectedStoryboard)">删除镜头</button>
        </div>
      </section>
    </div>
    <div v-if="!context.storyboards.length" class="empty surface-empty wb-empty mt-12"><strong>尚未拆解分镜</strong><span class="muted">使用 AI 从剧本生成镜头列表，或手动添加第一个分镜。</span></div>
  </div>
</template>
