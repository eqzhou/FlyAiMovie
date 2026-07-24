<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authStore } from '../auth'
import { invitationAPI } from '../api'

const route = useRoute()
const router = useRouter()
const invite = ref<any>(null)
const email = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const displayName = ref('')
const busy = ref(false)
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  loading.value = true
  error.value = ''
  try {
    invite.value = await invitationAPI.get(String(route.params.token))
    email.value = invite.value.email
  } catch (cause: any) {
    error.value = cause.message || '邀请加载失败'
  } finally {
    loading.value = false
  }
})

async function accept() {
  busy.value = true
  error.value = ''
  try {
    const actor = await invitationAPI.accept(String(route.params.token), {
      email: email.value,
      current_password: currentPassword.value || undefined,
      new_password: newPassword.value || undefined,
      display_name: displayName.value || undefined,
    })
    authStore.state.setupRequired = false
    authStore.state.actor = actor
    authStore.state.csrfToken = actor.csrf_token || ''
    if (authStore.state.csrfToken) sessionStorage.setItem('flyaimovie.csrf', authStore.state.csrfToken)
    await authStore.refreshOrganizations()
    await router.replace('/')
  } catch (cause: any) {
    error.value = cause.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <form class="auth-form" @submit.prevent="accept">
      <div class="brand auth-brand"><span class="brand-mark" aria-hidden="true"></span><span>FlyAiMovie</span></div>
      <h1>接受组织邀请</h1>
      <p class="auth-subtitle">加入团队后即可共享项目、素材与 AI 配置。</p>
      <div v-if="loading" class="page-loading auth-loading" role="status" aria-live="polite">
        <div class="page-loading-mark" aria-hidden="true"></div>
        <div><strong>正在验证邀请</strong><p class="muted" style="margin:6px 0 0">读取组织与角色信息…</p></div>
      </div>
      <template v-else>
        <p v-if="invite" class="muted invite-summary">加入 <strong>{{ invite.organization.name }}</strong>，角色：{{ invite.role }}</p>
        <div class="field"><label for="invite-email">邮箱</label><input id="invite-email" v-model.trim="email" type="email" required readonly /></div>
        <div class="field"><label for="invite-display-name">显示名称</label><input id="invite-display-name" v-model.trim="displayName" autocomplete="name" /></div>
        <div class="field"><label for="invite-current-password">已有账号当前密码（已有账号必填）</label><input id="invite-current-password" v-model="currentPassword" type="password" autocomplete="current-password" /></div>
        <div class="field"><label for="invite-new-password">新账号初始密码（新账号必填）</label><input id="invite-new-password" v-model="newPassword" type="password" minlength="12" maxlength="128" autocomplete="new-password" /></div>
        <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
        <button class="btn btn-primary auth-submit" :disabled="busy || !invite" type="submit">{{ busy ? '处理中' : '接受邀请' }}</button>
      </template>
    </form>
  </main>
</template>
