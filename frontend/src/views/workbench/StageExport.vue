<script setup lang="ts">
import { safeMediaHref } from '../../utils/mediaUrl'
import { useWorkbenchContext } from './context'

const context = useWorkbenchContext()
</script>

<template>
  <div id="workbench-stage-panel-export" class="panel wb-stage-panel" role="tabpanel" aria-labelledby="workbench-stage-export">
    <div v-if="context.canEdit" class="toolbar">
      <button class="btn" :disabled="!!context.busy" @click="context.composeAll">批量合成镜头</button>
      <button class="btn btn-primary" :disabled="!!context.busy" @click="context.mergeAll">拼接导出成片</button>
    </div>
    <p class="muted">将已生成视频与配音排队合成为镜头，再拼接为整集成片。任务状态可在右侧查看。</p>
    <video v-if="context.episode?.video_url" class="media export" :src="context.episode.video_url" controls />
    <div v-else class="empty surface-empty wb-empty mt-12"><strong>尚未导出成片</strong><span class="muted">先完成镜头视频与配音，再批量合成并拼接整集。</span></div>
    <div class="export-status">
      <h3>镜头合成状态</h3>
      <div class="list">
        <div v-for="storyboard in context.storyboards" :key="`c${storyboard.id}`" class="list-item">
          <div class="row between"><span>#{{ storyboard.storyboard_number }} {{ storyboard.title || '镜头' }}</span><span class="muted">{{ storyboard.composed_video_url ? '已合成' : (storyboard.video_url ? '有视频' : '缺视频') }} · {{ storyboard.tts_audio_url ? '有配音' : '无配音' }}</span></div>
          <video v-if="storyboard.composed_video_url" class="media shot" :src="storyboard.composed_video_url" controls />
        </div>
      </div>
    </div>
    <div class="split-2 mt-16">
      <div>
        <h3 class="section-title">素材库</h3>
        <div class="list">
          <div v-for="asset in context.assets" :key="asset.id" class="list-item">
            <div class="row between center"><span>{{ asset.name }}</span><span class="muted">{{ asset.type }} · {{ asset.category || '未分类' }}</span></div>
            <img v-if="asset.mime_type?.startsWith('image/')" class="thumb mt-8" :src="asset.url" :alt="asset.name" />
            <a v-if="safeMediaHref(asset.url)" :href="safeMediaHref(asset.url)" target="_blank" rel="noopener noreferrer" class="muted block-link">打开素材</a>
            <div v-if="context.canEdit && (asset.type === 'image' || asset.mime_type?.startsWith('image/'))" class="row mt-8 center">
              <select v-model.number="context.assetTargetShot[asset.id]" aria-label="目标分镜"><option :value="undefined">选择分镜</option><option v-for="storyboard in context.storyboards" :key="storyboard.id" :value="storyboard.id">#{{ storyboard.storyboard_number }} {{ storyboard.title || '镜头' }}</option></select>
              <select v-model="context.assetTargetFrame[asset.id]" aria-label="目标帧"><option value="first_frame">首帧</option><option value="last_frame">尾帧</option><option value="composed">分镜板</option></select>
              <button class="btn" @click="context.applyAsset(asset)">复用到分镜</button>
            </div>
          </div>
          <div v-if="!context.assets.length" class="empty wb-compact-empty">本集暂无统一素材记录</div>
        </div>
      </div>
      <div>
        <h3 class="section-title">任务状态</h3>
        <div class="list">
          <div v-for="job in context.jobs" :key="job.id" class="list-item">
            <div class="row between center"><span>#{{ job.id }} {{ job.kind }}</span><span class="muted">{{ job.status }}</span></div>
            <div class="muted sm mt-6">进度 {{ job.progress || 0 }}% · {{ job.last_error || '无错误' }}</div>
            <button v-if="context.canEdit && !['succeeded','failed','canceled'].includes(job.status)" class="btn btn-danger mt-8" :disabled="context.pendingJobActionIDs.includes(job.id)" @click="context.cancelJob(job)">{{ context.pendingJobActionIDs.includes(job.id) ? '取消中…' : '取消任务' }}</button>
            <button v-if="context.canEdit && ['failed','canceled'].includes(job.status)" class="btn mt-8" :disabled="context.pendingJobActionIDs.includes(job.id)" @click="context.retryJob(job)">{{ context.pendingJobActionIDs.includes(job.id) ? '重试中…' : '重试任务' }}</button>
          </div>
          <div v-if="!context.jobs.length" class="empty wb-compact-empty">暂无任务记录</div>
        </div>
      </div>
    </div>
  </div>
</template>
