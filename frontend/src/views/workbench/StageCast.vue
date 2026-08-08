<script setup lang="ts">
import { useWorkbenchContext } from './context'

const context = useWorkbenchContext()
</script>

<template>
  <div id="workbench-stage-panel-cast" class="cast-layout" role="tabpanel" aria-labelledby="workbench-stage-cast">
    <div class="panel">
      <div v-if="context.canEdit" class="toolbar">
        <button class="btn btn-primary" :disabled="!!context.busy" @click="context.runAgent('extractor', '请提取本集角色与场景并去重保存')">AI 提取</button>
        <button class="btn" :disabled="!!context.busy" @click="context.runAgent('voice_assigner', '请为所有角色分配音色')">AI 分配音色</button>
        <button class="btn" :disabled="!!context.busy || !context.characters.length" @click="context.batchCharImages">批量角色图</button>
        <button class="btn" :disabled="!!context.busy" @click="context.openCharacterLibraryImport">从角色库导入</button>
        <button class="btn" :disabled="!!context.busy" @click="context.editCharacter()">添加角色</button>
      </div>
      <h3 class="section-title">角色</h3>
      <div class="list">
        <div v-for="character in context.characters" :key="character.id" class="list-item">
          <div class="row between">
            <div class="stack">
              <h4>{{ character.name }} <span class="muted">{{ character.role }}</span></h4>
              <p class="muted sm">{{ character.appearance || character.description || '暂无外貌' }}</p>
              <p class="muted sm mt-6">音色：{{ character.voice_style || '未分配' }}</p>
              <audio v-if="character.voice_sample_url" class="audio-block" :src="character.voice_sample_url" controls />
              <div v-if="context.assignCharId === character.id" class="voice-list mt-8">
                <button
                  v-for="voice in context.voices"
                  :key="voice.voice_id"
                  class="voice-item"
                  type="button"
                  :class="{ active: character.voice_style === voice.voice_id }"
                  :disabled="!!context.busy"
                  @click="context.assignVoice(character, voice.voice_id, voice.provider)"
                >
                  <div>{{ voice.voice_name || voice.voice_id }}</div>
                  <div class="muted">{{ voice.language || voice.provider }}</div>
                </button>
                <div v-if="!context.voices.length" class="muted">无音色，请在设置中同步</div>
              </div>
            </div>
            <div class="row column-end">
              <img v-if="character.image_url" class="thumb" :src="character.image_url" :alt="`${character.name} 角色形象`" />
              <div v-if="context.canEdit" class="cast-actions">
                <button class="btn" :disabled="!!context.busy" @click="context.genCharImage(character)">形象</button>
                <label class="btn" :aria-disabled="!!context.busy">上传<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="!!context.busy" class="file-input-hidden" @change="context.uploadBoundImage('character', character, $event)" /></label>
                <button class="btn" :disabled="!!context.busy" @click="context.assignCharId = context.assignCharId === character.id ? null : character.id">音色</button>
                <button class="btn" :disabled="!!context.busy || !character.voice_style" @click="context.voiceSample(character)">试听</button>
                <button class="btn" :disabled="!!context.busy" @click="context.editCharacter(character)">编辑</button>
                <button class="btn" :disabled="!!context.busy" @click="context.saveCharacterToLibrary(character)">存入角色库</button>
                <button class="btn btn-danger" :disabled="!!context.busy" @click="context.removeCharacter(character)">删除</button>
              </div>
            </div>
          </div>
        </div>
        <div v-if="!context.characters.length" class="empty surface-empty wb-empty"><strong>尚未提取角色</strong><span class="muted">可使用 AI 提取剧本角色，或从角色库导入已有设定。</span></div>
      </div>
    </div>
    <div class="panel">
      <div class="toolbar spread">
        <h3 class="section-title">场景</h3>
        <button v-if="context.canEdit" class="btn" :disabled="!!context.busy" @click="context.editScene()">添加场景</button>
      </div>
      <div class="list">
        <div v-for="scene in context.scenes" :key="scene.id" class="list-item">
          <div class="row between">
            <div>
              <h4>{{ scene.location }} · {{ scene.time }}</h4>
              <p class="muted sm">{{ scene.prompt }}</p>
            </div>
            <div class="scene-actions">
              <img v-if="scene.image_url" class="thumb" :src="scene.image_url" :alt="`${scene.location} 场景图`" />
              <button v-if="context.canEdit" class="btn" :disabled="!!context.busy" @click="context.genSceneImage(scene)">生成场景</button>
              <label v-if="context.canEdit" class="btn" :aria-disabled="!!context.busy">上传<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="!!context.busy" class="file-input-hidden" @change="context.uploadBoundImage('scene', scene, $event)" /></label>
              <button v-if="context.canEdit" class="btn" :disabled="!!context.busy" @click="context.editScene(scene)">编辑</button>
              <button v-if="context.canEdit" class="btn" :disabled="!!context.busy || (context.drama?.episodes || []).length < 2" @click="context.transferScene(scene, 'copy')">复制</button>
              <button v-if="context.canEdit" class="btn" :disabled="!!context.busy || (context.drama?.episodes || []).length < 2" @click="context.transferScene(scene, 'move')">迁移</button>
              <button v-if="context.canEdit" class="btn btn-danger" :disabled="!!context.busy" @click="context.removeScene(scene)">删除</button>
            </div>
          </div>
        </div>
        <div v-if="!context.scenes.length" class="empty surface-empty wb-empty"><strong>尚未提取场景</strong><span class="muted">完成剧本后使用 AI 提取，或手动添加第一个场景。</span></div>
      </div>
      <div v-if="context.sceneTransfer" class="modal-mask" @click.self="context.sceneTransfer = null">
        <div class="modal" role="dialog" aria-modal="true" aria-labelledby="scene-transfer-title">
          <h3 id="scene-transfer-title">{{ context.sceneTransfer.mode === 'copy' ? '复制场景' : '迁移场景' }}</h3>
          <div class="field"><label for="scene-target-episode">目标剧集</label><select id="scene-target-episode" v-model.number="context.sceneTransfer.target_episode_id">
            <option :value="0">请选择</option><option v-for="item in (context.drama?.episodes || []).filter((row:any) => row.id !== context.episode?.id)" :key="item.id" :value="item.id">第 {{ item.episode_number }} 集 · {{ item.title }}</option>
          </select></div>
          <label v-if="context.sceneTransfer.mode === 'move'" class="check-inline"><input v-model="context.sceneTransfer.move_storyboards" type="checkbox" /> 同时迁移关联分镜</label>
          <div class="modal-actions"><button class="btn" @click="context.sceneTransfer = null">取消</button><button class="btn btn-primary" :disabled="!!context.busy" @click="context.confirmSceneTransfer">确认</button></div>
        </div>
      </div>
    </div>
  </div>
</template>
