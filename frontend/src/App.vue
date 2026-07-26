<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogOut, Menu, X, Sun, Moon, Monitor } from 'lucide-vue-next'
import { authStore } from './auth'
import ConfirmDialog from './components/ConfirmDialog.vue'

const router = useRouter()
const route = useRoute()
const navigationOpen = ref(false)

type Theme = 'light' | 'dark' | 'system'
const THEME_ORDER: Theme[] = ['system', 'light', 'dark']
const THEME_LABEL: Record<Theme, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色',
}

function readStoredTheme(): Theme {
  const stored = localStorage.getItem('theme')
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system'
}

const currentTheme = ref<Theme>(readStoredTheme())
const mediaQuery = typeof window !== 'undefined'
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null

function resolveDataTheme(theme: Theme): 'light' | 'dark' {
  if (theme === 'system') {
    return mediaQuery?.matches ? 'dark' : 'light'
  }
  return theme
}

function applyTheme(theme: Theme) {
  currentTheme.value = theme
  localStorage.setItem('theme', theme)
  document.documentElement.setAttribute('data-theme', resolveDataTheme(theme))
}

function onSystemThemeChange() {
  if (currentTheme.value === 'system') {
    document.documentElement.setAttribute('data-theme', resolveDataTheme('system'))
  }
}

// Apply before paint when possible (script setup runs before mount in SPA shell).
if (typeof document !== 'undefined') {
  document.documentElement.setAttribute('data-theme', resolveDataTheme(currentTheme.value))
}

onMounted(() => {
  applyTheme(currentTheme.value)
  mediaQuery?.addEventListener('change', onSystemThemeChange)
})

onBeforeUnmount(() => {
  mediaQuery?.removeEventListener('change', onSystemThemeChange)
})

function cycleTheme() {
  const currentIndex = THEME_ORDER.indexOf(currentTheme.value)
  applyTheme(THEME_ORDER[(currentIndex + 1) % THEME_ORDER.length])
}

const themeButtonLabel = computed(() => `切换主题，当前：${THEME_LABEL[currentTheme.value]}`)

watch(() => route.fullPath, () => { navigationOpen.value = false })

// 会话失效（401）后 actor 会被清空：主动带回登录页，避免停留在已失效页面。
watch(() => authStore.state.actor, (actor, previous) => {
  if (actor || !previous || !authStore.state.enabled) return
  if (['login', 'setup', 'invite', 'password-reset'].includes(String(route.name))) return
  router.replace('/login')
})

async function logout() {
  await authStore.logout()
  await router.replace('/login')
}

async function switchOrganization(event: Event) {
  const select = event.target as HTMLSelectElement
  const organizationId = Number(select.value)
  const currentId = authStore.state.actor?.organization.id
  if (!organizationId || organizationId === currentId) return
  try {
    await authStore.switchOrganization(organizationId)
    await router.replace('/')
  } catch (cause) {
    console.warn('switch organization failed', cause)
    select.value = String(currentId || '')
  }
}
</script>

<template>
  <div class="app-shell">
    <header v-if="!authStore.state.enabled || authStore.state.actor" class="topbar" @keydown.esc="navigationOpen = false">
      <router-link to="/" class="brand">
        <span class="brand-mark" aria-hidden="true"></span>
        <span>FlyAiMovie</span>
      </router-link>
      <button
        class="nav-toggle"
        type="button"
        aria-controls="primary-navigation"
        :aria-expanded="navigationOpen"
        :aria-label="navigationOpen ? '关闭导航' : '打开导航'"
        title="导航"
        @click="navigationOpen = !navigationOpen"
      ><X v-if="navigationOpen" :size="18" aria-hidden="true" /><Menu v-else :size="18" aria-hidden="true" /></button>
      <nav id="primary-navigation" class="nav" :class="{ open: navigationOpen }" aria-label="主导航">
        <router-link to="/">项目</router-link>
        <router-link to="/character-library">角色库</router-link>
        <router-link to="/jobs">任务</router-link>
        <router-link to="/settings">设置</router-link>
        <router-link v-if="authStore.state.actor && ['owner', 'admin'].includes(authStore.state.actor.role)" to="/audit">审计</router-link>
        <select v-if="authStore.state.organizations.length > 1" class="organization-switch" :value="authStore.state.actor?.organization.id" aria-label="切换组织" @change="switchOrganization">
          <option v-for="organization in authStore.state.organizations" :key="organization.id" :value="organization.id">{{ organization.name }}</option>
        </select>
        <span v-if="authStore.state.actor" class="nav-identity">{{ authStore.state.actor.organization.name }} · {{ authStore.state.actor.user.display_name }}</span>

        <button
          class="nav-theme-toggle"
          type="button"
          :title="themeButtonLabel"
          :aria-label="themeButtonLabel"
          @click="cycleTheme"
        >
          <Monitor v-if="currentTheme === 'system'" :size="16" aria-hidden="true" />
          <Sun v-else-if="currentTheme === 'light'" :size="16" aria-hidden="true" />
          <Moon v-else :size="16" aria-hidden="true" />
        </button>

        <button v-if="authStore.state.actor" class="nav-logout" type="button" title="退出登录" aria-label="退出登录" @click="logout"><LogOut :size="16" aria-hidden="true" /></button>
      </nav>
    </header>
    <main class="app-main">
      <router-view />
    </main>
    <ConfirmDialog />
  </div>
</template>
