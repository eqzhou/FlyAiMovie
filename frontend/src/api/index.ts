import { authStore, handleUnauthorized } from '../auth'

const BASE = '/api/v1'

async function req<T = any>(method: string, path: string, body?: any): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (method !== 'GET' && authStore.state.csrfToken) headers['X-CSRF-Token'] = authStore.state.csrfToken
  const opts: RequestInit = { method, credentials: 'include', headers }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const resp = await fetch(`${BASE}${path}`, opts)
  const json = await resp.json()
  if (resp.status === 401) handleUnauthorized()
  if (!resp.ok || (json.code && json.code >= 400)) {
    throw new Error(json.message || `${resp.status}`)
  }
  return (json.data ?? json) as T
}

export const api = {
  get: <T = any>(p: string) => req<T>('GET', p),
  post: <T = any>(p: string, b?: any) => req<T>('POST', p, b),
  put: <T = any>(p: string, b?: any) => req<T>('PUT', p, b),
  del: <T = any>(p: string, b?: any) => req<T>('DELETE', p, b),
}

export const uploadAPI = {
  image: async (file: File, fields: Record<string, string | number>) => {
    const form = new FormData()
    form.append('file', file)
    for (const [key, value] of Object.entries(fields)) form.append(key, String(value))
    const headers: Record<string, string> = {}
    if (authStore.state.csrfToken) headers['X-CSRF-Token'] = authStore.state.csrfToken
    const resp = await fetch(`${BASE}/upload/image`, { method: 'POST', credentials: 'include', headers, body: form })
    const json = await resp.json()
    if (resp.status === 401) handleUnauthorized()
    if (!resp.ok || (json.code && json.code >= 400)) throw new Error(json.message || `${resp.status}`)
    return json.data ?? json
  },
}

export const dramaAPI = {
  list: () => api.get<{ items: any[] }>('/dramas'),
  get: (id: number) => api.get(`/dramas/${id}`),
  create: (data: any) => api.post('/dramas', data),
  update: (id: number, data: any) => api.put(`/dramas/${id}`, data),
  del: (id: number) => api.del(`/dramas/${id}`),
}

export const episodeAPI = {
  create: (data: any) => api.post('/episodes', data),
  update: (id: number, data: any) => api.put(`/episodes/${id}`, data),
  characters: (id: number) => api.get(`/episodes/${id}/characters`),
  scenes: (id: number) => api.get(`/episodes/${id}/scenes`),
  storyboards: (id: number) => api.get(`/episodes/${id}/storyboards`),
  pipelineStatus: (id: number) => api.get(`/episodes/${id}/pipeline-status`),
}

export const storyboardAPI = {
  create: (data: any) => api.post('/storyboards', data),
  update: (id: number, data: any) => api.put(`/storyboards/${id}`, data),
  generateTTS: (id: number) => api.post(`/storyboards/${id}/generate-tts`),
  generateFrame: (id: number, data: any) => api.post(`/storyboards/${id}/generate-frame`, data),
  generateVideo: (id: number, data?: any) => api.post(`/storyboards/${id}/generate-video`, data || {}),
  batchFrames: (data: any) => api.post('/storyboards/batch-generate-frames', data),
  batchVideos: (data: any) => api.post('/storyboards/batch-generate-videos', data),
  batchTTS: (data: any) => api.post('/storyboards/batch-generate-tts', data),
  del: (id: number) => api.del(`/storyboards/${id}`),
}

export const characterAPI = {
  create: (data: any) => api.post('/characters', data),
  update: (id: number, data: any) => api.put(`/characters/${id}`, data),
  voiceSample: (id: number, episodeId: number) =>
    api.post(`/characters/${id}/generate-voice-sample`, { episode_id: episodeId }),
  generateImage: (id: number, episodeId: number) =>
    api.post(`/characters/${id}/generate-image`, { episode_id: episodeId }),
  batchImages: (ids: number[], episodeId: number) =>
    api.post('/characters/batch-generate-images', { character_ids: ids, episode_id: episodeId }),
  del: (id: number) => api.del(`/characters/${id}`),
}

export const sceneAPI = {
  create: (data: any) => api.post('/scenes', data),
  generateImage: (id: number, episodeId: number) =>
    api.post(`/scenes/${id}/generate-image`, { episode_id: episodeId }),
  update: (id: number, data: any) => api.put(`/scenes/${id}`, data),
  del: (id: number) => api.del(`/scenes/${id}`),
}

export const imageAPI = {
  generate: (d: any) => api.post('/images', d),
  get: (id: number) => api.get(`/images/${id}`),
  list: (params?: { drama_id?: number; storyboard_id?: number }) => {
    const q = new URLSearchParams()
    if (params?.drama_id) q.set('drama_id', String(params.drama_id))
    if (params?.storyboard_id) q.set('storyboard_id', String(params.storyboard_id))
    return api.get(`/images${q.size ? `?${q}` : ''}`)
  },
}

export const gridAPI = {
  prompt: (d: any) => api.post('/grid/prompt', d),
  generate: (d: any) => api.post('/grid/generate', d),
  status: (id: number) => api.get(`/grid/status/${id}`),
  split: (d: any) => api.post('/grid/split', d),
  history: (params?: { episode_id?: number; drama_id?: number }) => {
    const q = new URLSearchParams()
    if (params?.episode_id) q.set('episode_id', String(params.episode_id))
    if (params?.drama_id) q.set('drama_id', String(params.drama_id))
    return api.get(`/grid/history${q.size ? `?${q}` : ''}`)
  },
  historyGet: (id: number) => api.get(`/grid/history/${id}`),
}

