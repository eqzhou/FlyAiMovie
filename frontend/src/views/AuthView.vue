<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authStore } from '../auth'
import { passwordResetAPI } from '../api'

const route = useRoute()
const router = useRouter()
const organizationName = ref('')
const displayName = ref('')
const email = ref('')
const password = ref('')
const confirmPassword = ref('')
const busy = ref(false)
const error = ref('')
const isSetup = computed(() => route.name === 'setup')
const resetRequested = ref(false)
const resetMode = ref(false)

async function submit() {
  error.value = ''
  if (isSetup.value && password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  busy.value = true
  try {
    if (resetMode.value) {
      await passwordResetAPI.request(email.value)
      resetRequested.value = true
    } else if (isSetup.value) {
      await authStore.setup({
        organization_name: organizationName.value,
        display_name: displayName.value,
        email: email.value,
        password: password.value,
      })
    } else {
      await authStore.login(email.value, password.value)
    }
      if (!resetMode.value) await router.replace('/')
  } catch (cause: any) {
    error.value = cause.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <form class="auth-form" @submit.prevent="submit">
      <div class="brand auth-brand"><span class="brand-mark" aria-hidden="true"></span><span>FlyAiMovie</span></div>
      <h1>{{ isSetup ? '初始化制作空间' : (resetMode ? '找回密码' : '登录制作空间') }}</h1>
      <p class="auth-subtitle">{{ isSetup ? '创建 owner 账号后即可配置 AI 服务并开始制作。' : (resetMode ? '输入注册邮箱，我们会发送一次性恢复链接。' : '从大纲到成片的本地 AI 短剧工作台。') }}</p>
      <p v-if="resetRequested" class="muted" role="status">如果该邮箱存在账号，恢复说明会发送到邮箱。</p>
      <div v-if="isSetup" class="field">
        <label for="organization-name">空间名称</label>
        <input id="organization-name" v-model.trim="organizationName" required maxlength="100" autocomplete="organization" />
      </div>
      <div v-if="isSetup" class="field">
        <label for="display-name">显示名称</label>
        <input id="display-name" v-model.trim="displayName" maxlength="100" autocomplete="name" />
      </div>
      <div class="field">
        <label for="auth-email">邮箱</label>
        <input id="auth-email" v-model.trim="email" required type="email" maxlength="254" autocomplete="email" />
      </div>
      <div v-if="!resetMode" class="field">
        <label for="auth-password">密码</label>
        <input id="auth-password" v-model="password" required type="password" minlength="12" maxlength="128" :autocomplete="isSetup ? 'new-password' : 'current-password'" />
      </div>
      <div v-if="isSetup" class="field">
        <label for="confirm-password">确认密码</label>
        <input id="confirm-password" v-model="confirmPassword" required type="password" minlength="12" maxlength="128" autocomplete="new-password" />
      </div>
      <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
      <button class="btn btn-primary auth-submit" :disabled="busy || resetRequested" type="submit">
        {{ busy ? '处理中' : (resetMode ? '发送恢复说明' : (isSetup ? '创建空间' : '登录')) }}
      </button>
      <button v-if="!isSetup && !resetMode && !resetRequested" class="btn auth-submit" type="button" @click="resetMode=true; error=''">忘记密码</button>
      <button v-if="resetMode && !resetRequested" class="btn auth-submit" type="button" @click="resetMode=false">返回登录</button>
    </form>
  </main>
</template>
