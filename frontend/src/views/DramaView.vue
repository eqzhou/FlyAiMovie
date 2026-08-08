<script setup lang="ts">
import { useDrama } from './drama/useDrama'

const { router, drama, propsList, activeView, assetView, loading, busy, error, message, showEpisodeDialog, showPropDialog, showCharacterDialog, showSceneDialog, showSceneTransfer, episodeError, propError, characterError, sceneError, transferError, episodeTitleInput, propNameInput, characterNameInput, sceneLocationInput, episodeForm, propForm, characterForm, sceneForm, sceneTransfer, id, canEdit, sortedEpisodes, projectSummary, imageConfigs, videoConfigs, audioConfigs, activeVoices, projectStyle, otherEpisodes, load, openEpisodeDialog, closeEpisodeDialog, saveEpisode, openPropDialog, closePropDialog, saveProp, generatePropImage, removeProp, openCharacterDialog, closeCharacterDialog, saveCharacter, generateCharacterImage, sampleCharacterVoice, saveCharacterToLibrary, removeCharacter, openSceneDialog, closeSceneDialog, saveScene, generateSceneImage, removeScene, openSceneTransfer, closeSceneTransfer, confirmSceneTransfer, openEpisode, delEpisode, copyEpisode, moveEpisode, statusText, openEditEpisode } = useDrama()
void episodeTitleInput
void propNameInput
void characterNameInput
void sceneLocationInput
</script>