export const videoAPI = {
  generate: (d: any) => api.post('/videos', d),
  get: (id: number) => api.get(`/videos/${id}`),
  list: (params?: { drama_id?: number; storyboard_id?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.drama_id) q.set('drama_id', String(params.drama_id))
    if (params?.storyboard_id) q.set('storyboard_id', String(params.storyboard_id))
    if (params?.status) q.set('status', params.status)
    return api.get(`/videos${q.size ? `?${q}` : ''}`)
  },
}

export const composeAPI = {
  shot: (id: number) => api.post(`/compose/storyboards/${id}/compose`),
  all: (epId: number) => api.post(`/compose/episodes/${epId}/compose-all`),
  status: (epId: number) => api.get(`/compose/episodes/${epId}/compose-status`),
}

export const mergeAPI = {
  merge: (epId: number) => api.post(`/merge/episodes/${epId}/merge`),
  status: (epId: number) => api.get(`/merge/episodes/${epId}/merge`),
}

export const jobsAPI = {
  list: (params?: { status?: string; kind?: string; limit?: number }) => {
    const q = new URLSearchParams()
    if (params?.status) q.set('status', params.status)
    if (params?.kind) q.set('kind', params.kind)
    if (params?.limit) q.set('limit', String(params.limit))
    return api.get(`/jobs${q.size ? `?${q}` : ''}`)
  },
  get: (id: number) => api.get(`/jobs/${id}`),
  cancel: (id: number) => api.post(`/jobs/${id}/cancel`),
  retry: (id: number) => api.post(`/jobs/${id}/retry`),
}

export const agentAPI = {
  chat: (type: string, data: any) => api.post(`/agent/${type}/chat`, data),
}

export const settingsAPI = {
  aiConfigs: () => api.get('/ai-configs'),
  createAIConfig: (d: any) => api.post('/ai-configs', d),
  updateAIConfig: (id: number, d: any) => api.put(`/ai-configs/${id}`, d),
  deleteAIConfig: (id: number) => api.del(`/ai-configs/${id}`),
  providers: () => api.get('/ai-providers'),
  agentConfigs: () => api.get('/agent-configs'),
  upsertAgentConfig: (d: any) => api.post('/agent-configs', d),
  voices: () => api.get('/ai-voices'),
  syncVoices: () => api.post('/ai-voices/sync'),
}

export const auditAPI = {
  list: (params?: { page?: number; page_size?: number; action?: string; resource_type?: string }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.action) q.set('action', params.action)
    if (params?.resource_type) q.set('resource_type', params.resource_type)
    return api.get<{ items: any[]; pagination: { page: number; page_size: number; total: number } }>(`/audit-logs${q.size ? `?${q}` : ''}`)
  },
}

export const quotaAPI = {
  get: () => api.get<{ daily_job_limit: number; max_active_jobs: number; daily_jobs_used: number; active_jobs: number }>('/organization/quota'),
  update: (data: { daily_job_limit: number; max_active_jobs: number }) => api.put('/organization/quota', data),
}

export const memberAPI = {
  list: () => api.get<any[]>('/organization/members'),
  create: (data: { email: string; display_name?: string; password?: string; role: string }) => api.post('/organization/members', data),
  invitations: () => api.get<any[]>('/organization/members/invitations'),
  invite: (data: { email: string; role: string; ttl_hours?: number }) => api.post<any>('/organization/members/invitations', data),
  revokeInvitation: (id: number) => api.del(`/organization/members/invitations/${id}`),
  resendInvitation: (id: number) => api.post<any>(`/organization/members/invitations/${id}/resend`),
  update: (userId: number, role: string) => api.put(`/organization/members/${userId}`, { role }),
  remove: (userId: number) => api.del(`/organization/members/${userId}`),
}

export const invitationAPI = {
  get: (token: string) => api.get<any>(`/auth/invitations/${encodeURIComponent(token)}`),
  accept: (token: string, data: { email: string; current_password?: string; new_password?: string; display_name?: string }) =>
    api.post<any>(`/auth/invitations/${encodeURIComponent(token)}/accept`, data),
}

export const passwordResetAPI = {
  request: (email: string) => api.post('/auth/password-reset/request', { email }),
  consume: (token: string, newPassword: string) => api.post('/auth/password-reset/consume', { token, new_password: newPassword }),
}

export const organizationDataAPI = {
  export: () => api.get<any>('/organization/export'),
  remove: (password: string, confirmation: string) => api.del('/organization', { password, confirmation }),
}


export const propAPI = {
  list: (dramaId: number) => api.get(`/props?drama_id=${dramaId}`),
  create: (d: any) => api.post('/props', d),
  update: (id: number, d: any) => api.put(`/props/${id}`, d),
  del: (id: number) => api.del(`/props/${id}`),
  generateImage: (id: number, episodeId?: number) =>
    api.post(`/props/${id}/generate-image`, episodeId ? { episode_id: episodeId } : {}),
}

export const assetAPI = {
  list: (params?: { drama_id?: number; episode_id?: number; storyboard_id?: number; type?: string; category?: string }) => {
    const q = new URLSearchParams()
    for (const key of ['drama_id', 'episode_id', 'storyboard_id', 'type', 'category'] as const) {
      const value = params?.[key]
      if (value !== undefined && value !== '') q.set(key, String(value))
    }
    return api.get(`/assets${q.size ? `?${q}` : ''}`)
  },
  get: (id: number) => api.get(`/assets/${id}`),
  create: (data: any) => api.post('/assets', data),
  update: (id: number, data: any) => api.put(`/assets/${id}`, data),
  del: (id: number) => api.del(`/assets/${id}`),
  apply: (id: number, data: { storyboard_id: number; frame_type: string }) => api.post(`/assets/${id}/apply`, data),
}
