import { reactive } from 'vue'

const BASE = '/api/v1'

type AuthActor = {
  user: { id: number; email: string; display_name: string; is_platform_admin?: boolean }
  organization: { id: number; name: string; slug: string }
  role: 'owner' | 'admin' | 'editor' | 'viewer'
  csrf_token?: string
}

const state = reactive({
  initialized: false,
  enabled: false,
  setupRequired: false,
  registrationEnabled: true,
  requireEmailVerification: false,
  actor: null as AuthActor | null,
  csrfToken: sessionStorage.getItem('flyaimovie.csrf') || '',
  organizations: [] as Array<{ id: number; name: string; slug: string; role: string; current: boolean }>,
})
let initializePromise: Promise<void> | null = null

async function parseResponseJSON(response: Response): Promise<Record<string, any>> {
  const text = await response.text()
  if (!text) return {}
  try {
    return JSON.parse(text)
  } catch {
    return { message: text.slice(0, 200) }
  }
}

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
  const payload = await parseResponseJSON(response)
  if (!response.ok) throw new Error(payload.message || `请求失败（${response.status}）`)
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

async function refreshOrganizationsBestEffort() {
  try {
    await refreshOrganizations()
  } catch (cause) {
    // 主操作（登录、切组织等）已经提交成功，列表刷新失败不能伪装成提交失败。
    state.organizations = state.actor ? [{
      id: state.actor.organization.id,
      name: state.actor.organization.name,
      slug: state.actor.organization.slug,
      role: state.actor.role,
      current: true,
    }] : []
    console.warn('organization list refresh failed', cause)
  }
}

async function performInitialize() {
    const status = await authRequest('/auth/status')
    state.enabled = Boolean(status.enabled)
    state.setupRequired = Boolean(status.setup_required)
    state.registrationEnabled = status.registration_enabled !== false
    state.requireEmailVerification = Boolean(status.require_email_verification)
    if (state.enabled && !state.setupRequired) {
      try {
        setActor(await authRequest('/auth/me'))
        await refreshOrganizationsBestEffort()
      } catch {
        setActor(null)
      }
    }
  state.initialized = true
}

async function initialize() {
  if (state.initialized) return
  if (!initializePromise) {
    initializePromise = performInitialize().finally(() => { initializePromise = null })
  }
  await initializePromise
}

async function login(email: string, password: string) {
  const actor = await authRequest('/auth/login', 'POST', { email, password })
  state.setupRequired = false
  setActor(actor)
  await refreshOrganizationsBestEffort()
}

async function setup(data: { organization_name: string; display_name: string; email: string; password: string }) {
  const actor = await authRequest('/auth/setup', 'POST', data)
  state.setupRequired = false
  setActor(actor)
  await refreshOrganizationsBestEffort()
}

export class RegistrationVerificationRequiredError extends Error {
  email: string

  constructor(email: string) {
    super('verification_required')
    this.name = 'RegistrationVerificationRequiredError'
    this.email = email
  }
}

async function register(data: { organization_name: string; display_name: string; email: string; password: string }) {
  const result = await authRequest('/auth/register', 'POST', data)
  if (result?.verification_required) {
    throw new RegistrationVerificationRequiredError(result.email || data.email)
  }
  setActor(result)
  await refreshOrganizationsBestEffort()
}

async function logout() {
  try {
    await authRequest('/auth/logout', 'POST')
  } catch (cause) {
    // 网络异常时也要完成本地退出，避免用户卡在已失效的会话上。
    console.warn('logout request failed', cause)
  } finally {
    setActor(null)
    state.organizations = []
  }
}

async function adoptActor(actor: AuthActor) {
  state.setupRequired = false
  setActor(actor)
  await refreshOrganizationsBestEffort()
}

async function switchOrganization(organizationId: number) {
  const actor = await authRequest('/auth/switch-organization', 'POST', { organization_id: organizationId })
  setActor(actor)
  await refreshOrganizationsBestEffort()
}

async function changePassword(currentPassword: string, newPassword: string) {
  const actor = await authRequest('/auth/change-password', 'POST', { current_password: currentPassword, new_password: newPassword })
  setActor(actor)
  await refreshOrganizationsBestEffort()
}

export function authSessionFingerprint() {
  return `${state.actor?.user.id || 0}:${state.actor?.organization.id || 0}:${state.csrfToken}`
}

export function handleUnauthorized(requestSession = authSessionFingerprint()) {
  // 忽略旧登录态或旧组织中迟到的 401，避免清掉刚建立的新会话。
  if (state.enabled && requestSession === authSessionFingerprint()) setActor(null)
}

export const authStore = { state, initialize, login, setup, register, logout, adoptActor, switchOrganization, refreshOrganizations, changePassword }
