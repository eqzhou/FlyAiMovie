<script setup lang="ts">
import { useWorkbenchContext } from './context'

const context = useWorkbenchContext()
</script>

<template>
  <div id="workbench-stage-panel-script" class="panel wb-stage-panel" role="tabpanel" aria-labelledby="workbench-stage-script">
    <div v-if="context.canEdit" class="toolbar">
      <button class="btn btn-primary" :disabled="!!context.busy" @click="context.saveContent">保存原文</button>
      <button class="btn" :disabled="!!context.busy" @click="context.runAgent('script_rewriter', '请将当前集内容改写为格式化剧本并保存')">AI 改写剧本</button>
    </div>
    <div class="split-2">
      <div class="field">
        <label>原始内容 / 大纲</label>
        <textarea v-model="context.rawContent" rows="18" :readonly="!context.canEdit" placeholder="粘贴小说、大纲或分场内容…" />
      </div>
      <div class="field">
        <label>格式化剧本</label>
        <textarea :value="context.episode?.script_content || ''" rows="18" readonly placeholder="AI 改写后显示在此" />
      </div>
    </div>
  </div>
</template>
