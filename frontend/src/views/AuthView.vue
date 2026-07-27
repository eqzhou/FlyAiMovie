<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authStore, RegistrationVerificationRequiredError } from '../auth'
import { passwordResetAPI } from '../api'
import { passwordValidationMessage } from '../utils/password'

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
const isRegister = computed(() => route.name === 'register')
const authUnavailable = computed(() => route.query.reason === 'auth-unavailable')
const resetRequested = ref(false)
const resetMode = ref(false)
const verificationPendingEmail = ref('')
const showRegisterEntry = computed(() => (
  !isSetup.value
  && !isRegister.value
  && !resetMode.value
  && authStore.state.registrationEnabled
  && !authStore.state.setupRequired
))

const pageTitle = computed(() => {
  if (isSetup.value) return '初始化制作空间'
  if (isRegister.value) return '注册制作空间'
  if (resetMode.value) return '找回密码'
  return '登录制作空间'
})

const pageSubtitle = computed(() => {
  if (isSetup.value) return '创建 owner 账号后即可配置 AI 服务并开始制作。'
  if (isRegister.value) return '创建你的制作空间与 owner 账号。'
  if (resetMode.value) return '输入注册邮箱，我们会发送一次性恢复链接。'
  return '从大纲到成片的本地 AI 短剧工作台。'
})

const submitLabel = computed(() => {
  if (busy.value) return '处理中'
  if (resetMode.value) return '发送恢复说明'
  if (isSetup.value) return '创建空间'
  if (isRegister.value) return '创建账号'
  return '登录'
})

async function submit() {
  error.value = ''
  if (resetRequested.value || verificationPendingEmail.value) return
  if ((isSetup.value || isRegister.value) && password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  if (isSetup.value || isRegister.value) {
    const validationMessage = passwordValidationMessage(password.value)
    if (validationMessage) { error.value = validationMessage; return }
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
      await router.replace('/')
    } else if (isRegister.value) {
      await authStore.register({
        organization_name: organizationName.value,
        display_name: displayName.value,
        email: email.value,
        password: password.value,
      })
      await router.replace('/')
    } else {
      await authStore.login(email.value, password.value)
      await router.replace('/')
    }
  } catch (cause: any) {
    if (cause instanceof RegistrationVerificationRequiredError) {
      verificationPendingEmail.value = cause.email
      return
    }
    error.value = cause.message
  } finally {
    busy.value = false
  }
}

function goRegister() {
  error.value = ''
  resetMode.value = false
  resetRequested.value = false
  router.push({ name: 'register' })
}

function goLogin() {
  error.value = ''
  resetMode.value = false
  resetRequested.value = false
  verificationPendingEmail.value = ''
  router.push({ name: 'login' })
}
</script>

<template>
  <main class="auth-page">
    <form class="auth-form" @submit.prevent="submit">
      <div class="brand auth-brand"><span class="brand-mark" aria-hidden="true"></span><span>FlyAiMovie</span></div>
      <h1>{{ pageTitle }}</h1>
      <p class="auth-subtitle">{{ pageSubtitle }}</p>
      <div v-if="authUnavailable" class="inline-alert auth-service-alert" role="alert"><div><strong>暂时无法验证登录状态</strong><span>本地服务可能尚未启动，请确认后端运行后重试登录。</span></div></div>
      <p v-if="resetRequested" class="muted" role="status">如果该邮箱存在账号，恢复说明会发送到邮箱。</p>
      <p v-if="verificationPendingEmail" class="muted" role="status">账号已创建，邮箱 {{ verificationPendingEmail }} 需要完成验证后才能登录。请稍后重试或联系管理员开通访问。</p>
      <div v-if="isSetup || isRegister" class="field">
        <label for="organization-name">空间名称</label>
        <input id="organization-name" v-model.trim="organizationName" required maxlength="100" autocomplete="organization" />
      </div>
      <div v-if="isSetup || isRegister" class="field">
        <label for="display-name">显示名称</label>
        <input id="display-name" v-model.trim="displayName" maxlength="100" autocomplete="name" />
      </div>
      <div class="field">
        <label for="auth-email">邮箱</label>
        <input id="auth-email" v-model.trim="email" required type="email" maxlength="254" autocomplete="email" />
      </div>
      <div v-if="!resetMode && !verificationPendingEmail" class="field">
        <label for="auth-password">密码</label>
        <input id="auth-password" v-model="password" required type="password" minlength="12" maxlength="72" :autocomplete="(isSetup || isRegister) ? 'new-password' : 'current-password'" />
      </div>
      <div v-if="(isSetup || isRegister) && !verificationPendingEmail" class="field">
        <label for="confirm-password">确认密码</label>
        <input id="confirm-password" v-model="confirmPassword" required type="password" minlength="12" maxlength="72" autocomplete="new-password" />
      </div>
      <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
      <button v-if="!resetRequested && !verificationPendingEmail" class="btn btn-primary auth-submit" :disabled="busy" type="submit">
        {{ submitLabel }}
      </button>
      <button v-if="showRegisterEntry" class="btn auth-submit" type="button" @click="goRegister">注册</button>
      <button v-if="!isSetup && !isRegister && !resetMode" class="btn auth-submit" type="button" @click="resetMode=true; error=''">忘记密码</button>
      <button v-if="resetMode" class="btn auth-submit" type="button" @click="resetMode=false; resetRequested=false; error=''">返回登录</button>
      <button v-if="isRegister" class="btn auth-submit" type="button" @click="goLogin">返回登录</button>
    </form>
  </main>
</template>
