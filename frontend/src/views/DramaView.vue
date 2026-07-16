<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { dramaAPI, episodeAPI, propAPI, settingsAPI } from '../api'

const route = useRoute()
const router = useRouter()
const drama = ref<any>(null)
const configs = ref<any[]>([])
const showAddEp = ref(false)
const epForm = ref({ title: '', image_config_id: 0, video_config_id: 0, audio_config_id: 0 })
const msg = ref('')
const propsList = ref<any[]>([])
const propForm = ref({ name: '', description: '' })
const showProp = ref(false)

const id = computed(() => Number(route.params.id))

async function load() {
  drama.value = await dramaAPI.get(id.value)
  try { propsList.value = await propAPI.list(id.value) } catch { propsList.value = drama.value.props || [] }
  configs.value = await settingsAPI.aiConfigs()
  const img = configs.value.find((c) => c.service_type === 'image')
  const vid = configs.value.find((c) => c.service_type === 'video')
  const aud = configs.value.find((c) => c.service_type === 'audio')
  epForm.value.image_config_id = img?.id || 0
  epForm.value.video_config_id = vid?.id || 0
  epForm.value.audio_config_id = aud?.id || 0
}

async function addEpisode() {
  if (!epForm.value.image_config_id || !epForm.value.video_config_id || !epForm.value.audio_config_id) {
    msg.value = '请先在设置中配置图片/视频/音频服务，并选择配置'
    return
  }
  await episodeAPI.create({
    drama_id: id.value,
    title: epForm.value.title || undefined,
    image_config_id: epForm.value.image_config_id,
    video_config_id: epForm.value.video_config_id,
    audio_config_id: epForm.value.audio_config_id,
  })
  showAddEp.value = false
  await load()
}

async function addProp() {
  if (!propForm.value.name.trim()) return
  await propAPI.create({ drama_id: id.value, name: propForm.value.name, description: propForm.value.description })
  propForm.value = { name: '', description: '' }
  showProp.value = false
  await load()
}

async function genPropImage(p: any) {
  const ep = (drama.value.episodes || [])[0]
  await propAPI.generateImage(p.id, ep?.id)
  await load()
}

function openEp(ep: any) {
  router.push(`/drama/${id.value}/episode/${ep.episode_number}`)
}

onMounted(load)
</script>

<template>
  <div class="page" v-if="drama">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ drama.title }}</h1>
        <p class="page-desc">{{ drama.description || '暂无简介' }} · {{ drama.style }}</p>
      </div>
      <div class="row">
        <button class="btn" @click="router.push('/')">返回</button>
        <button class="btn" @click="router.push(`/drama/${id}/assets`)">素材库</button>
        <button class="btn btn-primary" @click="showAddEp = true">新增集</button>
      </div>
    </div>

    <div class="grid">
      <div v-for="ep in drama.episodes || []" :key="ep.id" class="card project-card" @click="openEp(ep)">
        <div>
          <span class="badge">第 {{ ep.episode_number }} 集</span>
          <h3 class="project-title">{{ ep.title }}</h3>
          <p class="muted">状态：{{ ep.status }}</p>
        </div>
        <div class="card-footer">
          <span>{{ ep.script_content ? '已有剧本' : '待写剧本' }}</span>
          <span>进入工作台 →</span>
        </div>
      </div>
    </div>

    <div class="panel" style="margin-top:20px">
      <div class="toolbar" style="justify-content:space-between">
        <h3 style="margin:0">项目资产</h3>
        <button class="btn" @click="showProp = true">添加道具</button>
      </div>
      <p class="muted">角色 {{ drama.characters?.length || 0 }} · 场景 {{ drama.scenes?.length || 0 }} · 道具 {{ propsList.length }}</p>
      <div class="list" style="margin-top:12px" v-if="propsList.length">
        <div v-for="p in propsList" :key="p.id" class="list-item row" style="justify-content:space-between;align-items:center">
          <div>
            <h4 style="margin:0">{{ p.name }}</h4>
            <p class="muted" style="margin:4px 0 0;font-size:12px">{{ p.description || p.prompt }}</p>
          </div>
          <div class="row">
            <img v-if="p.image_url" class="thumb" :src="p.image_url" />
            <button class="btn" @click="genPropImage(p)">生成图</button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="showProp" class="modal-mask" @click.self="showProp = false">
      <div class="modal">
        <h3>添加道具</h3>
        <div class="field"><label>名称</label><input v-model="propForm.name" /></div>
        <div class="field"><label>描述</label><textarea v-model="propForm.description" rows="3" /></div>
        <div class="modal-actions">
          <button class="btn" @click="showProp = false">取消</button>
          <button class="btn btn-primary" @click="addProp">创建</button>
        </div>
      </div>
    </div>

    <div v-if="showAddEp" class="modal-mask" @click.self="showAddEp = false">
      <div class="modal">
        <h3>新增集</h3>
        <p v-if="msg" class="muted">{{ msg }}</p>
        <div class="field"><label>标题</label><input v-model="epForm.title" placeholder="可选" /></div>
        <div class="field"><label>图片配置</label>
          <select v-model.number="epForm.image_config_id">
            <option :value="0">请选择</option>
            <option v-for="c in configs.filter(x => x.service_type==='image')" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field"><label>视频配置</label>
          <select v-model.number="epForm.video_config_id">
            <option :value="0">请选择</option>
            <option v-for="c in configs.filter(x => x.service_type==='video')" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field"><label>音频配置</label>
          <select v-model.number="epForm.audio_config_id">
            <option :value="0">请选择</option>
            <option v-for="c in configs.filter(x => x.service_type==='audio')" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showAddEp = false">取消</button>
          <button class="btn btn-primary" @click="addEpisode">创建</button>
        </div>
      </div>
    </div>
  </div>
</template>
