<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { passwordResetAPI } from '../api'

const route = useRoute()
const router = useRouter()
const password = ref('')
const confirm = ref('')
const error = ref('')
const done = ref(false)
const busy = ref(false)

async function submit() {
  error.value = ''
  if (password.value !== confirm.value) { error.value = '两次密码不一致'; return }
  busy.value = true
  try {
    await passwordResetAPI.consume(String(route.params.token), password.value)
    done.value = true
  } catch (cause: any) { error.value = cause.message }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-page">
    <form v-if="!done" class="auth-form" @submit.prevent="submit">
      <div class="brand auth-brand"><span class="brand-mark"></span><span>FlyAiMovie</span></div>
      <h1>设置新密码</h1>
      <div class="field"><label>新密码</label><input v-model="password" type="password" minlength="12" maxlength="128" required autocomplete="new-password" /></div>
      <div class="field"><label>确认新密码</label><input v-model="confirm" type="password" minlength="12" maxlength="128" required autocomplete="new-password" /></div>
      <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
      <button class="btn btn-primary auth-submit" :disabled="busy" type="submit">{{ busy ? '处理中' : '更新密码' }}</button>
    </form>
    <section v-else class="auth-form"><h1>密码已更新</h1><p class="muted">请使用新密码重新登录。</p><button class="btn btn-primary auth-submit" @click="router.replace('/login')">返回登录</button></section>
  </main>
</template>
