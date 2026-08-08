<script setup lang="ts">
import { ArrowLeft, ArrowRight, RefreshCw, Settings2, WandSparkles } from 'lucide-vue-next'
import { productionStageLabel, productionStatusLabel } from './workbench/labels'
import { useWorkbench } from './workbench/useWorkbench'
import StageScript from './workbench/StageScript.vue'
import StageCast from './workbench/StageCast.vue'
import StageGrid from './workbench/StageGrid.vue'
import StageBoards from './workbench/StageBoards.vue'
import StageExport from './workbench/StageExport.vue'
import PromptEditorModal from './workbench/PromptEditorModal.vue'
import CharacterLibraryImportModal from './workbench/CharacterLibraryImportModal.vue'
import CharacterFormModal from './workbench/CharacterFormModal.vue'
import SceneFormModal from './workbench/SceneFormModal.vue'
import StoryboardFormModal from './workbench/StoryboardFormModal.vue'
import ProductionConfirmModal from './workbench/ProductionConfirmModal.vue'
import EpisodeConfigModal from './workbench/EpisodeConfigModal.vue'

const {
  router, drama, episode, workbenchReady, loading, busy, loadError, refreshWarning, log, toast,
  dramaId, status, progressPct, hasActiveJobs, stageProgressLabel, stages, tab, currentStageIndex,
  currentStage, previousStage, nextStage, selectStage, handleStageKeydown, canEdit,
  currentProduction, hasActiveProduction, openProduction, cancelProduction, retryProduction,
  openEpisodeConfig, loadWorkbench, characterForm, characterFormModal, characterError, saveCharacter,
  showCharacterLibraryImport, characterLibraryQuery, characterLibraryLoading,
  filteredCharacterLibraryTemplates, characterLibraryTemplates, characterLibraryError,
  closeCharacterLibraryImport, importCharacterFromLibrary, sceneForm, sceneFormModal, sceneError,
  saveScene, storyboardForm, storyboardFormModal, storyboardSceneOptions, storyboardCharacterOptions,
  storyboardError, saveStoryboard, toggleStoryboardCharacter, promptEditor, promptEditorTemplates,
  applySelectedPromptTemplate, savePromptEditor, showProductionModal, productionServices,
  productionUsesExternalService, productionReady, productionError, startProduction, episodeConfigForm,
  configs, saveEpisodeConfig, busyLabel,
} = useWorkbench()

// These refs are bound by name to the focusable modal components below.
void characterFormModal
void sceneFormModal
void storyboardFormModal
</script>

