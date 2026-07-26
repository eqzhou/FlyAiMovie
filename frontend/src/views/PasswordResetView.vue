<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { passwordResetAPI } from '../api'
import { passwordValidationMessage } from '../utils/password'

const route = useRoute()
const router = useRouter()
const password = ref('')
const confirm = ref('')
const error = ref('')
const done = ref(false)
const busy = ref(false)

async function submit() {
  error.value = ''
  if (password.value !== confirm.value) {
    error.value = '两次密码不一致'
    return
  }
  const validationMessage = passwordValidationMessage(password.value, '新密码')
  if (validationMessage) { error.value = validationMessage; return }
  busy.value = true
  try {
    await passwordResetAPI.consume(String(route.params.token), password.value)
    done.value = true
  } catch (cause: any) {
    error.value = cause.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <form v-if="!done" class="auth-form" @submit.prevent="submit">
      <div class="brand auth-brand"><span class="brand-mark" aria-hidden="true"></span><span>FlyAiMovie</span></div>
      <h1>设置新密码</h1>
      <p class="auth-subtitle">密码需为 12–72 字节。更新后请使用新密码登录。</p>
      <div class="field"><label for="reset-password">新密码</label><input id="reset-password" v-model="password" type="password" minlength="12" maxlength="72" required autocomplete="new-password" /></div>
      <div class="field"><label for="reset-confirm">确认新密码</label><input id="reset-confirm" v-model="confirm" type="password" minlength="12" maxlength="72" required autocomplete="new-password" /></div>
      <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
      <button class="btn btn-primary auth-submit" :disabled="busy" type="submit">{{ busy ? '处理中' : '更新密码' }}</button>
    </form>
    <section v-else class="auth-form" aria-live="polite">
      <div class="brand auth-brand"><span class="brand-mark" aria-hidden="true"></span><span>FlyAiMovie</span></div>
      <h1>密码已更新</h1>
      <p class="auth-subtitle">请使用新密码重新登录制作空间。</p>
      <button class="btn btn-primary auth-submit" type="button" @click="router.replace('/login')">返回登录</button>
    </section>
  </main>
</template>
