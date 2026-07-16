import { reactive } from 'vue'

const BASE = '/api/v1'

type AuthActor = {
  user: { id: number; email: string; display_name: string }
  organization: { id: number; name: string; slug: string }
  role: 'owner' | 'admin' | 'editor' | 'viewer'
  csrf_token?: string
}

const state = reactive({
  initialized: false,
  enabled: false,
  setupRequired: false,
  actor: null as AuthActor | null,
  csrfToken: sessionStorage.getItem('flyaimovie.csrf') || '',
  organizations: [] as Array<{ id: number; name: string; slug: string; role: string; current: boolean }>,
})

async function authRequest(path: string, method = 'GET', body?: unknown) {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (state.csrfToken && method !== 'GET') headers['X-CSRF-Token'] = state.csrfToken
  const response = await fetch(`${BASE}${path}`, {
    method,
    credentials: 'include',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await response.json()
  if (!response.ok) throw new Error(payload.message || String(response.status))
  return payload.data ?? payload
}

function setActor(actor: AuthActor | null) {
  state.actor = actor
  state.csrfToken = actor?.csrf_token || ''
  if (state.csrfToken) sessionStorage.setItem('flyaimovie.csrf', state.csrfToken)
  else sessionStorage.removeItem('flyaimovie.csrf')
}

async function refreshOrganizations() {
  state.organizations = state.actor ? await authRequest('/auth/organizations') : []
}

async function initialize() {
  if (state.initialized) return
  try {
    const status = await authRequest('/auth/status')
    state.enabled = Boolean(status.enabled)
    state.setupRequired = Boolean(status.setup_required)
    if (state.enabled && !state.setupRequired) {
      try {
        setActor(await authRequest('/auth/me'))
        await refreshOrganizations()
      } catch {
        setActor(null)
      }
    }
  } finally {
    state.initialized = true
  }
}

async function login(email: string, password: string) {
  const actor = await authRequest('/auth/login', 'POST', { email, password })
  state.setupRequired = false
  setActor(actor)
  await refreshOrganizations()
}

async function setup(data: { organization_name: string; display_name: string; email: string; password: string }) {
  const actor = await authRequest('/auth/setup', 'POST', data)
  state.setupRequired = false
  setActor(actor)
  await refreshOrganizations()
}

async function logout() {
  try {
    await authRequest('/auth/logout', 'POST')
  } finally {
    setActor(null)
	state.organizations = []
  }
}

async function switchOrganization(organizationId: number) {
  const actor = await authRequest('/auth/switch-organization', 'POST', { organization_id: organizationId })
  setActor(actor)
  await refreshOrganizations()
}

async function changePassword(currentPassword: string, newPassword: string) {
  const actor = await authRequest('/auth/change-password', 'POST', { current_password: currentPassword, new_password: newPassword })
  setActor(actor)
  await refreshOrganizations()
}

export function handleUnauthorized() {
  if (state.enabled) setActor(null)
}

export const authStore = { state, initialize, login, setup, logout, switchOrganization, refreshOrganizations, changePassword }