<template>
  <div v-if="loading && !drama" class="page">
    <div class="page-loading" role="status" aria-live="polite">
      <div class="page-loading-mark" aria-hidden="true"></div>
      <div>
        <strong>正在打开项目</strong>
        <p class="muted">加载剧集、资产与生成配置…</p>
      </div>
    </div>
  </div>
  <div v-else-if="error && !drama" class="page"><div class="load-error"><h2>项目加载失败</h2><p class="muted">{{ error }}</p><button class="btn" @click="load">重新加载</button></div></div>
  <div v-else-if="drama" class="page" :aria-busy="loading">
    <div v-if="error" class="inline-alert" role="alert">
      <div><strong>部分内容暂未更新</strong><span>{{ error }}</span></div>
      <button class="btn" type="button" :disabled="loading" @click="load">重试加载</button>
    </div>
    <div class="page-head">
      <div><h1 class="page-title">{{ drama.title }}</h1><p class="page-desc project-description">{{ projectSummary }} · {{ projectStyle }}</p></div>
      <div class="toolbar project-head-actions"><button class="btn" @click="router.push('/')">返回项目</button><button class="btn" @click="router.push(`/drama/${id}/assets`)">素材库</button></div>
    </div>

    <div class="settings-tabs" role="tablist" aria-label="项目内容">
      <button role="tab" :aria-selected="activeView === 'episodes'" :class="{ active: activeView === 'episodes' }" @click="activeView = 'episodes'">剧集</button>
      <button role="tab" :aria-selected="activeView === 'assets'" :class="{ active: activeView === 'assets' }" @click="activeView = 'assets'">项目资产</button>
    </div>

    <section v-if="activeView === 'episodes'" class="settings-section" role="tabpanel">
      <div class="settings-section-head"><div><h2>剧集</h2><p class="muted">{{ sortedEpisodes.length }} 集 · 按制作顺序进入工作台</p></div><button v-if="canEdit" class="btn btn-primary" @click="openEpisodeDialog()">新增剧集</button></div>
      <div v-if="sortedEpisodes.length" class="episode-grid">
        <article v-for="(episode, episodeIndex) in sortedEpisodes" :key="episode.id" class="card episode-card" role="button" tabindex="0" @click="openEpisode(episode)" @keydown.enter="openEpisode(episode)" @keydown.space.prevent="openEpisode(episode)">
          <div class="episode-number">{{ String(episode.episode_number).padStart(2, '0') }}</div>
          <div class="episode-main"><div class="episode-title-row"><h3>{{ episode.title || `第 ${episode.episode_number} 集` }}</h3><span class="audit-status" :class="episode.status === 'failed' ? 'fail' : episode.status === 'completed' ? 'ok' : ''">{{ statusText(episode.status) }}</span></div><p class="muted">{{ episode.script_content ? '剧本已就绪' : '等待录入或生成剧本' }}</p></div>
          <div class="episode-actions" @click.stop>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`编辑剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="编辑剧集" :disabled="!!busy" @click="openEditEpisode(episode, $event)">编辑</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`上移剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="上移" :disabled="!!busy || episodeIndex === 0" @click="moveEpisode(episode, 'up', $event)">上移</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`下移剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="下移" :disabled="!!busy || episodeIndex === sortedEpisodes.length - 1" @click="moveEpisode(episode, 'down', $event)">下移</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`复制剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="复制剧集" :disabled="!!busy" @click="copyEpisode(episode, $event)">复制</button>
            <button v-if="canEdit" type="button" class="btn btn-ghost" :aria-label="`删除剧集 ${episode.title || ('第 ' + episode.episode_number + ' 集')}`" title="删除剧集" :disabled="!!busy" @click="delEpisode(episode, $event)">删除</button>
            <span class="episode-enter">进入工作台</span>
          </div>
        </article>
      </div>
      <div v-else class="empty project-empty"><strong>还没有剧集</strong><span class="muted">创建第一集后即可进入 AI 制作工作台。</span><button v-if="canEdit" class="btn btn-primary" @click="openEpisodeDialog()">新增剧集</button></div>
    </section>

    <section v-else class="settings-section" role="tabpanel">
      <div class="settings-section-head">
        <div><h2>项目资产</h2><p class="muted">角色 {{ drama.characters?.length || 0 }} · 场景 {{ drama.scenes?.length || 0 }} · 道具 {{ propsList.length }}</p></div>
        <button v-if="canEdit && assetView === 'characters'" class="btn btn-primary" @click="openCharacterDialog()">添加角色</button>
        <button v-else-if="canEdit && assetView === 'scenes'" class="btn btn-primary" @click="openSceneDialog()">添加场景</button>
        <button v-else-if="canEdit && assetView === 'props'" class="btn btn-primary" @click="openPropDialog()">添加道具</button>
      </div>
      <div class="project-asset-tabs" role="tablist" aria-label="资产类型">
        <button role="tab" :aria-selected="assetView === 'characters'" :class="{ active: assetView === 'characters' }" @click="assetView = 'characters'">角色 {{ drama.characters?.length || 0 }}</button>
        <button role="tab" :aria-selected="assetView === 'scenes'" :class="{ active: assetView === 'scenes' }" @click="assetView = 'scenes'">场景 {{ drama.scenes?.length || 0 }}</button>
        <button role="tab" :aria-selected="assetView === 'props'" :class="{ active: assetView === 'props' }" @click="assetView = 'props'">道具 {{ propsList.length }}</button>
      </div>

      <div class="panel project-asset-list">
        <table v-if="assetView === 'characters' && drama.characters?.length" class="table">
          <thead><tr><th>形象</th><th>名称</th><th>定位</th><th>外观</th><th>音色</th><th></th></tr></thead>
          <tbody>
            <tr v-for="character in drama.characters" :key="character.id">
              <td><img v-if="character.image_url" class="thumb" :src="character.image_url" :alt="character.name" /><span v-else class="character-library-placeholder">{{ (character.name || '?').slice(0, 1) }}</span></td>
              <td><strong>{{ character.name }}</strong></td>
              <td>{{ character.role || '未设置' }}</td>
              <td>{{ character.appearance || character.description || '未填写' }}</td>
              <td>{{ character.voice_style || '未绑定' }}</td>
              <td>
                <div v-if="canEdit" class="toolbar settings-table-actions">
                  <button class="btn" :disabled="!!busy" @click="generateCharacterImage(character)">生成形象</button>
                  <button class="btn" :disabled="!!busy" @click="sampleCharacterVoice(character)">试听</button>
                  <button class="btn" :disabled="!!busy" @click="saveCharacterToLibrary(character)">存入角色库</button>
                  <button class="btn" :disabled="!!busy" @click="openCharacterDialog(character)">编辑</button>
                  <button class="btn btn-danger" :disabled="!!busy" @click="removeCharacter(character)">删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <table v-else-if="assetView === 'scenes' && drama.scenes?.length" class="table">
          <thead><tr><th>形象</th><th>地点</th><th>时间</th><th>描述</th><th></th></tr></thead>
          <tbody>
            <tr v-for="scene in drama.scenes" :key="scene.id">
              <td><img v-if="scene.image_url" class="thumb" :src="scene.image_url" :alt="scene.location" /><span v-else class="character-library-placeholder">{{ (scene.location || '?').slice(0, 1) }}</span></td>
              <td><strong>{{ scene.location }}</strong></td>
              <td>{{ scene.time || '未设置' }}</td>
              <td>{{ scene.description || scene.prompt || '未填写' }}</td>
              <td>
                <div v-if="canEdit" class="toolbar settings-table-actions">
                  <button class="btn" :disabled="!!busy" @click="generateSceneImage(scene)">生成场景</button>
                  <button class="btn" :disabled="!!busy || otherEpisodes.length === 0" @click="openSceneTransfer(scene, 'copy')">复制</button>
                  <button class="btn" :disabled="!!busy || otherEpisodes.length === 0" @click="openSceneTransfer(scene, 'move')">迁移</button>
                  <button class="btn" :disabled="!!busy" @click="openSceneDialog(scene)">编辑</button>
                  <button class="btn btn-danger" :disabled="!!busy" @click="removeScene(scene)">删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <table v-else-if="assetView === 'props' && propsList.length" class="table">
          <thead><tr><th>形象</th><th>名称</th><th>类型</th><th>描述</th><th></th></tr></thead>
          <tbody>
            <tr v-for="prop in propsList" :key="prop.id">
              <td><img v-if="prop.image_url" class="thumb" :src="prop.image_url" :alt="prop.name" /><span v-else class="character-library-placeholder">{{ prop.name.slice(0, 1) }}</span></td>
              <td><strong>{{ prop.name }}</strong></td>
              <td>{{ prop.type || '未分类' }}</td>
              <td>{{ prop.description || prop.prompt || '未填写' }}</td>
              <td>
                <div v-if="canEdit" class="toolbar settings-table-actions">
                  <button class="btn" :disabled="!!busy" @click="generatePropImage(prop)">生成图片</button>
                  <button class="btn" :disabled="!!busy" @click="openPropDialog(prop)">编辑</button>
                  <button class="btn btn-danger" :disabled="!!busy" @click="removeProp(prop)">删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty project-empty">
          <strong>暂无{{ assetView === 'characters' ? '角色' : assetView === 'scenes' ? '场景' : '道具' }}</strong>
          <span class="muted">可在此直接维护，也可在剧集工作台中通过 AI 提取。</span>
          <button v-if="canEdit && assetView === 'characters'" class="btn btn-primary" @click="openCharacterDialog()">添加角色</button>
          <button v-else-if="canEdit && assetView === 'scenes'" class="btn btn-primary" @click="openSceneDialog()">添加场景</button>
          <button v-else-if="canEdit && assetView === 'props'" class="btn btn-primary" @click="openPropDialog()">添加道具</button>
        </div>
      </div>
    </section>

    <div v-if="showEpisodeDialog" class="modal-mask" @click.self="closeEpisodeDialog">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="episodeForm.id ? 'edit-episode-title' : 'add-episode-title'" @keydown.esc="closeEpisodeDialog" @submit.prevent="saveEpisode">
        <h3 :id="episodeForm.id ? 'edit-episode-title' : 'add-episode-title'">{{ episodeForm.id ? '编辑剧集' : '新增剧集' }}</h3>
        <div class="field"><label for="episode-title">标题（选填）</label><input id="episode-title" ref="episodeTitleInput" v-model="episodeForm.title" maxlength="200" placeholder="默认按集数命名" /></div>
        <div class="field"><label for="episode-image-config">图片服务</label><select id="episode-image-config" v-model.number="episodeForm.image_config_id"><option :value="0">请选择</option><option v-for="config in imageConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <div class="field"><label for="episode-video-config">视频服务</label><select id="episode-video-config" v-model.number="episodeForm.video_config_id"><option :value="0">请选择</option><option v-for="config in videoConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <div class="field"><label for="episode-audio-config">音频服务</label><select id="episode-audio-config" v-model.number="episodeForm.audio_config_id"><option :value="0">请选择</option><option v-for="config in audioConfigs" :key="config.id" :value="config.id">{{ config.name }}</option></select></div>
        <p v-if="episodeError" class="form-error" role="alert">{{ episodeError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closeEpisodeDialog">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'episode'">{{ busy === 'episode' ? '保存中…' : (episodeForm.id ? '保存剧集' : '创建剧集') }}</button></div>
      </form>
    </div>

    <div v-if="showCharacterDialog" class="modal-mask" @click.self="closeCharacterDialog">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="characterForm.id ? 'edit-character-title' : 'add-character-title'" @keydown.esc="closeCharacterDialog" @submit.prevent="saveCharacter">
        <h3 :id="characterForm.id ? 'edit-character-title' : 'add-character-title'">{{ characterForm.id ? '编辑角色' : '添加角色' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field"><label for="character-name">名称 <span class="required-mark">*</span></label><input id="character-name" ref="characterNameInput" v-model="characterForm.name" maxlength="200" required /></div>
        <div class="field"><label for="character-role">定位</label><input id="character-role" v-model="characterForm.role" maxlength="120" placeholder="主角 / 配角" /></div>
        <div class="field"><label for="character-appearance">外观</label><textarea id="character-appearance" v-model="characterForm.appearance" rows="3" maxlength="4000" /></div>
        <div class="field"><label for="character-description">描述</label><textarea id="character-description" v-model="characterForm.description" rows="3" maxlength="4000" /></div>
        <div class="field"><label for="character-personality">性格</label><textarea id="character-personality" v-model="characterForm.personality" rows="2" maxlength="4000" /></div>
        <div class="field"><label for="character-voice">音色</label>
          <select id="character-voice" v-model="characterForm.voice_style" @change="characterForm.voice_provider = activeVoices.find((voice) => voice.voice_id === characterForm.voice_style)?.provider || characterForm.voice_provider">
            <option value="">未绑定</option>
            <option v-for="voice in activeVoices" :key="voice.voice_id" :value="voice.voice_id">{{ voice.voice_name || voice.voice_id }} · {{ voice.provider }}</option>
          </select>
        </div>
        <div class="field"><label for="character-reference-images">参考图 URL（每行一个，最多 8 张）</label><textarea id="character-reference-images" v-model="characterForm.reference_images" rows="3" maxlength="4000" placeholder="可选，生成形象时用于保持角色一致性" /></div>
        <p v-if="characterError" class="form-error" role="alert">{{ characterError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closeCharacterDialog">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'character'">{{ busy === 'character' ? '保存中…' : (characterForm.id ? '保存角色' : '添加角色') }}</button></div>
      </form>
    </div>

    <div v-if="showSceneDialog" class="modal-mask" @click.self="closeSceneDialog">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="sceneForm.id ? 'edit-scene-title' : 'add-scene-title'" @keydown.esc="closeSceneDialog" @submit.prevent="saveScene">
        <h3 :id="sceneForm.id ? 'edit-scene-title' : 'add-scene-title'">{{ sceneForm.id ? '编辑场景' : '添加场景' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field"><label for="scene-location">地点 <span class="required-mark">*</span></label><input id="scene-location" ref="sceneLocationInput" v-model="sceneForm.location" maxlength="200" required /></div>
        <div class="field"><label for="scene-time">时间</label><input id="scene-time" v-model="sceneForm.time" maxlength="120" placeholder="夜 / 清晨" /></div>
        <div class="field"><label for="scene-prompt">画面提示词</label><textarea id="scene-prompt" v-model="sceneForm.prompt" rows="4" maxlength="4000" placeholder="可选，用于生成场景图" /></div>
        <p v-if="sceneError" class="form-error" role="alert">{{ sceneError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closeSceneDialog">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'scene'">{{ busy === 'scene' ? '保存中…' : (sceneForm.id ? '保存场景' : '添加场景') }}</button></div>
      </form>
    </div>

    <div v-if="showSceneTransfer && sceneTransfer" class="modal-mask" @click.self="closeSceneTransfer">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="sceneTransfer.mode === 'copy' ? 'copy-scene-title' : 'move-scene-title'" @keydown.esc="closeSceneTransfer" @submit.prevent="confirmSceneTransfer">
        <h3 :id="sceneTransfer.mode === 'copy' ? 'copy-scene-title' : 'move-scene-title'">{{ sceneTransfer.mode === 'copy' ? '复制场景' : '迁移场景' }}</h3>
        <p class="muted">场景：{{ sceneTransfer.scene.location }}</p>
        <div class="field"><label for="scene-target-episode">目标剧集</label>
          <select id="scene-target-episode" v-model.number="sceneTransfer.target_episode_id">
            <option :value="0">请选择</option>
            <option v-for="episode in otherEpisodes" :key="episode.id" :value="episode.id">{{ episode.title || `第 ${episode.episode_number} 集` }}</option>
          </select>
        </div>
        <div v-if="sceneTransfer.mode === 'move'" class="field">
          <label><input v-model="sceneTransfer.move_storyboards" type="checkbox" /> 同时迁移关联分镜</label>
        </div>
        <p v-if="transferError" class="form-error" role="alert">{{ transferError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closeSceneTransfer">取消</button><button type="submit" class="btn btn-primary" :disabled="!!busy">{{ sceneTransfer.mode === 'copy' ? '确认复制' : '确认迁移' }}</button></div>
      </form>
    </div>

    <div v-if="showPropDialog" class="modal-mask" @click.self="closePropDialog">
      <form class="modal settings-modal" role="dialog" aria-modal="true" :aria-labelledby="propForm.id ? 'edit-prop-title' : 'add-prop-title'" @keydown.esc="closePropDialog" @submit.prevent="saveProp">
        <h3 :id="propForm.id ? 'edit-prop-title' : 'add-prop-title'">{{ propForm.id ? '编辑道具' : '添加道具' }}</h3>
        <p class="form-required-note"><span class="required-mark">*</span> 为必填项</p>
        <div class="field"><label for="prop-name">名称 <span class="required-mark">*</span></label><input id="prop-name" ref="propNameInput" v-model="propForm.name" maxlength="120" required /></div>
        <div class="field"><label for="prop-type">类型</label><input id="prop-type" v-model="propForm.type" maxlength="120" placeholder="可选，如武器、饰品、家具" /></div>
        <div class="field"><label for="prop-description">描述</label><textarea id="prop-description" v-model="propForm.description" rows="4" maxlength="4000" /></div>
        <div class="field"><label for="prop-prompt">画面提示词</label><textarea id="prop-prompt" v-model="propForm.prompt" rows="3" maxlength="4000" placeholder="可选，用于生成道具形象" /></div>
        <div class="field"><label for="prop-reference-images">参考图 URL（每行一个，最多 8 张）</label><textarea id="prop-reference-images" v-model="propForm.reference_images" rows="3" maxlength="4000" placeholder="可选，生成道具图时用于保持外观一致" /></div>
        <p v-if="propError" class="form-error" role="alert">{{ propError }}</p>
        <div class="modal-actions"><button type="button" class="btn" @click="closePropDialog">取消</button><button type="submit" class="btn btn-primary" :disabled="busy === 'prop'">{{ busy === 'prop' ? '保存中…' : (propForm.id ? '保存道具' : '添加道具') }}</button></div>
      </form>
    </div>

    <div v-if="message" class="toast" role="status">{{ message }}</div>
  </div>
</template>