<template>
  <div v-if="episode && workbenchReady" class="page workbench" :aria-busy="loading || !!busy">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ drama?.title }} · {{ episode.title }}</h1>
        <p class="page-desc">制作工作台 · 剧本 → 角色场景 → 宫格帧 → 分镜视频 → 合成导出</p>
      </div>
      <div class="row">
        <button v-if="canEdit" class="btn btn-primary" :disabled="hasActiveProduction || !!busy" @click="openProduction"><WandSparkles :size="16" aria-hidden="true" />自动制作</button>
        <button v-if="canEdit" class="btn" :disabled="loading || !!busy" @click="openEpisodeConfig"><Settings2 :size="16" aria-hidden="true" />生成配置</button>
        <button class="btn" :disabled="loading || !!busy" @click="loadWorkbench"><RefreshCw :size="16" :class="{ spinning: loading }" aria-hidden="true" />{{ loading ? '刷新中' : '刷新' }}</button>
        <button class="btn" @click="router.push(`/drama/${dramaId}`)"><ArrowLeft :size="16" aria-hidden="true" />返回项目</button>
      </div>
    </div>

    <div v-if="refreshWarning" class="inline-alert" role="alert">
      <div><strong>部分内容暂未更新</strong><span>{{ refreshWarning }}</span></div>
      <button class="btn" type="button" @click="loadWorkbench">重试加载</button>
    </div>

    <section v-if="currentProduction" class="automation-status" aria-label="自动制作状态">
      <div class="automation-main">
        <div class="automation-heading">
          <span class="automation-mark" :class="currentProduction.status"></span>
          <div><strong>{{ productionStageLabel(currentProduction.stage) }}</strong><span>{{ productionStatusLabel(currentProduction.status) }} · 第 {{ currentProduction.attempt }} 次</span></div>
        </div>
        <div class="automation-progress"><div class="progress-bar" role="progressbar" :aria-valuenow="currentProduction.progress || 0" aria-valuemin="0" aria-valuemax="100" aria-label="自动制作进度"><i :style="`width: ${currentProduction.progress || 0}%`"></i></div><strong>{{ currentProduction.progress || 0 }}%</strong></div>
      </div>
      <div class="automation-footer">
        <span :class="{ 'job-error': currentProduction.last_error }">{{ currentProduction.last_error || currentProduction.status_message || '等待任务调度' }}</span>
        <div class="mini-actions">
          <button v-if="hasActiveProduction && canEdit" class="btn btn-danger" :disabled="!!busy" @click="cancelProduction">取消自动制作</button>
          <button v-if="['failed','canceled'].includes(currentProduction.status) && canEdit" class="btn" :disabled="!!busy || currentProduction.attempt >= currentProduction.max_attempts" @click="retryProduction">重试自动制作</button>
          <button class="btn" @click="router.push('/jobs')">查看任务</button>
        </div>
      </div>
    </section>

    <section class="production-overview" aria-label="制作进度">
      <div class="production-summary">
        <div class="production-stats" aria-label="资产统计">
          <span class="stat-chip" :class="status?.has_script ? 'done' : 'pending'">剧本 <strong>{{ status?.has_script ? '✓' : '—' }}</strong></span>
          <span class="stat-chip" :class="(status?.characters || 0) > 0 ? 'done' : 'pending'">角色 <strong>{{ status?.characters || 0 }}</strong></span>
          <span class="stat-chip" :class="(status?.scenes || 0) > 0 ? 'done' : 'pending'">场景 <strong>{{ status?.scenes || 0 }}</strong></span>
          <span class="stat-chip" :class="(status?.storyboards || 0) > 0 ? 'done' : 'pending'">分镜 <strong>{{ status?.storyboards || 0 }}</strong></span>
          <span class="stat-chip" :class="(status?.with_video || 0) > 0 ? 'done' : 'pending'">视频 <strong>{{ status?.with_video || 0 }}</strong></span>
          <span class="stat-chip" :class="(status?.with_tts || 0) > 0 ? 'done' : 'pending'">配音 <strong>{{ status?.with_tts || 0 }}</strong></span>
          <span class="stat-chip" :class="(status?.composed || 0) > 0 || episode?.video_url ? 'done' : 'pending'">合成 <strong>{{ status?.composed || 0 }}</strong></span>
        </div>
        <div class="production-progress">
          <div class="production-progress-label">
            <span>制作进度</span>
            <strong>{{ progressPct }}%</strong>
          </div>
          <div
            class="progress-bar"
            :class="{ active: hasActiveProduction || hasActiveJobs }"
            role="progressbar"
            :aria-valuenow="progressPct"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="stageProgressLabel"
          >
            <i :style="`width: ${progressPct}%`"></i>
          </div>
          <p class="production-progress-meta">{{ stageProgressLabel }}</p>
        </div>
      </div>

      <div class="stage-tabs-shell">
        <div class="stage-tabs" role="tablist" aria-label="制作阶段">
          <button
            v-for="(stage, index) in stages"
            :key="stage.id"
            class="stage-tab"
            :class="{ active: tab === stage.id, complete: stage.complete }"
            role="tab"
            :id="`workbench-stage-${stage.id}`"
            :aria-controls="`workbench-stage-panel-${stage.id}`"
            :aria-label="`${stage.label}${stage.complete ? '，已完成' : ''}`"
            :aria-selected="tab === stage.id"
            :aria-current="tab === stage.id ? 'step' : undefined"
            :tabindex="tab === stage.id ? 0 : -1"
            :title="`${stage.label} · ${stage.detail}${stage.complete ? ' · 已完成' : ''}`"
            @click="selectStage(stage.id)"
            @keydown="handleStageKeydown($event, index)"
          >
            <span class="stage-index" aria-hidden="true">{{ stage.complete ? '✓' : index + 1 }}</span>
            <span class="stage-copy">
              <strong>{{ stage.label }}</strong>
              <small>{{ stage.detail }}</small>
            </span>
          </button>
        </div>
      </div>
    </section>

    <div class="wb-shell">
      <div class="wb-main">
        <div class="stage-commandbar">
          <div class="stage-commandbar-copy">
            <span>阶段 {{ currentStageIndex + 1 }} / {{ stages.length }}</span>
            <strong>{{ currentStage.label }}</strong>
            <small>{{ currentStage.detail }}</small>
          </div>
          <div class="stage-commandbar-actions">
            <button
              v-if="previousStage"
              class="btn stage-prev"
              type="button"
              :aria-label="`返回上一阶段：${previousStage.label}`"
              @click="selectStage(previousStage.id)"
            >
              <ArrowLeft :size="15" aria-hidden="true" />{{ previousStage.label }}
            </button>
            <button
              v-if="nextStage"
              class="btn stage-next"
              type="button"
              @click="selectStage(nextStage.id)"
            >
              下一步：{{ nextStage.label }}<ArrowRight :size="15" aria-hidden="true" />
            </button>
            <span v-else class="stage-complete-flag" role="status">全部阶段已走完</span>
          </div>
        </div>
        <StageScript v-if="tab === 'script'" />
        <StageCast v-else-if="tab === 'cast'" />
        <StageGrid v-else-if="tab === 'grid'" />
        <StageBoards v-else-if="tab === 'boards'" />
        <StageExport v-else-if="tab === 'export'" />
        <div v-if="log" class="panel mt-16">
          <h3 class="section-title">Agent / 任务输出</h3>
          <pre class="log-box">{{ log }}</pre>
        </div>
      </div>
    </div>

    <CharacterFormModal
      v-if="characterForm"
      ref="characterFormModal"
      :form="characterForm"
      :error="characterError"
      :busy="busy"
      @close="characterForm = null"
      @submit="saveCharacter"
    />

    <CharacterLibraryImportModal
      v-if="showCharacterLibraryImport"
      v-model:query="characterLibraryQuery"
      :loading="characterLibraryLoading"
      :templates="filteredCharacterLibraryTemplates"
      :total-count="characterLibraryTemplates.length"
      :error="characterLibraryError"
      :busy="busy"
      @close="closeCharacterLibraryImport"
      @import="importCharacterFromLibrary"
    />

    <SceneFormModal
      v-if="sceneForm"
      ref="sceneFormModal"
      :form="sceneForm"
      :error="sceneError"
      :busy="busy"
      @close="sceneForm = null"
      @submit="saveScene"
    />

    <StoryboardFormModal
      v-if="storyboardForm"
      ref="storyboardFormModal"
      :form="storyboardForm"
      :scene-options="storyboardSceneOptions"
      :character-options="storyboardCharacterOptions"
      :error="storyboardError"
      :busy="busy"
      @close="storyboardForm = null"
      @submit="saveStoryboard"
      @toggle-character="toggleStoryboardCharacter"
    />

    <PromptEditorModal
      v-if="promptEditor"
      :editor="promptEditor"
      :templates="promptEditorTemplates"
      :busy="busy"
      @close="promptEditor = null"
      @apply="applySelectedPromptTemplate"
      @save="savePromptEditor"
    />

    <ProductionConfirmModal
      v-if="showProductionModal"
      :services="productionServices"
      :uses-external-service="productionUsesExternalService"
      :ready="productionReady"
      :error="productionError"
      :busy="busy"
      @close="showProductionModal = false"
      @start="startProduction"
    />

    <EpisodeConfigModal
      v-if="episodeConfigForm"
      :form="episodeConfigForm"
      :configs="configs"
      :busy="busy"
      @close="episodeConfigForm = null"
      @save="saveEpisodeConfig"
    />

    <div v-if="toast" class="toast" role="status">{{ toast }}</div>
    <div v-if="busy" class="toast busy" role="status" aria-live="polite"><span class="busy-indicator" aria-hidden="true"></span>{{ busyLabel }}</div>
  </div>
  <div v-else-if="loading" class="page">
    <div class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>正在加载本集</strong>
        <p class="muted">同步剧本、角色、分镜与任务状态…</p>
      </div>
    </div>
  </div>
  <div v-else class="page">
    <div class="panel load-error" role="alert">
      <h2>无法加载本集</h2>
      <p class="muted">{{ loadError || '剧集不存在' }}</p>
      <button class="btn btn-primary" type="button" @click="loadWorkbench">重新加载</button>
    </div>
  </div>
</template>
