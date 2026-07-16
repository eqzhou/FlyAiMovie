import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import DramaView from '../views/DramaView.vue'
import WorkbenchView from '../views/WorkbenchView.vue'
import SettingsView from '../views/SettingsView.vue'
import AssetsView from '../views/AssetsView.vue'
import AuthView from '../views/AuthView.vue'
import AuditView from '../views/AuditView.vue'
import InvitationView from '../views/InvitationView.vue'
import { authStore } from '../auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/drama/:id', name: 'drama', component: DramaView },
    { path: '/drama/:id/assets', name: 'assets', component: AssetsView },
    { path: '/drama/:id/episode/:episodeNumber', name: 'workbench', component: WorkbenchView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/audit', name: 'audit', component: AuditView },
    { path: '/login', name: 'login', component: AuthView },
    { path: '/setup', name: 'setup', component: AuthView },
    { path: '/invite/:token', name: 'invite', component: InvitationView },
    { path: '/password-reset/:token', name: 'password-reset', component: () => import('../views/PasswordResetView.vue') },
  ],
})

router.beforeEach(async (to) => {
  await authStore.initialize()
  if (to.name === 'invite' || to.name === 'password-reset') return true
  if (!authStore.state.enabled) return true
  if (authStore.state.setupRequired) return to.name === 'setup' ? true : { name: 'setup' }
  if (!authStore.state.actor) return to.name === 'login' ? true : { name: 'login' }
  if (to.name === 'login' || to.name === 'setup') return { name: 'home' }
  if (to.name === 'audit' && !['owner', 'admin'].includes(authStore.state.actor.role)) return { name: 'home' }
  return true
})

export default router
