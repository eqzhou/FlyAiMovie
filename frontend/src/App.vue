<script setup lang="ts">
import { useRouter } from 'vue-router'
import { authStore } from './auth'

const router = useRouter()

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
    <header v-if="!authStore.state.enabled || authStore.state.actor" class="topbar">
      <router-link to="/" class="brand">
        <span class="brand-mark"></span>
        <span>FlyAiMovie</span>
      </router-link>
      <nav class="nav">
        <router-link to="/">项目</router-link>
        <router-link to="/settings">设置</router-link>
        <router-link v-if="authStore.state.actor && ['owner', 'admin'].includes(authStore.state.actor.role)" to="/audit">审计</router-link>
        <select v-if="authStore.state.organizations.length > 1" class="organization-switch" :value="authStore.state.actor?.organization.id" aria-label="切换组织" @change="switchOrganization">
          <option v-for="organization in authStore.state.organizations" :key="organization.id" :value="organization.id">{{ organization.name }}</option>
        </select>
        <span v-if="authStore.state.actor" class="nav-identity">{{ authStore.state.actor.organization.name }} · {{ authStore.state.actor.user.display_name }}</span>
        <button v-if="authStore.state.actor" class="nav-logout" title="退出登录" aria-label="退出登录" @click="logout">↪</button>
      </nav>
    </header>
    <router-view />
  </div>
</template>
