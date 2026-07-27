import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import DramaView from '../views/DramaView.vue'
import WorkbenchView from '../views/WorkbenchView.vue'
import SettingsView from '../views/SettingsView.vue'
import AssetsView from '../views/AssetsView.vue'
import AuthView from '../views/AuthView.vue'
import AuditView from '../views/AuditView.vue'
import InvitationView from '../views/InvitationView.vue'
import CharacterLibraryView from '../views/CharacterLibraryView.vue'
import JobsView from '../views/JobsView.vue'
import { authStore } from '../auth'

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition
    // 仅 query 变化（如工作台阶段切换）时保持当前滚动位置
    if (to.path === from.path) return undefined
    return { top: 0 }
  },
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/drama/:id', name: 'drama', component: DramaView },
    { path: '/drama/:id/assets', name: 'assets', component: AssetsView },
    { path: '/drama/:id/episode/:episodeNumber', name: 'workbench', component: WorkbenchView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/character-library', name: 'character-library', component: CharacterLibraryView },
	{ path: '/jobs', name: 'jobs', component: JobsView },
    { path: '/audit', name: 'audit', component: AuditView },
    { path: '/login', name: 'login', component: AuthView },
    { path: '/register', name: 'register', component: AuthView },
    { path: '/setup', name: 'setup', component: AuthView },
    { path: '/invite/:token', name: 'invite', component: InvitationView },
    { path: '/password-reset/:token', name: 'password-reset', component: () => import('../views/PasswordResetView.vue') },
  ],
})

router.beforeEach(async (to) => {
  try {
    await authStore.initialize()
  } catch {
    // 认证状态未知时保持 fail-closed，仅允许进入登录页等待服务恢复。
    return to.name === 'login' ? true : { name: 'login', query: { reason: 'auth-unavailable' } }
  }
  if (to.name === 'invite' || to.name === 'password-reset') return true
  if (!authStore.state.enabled) return true
  if (authStore.state.setupRequired) return to.name === 'setup' ? true : { name: 'setup' }
  const isRegisterRoute = to.name === 'register'
  if (!authStore.state.actor) {
    if (to.name === 'login') return true
    if (isRegisterRoute && authStore.state.registrationEnabled) return true
    return { name: 'login' }
  }
  if (to.name === 'login' || to.name === 'setup' || isRegisterRoute) return { name: 'home' }
  if (to.name === 'audit' && !['owner', 'admin'].includes(authStore.state.actor.role)) return { name: 'home' }
  return true
})

export default router
