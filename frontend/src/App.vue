<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogOut, Menu, X } from 'lucide-vue-next'
import { authStore } from './auth'

const router = useRouter()
const route = useRoute()
const navigationOpen = ref(false)

watch(() => route.fullPath, () => { navigationOpen.value = false })

async function logout() {
  await authStore.logout()
  await router.replace('/login')
}

async function switchOrganization(event: Event) {
  const organizationId = Number((event.target as HTMLSelectElement).value)
  if (!organizationId || organizationId === authStore.state.actor?.organization.id) return
  await authStore.switchOrganization(organizationId)
  await router.replace('/')
}
</script>

<template>
  <div class="app-shell">
    <header v-if="!authStore.state.enabled || authStore.state.actor" class="topbar" @keydown.esc="navigationOpen = false">
      <router-link to="/" class="brand">
        <span class="brand-mark"></span>
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
        <button v-if="authStore.state.actor" class="nav-logout" title="退出登录" aria-label="退出登录" @click="logout"><LogOut :size="16" aria-hidden="true" /></button>
      </nav>
    </header>
    <router-view />
  </div>
</template>
