<script setup lang="ts">
import { useWorkbenchContext } from './context'

const context = useWorkbenchContext()
</script>

<template>
  <div id="workbench-stage-panel-grid" class="panel wb-stage-panel" role="tabpanel" aria-labelledby="workbench-stage-grid">
    <div v-if="context.canEdit" class="toolbar">
      <div class="seg" role="group" aria-label="宫格生成模式">
        <button :disabled="!!context.busy" :class="{ active: context.gridMode === 'first_frame' }" :aria-pressed="context.gridMode === 'first_frame'" @click="context.selectGridMode('first_frame')">首帧宫格</button>
        <button :disabled="!!context.busy" :class="{ active: context.gridMode === 'first_last' }" :aria-pressed="context.gridMode === 'first_last'" @click="context.selectGridMode('first_last')">首尾参考</button>
        <button :disabled="!!context.busy" :class="{ active: context.gridMode === 'multi_ref' }" :aria-pressed="context.gridMode === 'multi_ref'" @click="context.selectGridMode('multi_ref')">多参一致</button>
      </div>
      <label class="dim-field">行
        <input type="number" min="1" max="4" :value="context.gridRows" :disabled="!!context.busy" @change="context.updateGridDimension('rows', Number(($event.target as HTMLInputElement).value))" />
      </label>
      <label class="dim-field">列
        <input type="number" min="1" max="4" :value="context.gridCols" :disabled="!!context.busy" @change="context.updateGridDimension('cols', Number(($event.target as HTMLInputElement).value))" />
      </label>
      <button class="btn" :disabled="!!context.busy" @click="context.buildGridPrompt">生成提示词</button>
      <button class="btn" :disabled="!!context.busy" @click="context.openGridPromptEditor">套用提示词模板</button>
      <button class="btn btn-primary" :disabled="!!context.busy || !!context.gridSelectionError" :title="context.gridSelectionError || undefined" @click="context.generateGrid">生成宫格图</button>
      <button class="btn" :disabled="!!context.busy || !context.gridImage || !!context.gridCells.length" @click="context.splitGrid">{{ context.gridCells.length ? '已切分，可在下方重新分配' : '切分写回分镜' }}</button>
      <button class="btn" :disabled="!!context.busy" @click="context.runAgent('grid_prompt_generator', '请为本集镜头生成宫格首帧提示词')">Agent 提示词</button>
      <span v-if="context.gridSelectionError" class="muted grid-selection-hint" role="status">{{ context.gridSelectionError }}</span>
    </div>
    <div class="field">
      <label for="grid-prompt">宫格提示词</label>
      <textarea id="grid-prompt" v-model="context.gridPrompt" rows="8" :readonly="!context.canEdit" placeholder="可先点击「生成提示词」" />
    </div>
    <div class="split-2">
      <div>
        <div class="muted sm section-kicker">预览</div>
        <img v-if="context.gridImage" class="grid-preview" :src="context.gridImage" alt="宫格图预览" />
        <div v-else class="empty surface-empty wb-empty"><strong>尚未生成宫格图</strong><span class="muted">选择宫格模式与镜头，确认提示词后即可生成预览。</span></div>
        <div v-if="context.gridCells.length" class="cell-grid mt-12">
          <div v-if="!context.gridCellsVerified" class="inline-alert grid-cell-legacy" role="status"><div><strong>历史切片仅供查看</strong><span>这份历史生成于安全归属记录之前。请生成新宫格并重新切分后再分配。</span></div></div>
          <div v-for="(url, index) in context.gridCells" :key="`${context.gridHistoryId || 'draft'}-${index}`" class="grid-cell-card" role="group" :aria-label="`宫格切片 ${index + 1}`">
            <div class="grid-cell-visual"><span>#{{ index + 1 }}</span><img :src="url" :alt="`宫格切片 ${index + 1}`" /></div>
            <p class="grid-cell-assignment">{{ context.gridAssignmentLabel(index) }}</p>
            <p v-if="context.gridCellErrors[index]" class="grid-cell-error" role="alert">{{ context.gridCellErrors[index] }}</p>
            <div v-if="context.canEdit" class="grid-cell-controls">
              <label><span>目标镜头</span><select :value="context.gridCellTarget(index).storyboard_id" aria-label="目标镜头" :disabled="!context.gridCellsVerified || context.assigningGridCell !== null" @change="context.updateGridCellTarget(index, { storyboard_id: Number(($event.target as HTMLSelectElement).value) })"><option :value="0">请选择</option><option v-for="storyboard in context.storyboards" :key="storyboard.id" :value="storyboard.id">#{{ storyboard.storyboard_number }} {{ storyboard.title || '镜头' }}</option></select></label>
              <label><span>写入位置</span><select :value="context.gridCellTarget(index).frame_type" aria-label="写入位置" :disabled="!context.gridCellsVerified || context.assigningGridCell !== null" @change="context.updateGridCellTarget(index, { frame_type: ($event.target as HTMLSelectElement).value })"><option value="first_frame">首帧</option><option value="last_frame">尾帧</option><option value="composed">分镜板</option></select></label>
              <button class="btn" type="button" :disabled="context.assigningGridCell !== null || !context.gridHistoryId || !context.gridCellsVerified || !context.gridCellTarget(index).storyboard_id" @click="context.assignGridCell(index)">{{ context.assigningGridCell === index ? '分配中…' : '重新分配' }}</button>
            </div>
          </div>
        </div>
      </div>
      <div>
        <div class="muted sm section-kicker">写入分镜（勾选）</div>
        <div class="list">
          <label v-for="storyboard in context.storyboards" :key="storyboard.id" class="list-item shot-pick">
            <input v-if="context.canEdit" type="checkbox" :checked="context.selectedShotIds.includes(storyboard.id)" @change="context.toggleShot(storyboard.id)" />
            <span class="grow">#{{ storyboard.storyboard_number }} {{ storyboard.title || '镜头' }}</span>
            <img v-if="storyboard.first_frame_image" class="thumb sm" :src="storyboard.first_frame_image" :alt="`镜头 ${storyboard.storyboard_number} 首帧`" />
          </label>
          <div v-if="!context.storyboards.length" class="empty surface-empty wb-empty"><strong>还没有可选镜头</strong><span class="muted">请先到「分镜与视频」阶段拆解剧本。</span></div>
        </div>
        <div class="mt-12">
          <div class="muted sm section-kicker tight">历史</div>
          <div class="list">
            <div v-for="history in context.gridHist" :key="history.id" class="list-item compact">
              #{{ history.id }} · {{ history.mode }} · {{ history.rows }}x{{ history.cols }} · {{ history.status }}
              <button v-if="history.image_url" class="btn mt-6" :disabled="!!context.busy || context.assigningGridCell !== null" :aria-label="`载入宫格 #${history.id}`" @click="context.loadGridHistory(history)">载入</button>
            </div>
            <div v-if="!context.gridHist.length" class="muted">暂无历史</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
