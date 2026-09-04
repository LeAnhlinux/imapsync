import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (res) => res,
  (err) => {
    // A 401 from /auth/login means "wrong password", not "your session ended".
    // Redirecting on it reloaded the page the user was already on, which wiped
    // the error message the login form had just set — so a mistyped password
    // silently blanked the form and said nothing at all.
    const isAuthCall = err.config?.url?.startsWith('/auth/')
    if (err.response?.status === 401 && !isAuthCall) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

// Auth
export const authApi = {
  register: (data: { username: string; email: string; password: string }) =>
    api.post('/auth/register', data),
  login: (data: { username: string; password: string }) =>
    api.post<{ access_token: string; token_type: string }>('/auth/login', data),
  me: () => api.get('/auth/me'),
}

// Profiles
export const profileApi = {
  list: () => api.get('/profiles'),
  get: (id: number) => api.get(`/profiles/${id}`),
  create: (data: Record<string, unknown>) => api.post('/profiles', data),
  update: (id: number, data: Record<string, unknown>) => api.put(`/profiles/${id}`, data),
  delete: (id: number) => api.delete(`/profiles/${id}`),
}

// Sync
export const syncApi = {
  start: (data: Record<string, unknown>) => api.post('/sync/start', data),
  running: () => api.get('/sync/running'),
  get: (id: number) => api.get(`/sync/${id}`),
  abort: (id: number) => api.post(`/sync/${id}/abort`),
  log: (id: number) => api.get(`/sync/${id}/log`),
  delete: (id: number) => api.delete(`/sync/${id}`),
}

// History
export const historyApi = {
  list: (params?: Record<string, unknown>) => api.get('/history', { params }),
  stats: () => api.get('/history/stats'),
  // Deletes finished jobs older than the cutoff, and their logs.
  purge: (olderThanDays: number) =>
    api.post<{ deleted: number }>('/history/purge', { older_than_days: olderThanDays }),
  getRetention: () => api.get<Retention>('/history/retention'),
  saveRetention: (data: { enabled: boolean; keep_days: number }) =>
    api.put<Retention>('/history/retention', data),
  runRetention: () => api.post<{ deleted: number }>('/history/retention/run'),
}

export interface Retention {
  enabled: boolean
  keep_days: number
  last_run_at: string | null
  last_deleted: number
}

// Schedules
export const scheduleApi = {
  list: () => api.get('/schedules'),
  create: (data: Record<string, unknown>) => api.post('/schedules', data),
  update: (id: number, data: Record<string, unknown>) => api.put(`/schedules/${id}`, data),
  delete: (id: number) => api.delete(`/schedules/${id}`),
}

// Folders
export interface FolderInfo {
  name: string
  messages: number
  size: number
}

export const folderApi = {
  // Either profile_id, or host/user/password. Slow: it opens a real IMAP
  // connection and SELECTs every folder.
  browse: (data: Record<string, unknown>) => api.post<FolderInfo[]>('/folders/browse', data),
}

// Batch
export const batchApi = {
  upload: (file: File, dryRun = false, ssl1 = true, ssl2 = true, automap = true, usecache = true) => {
    const form = new FormData()
    form.append('file', file)
    return api.post(`/batch/upload?dry_run=${dryRun}&ssl1=${ssl1}&ssl2=${ssl2}&automap=${automap}&usecache=${usecache}`, form)
  },
  uploadText: (text: string, dryRun = false, ssl1 = true, ssl2 = true, automap = true, usecache = true) =>
    api.post(`/batch/upload-text?dry_run=${dryRun}&ssl1=${ssl1}&ssl2=${ssl2}&automap=${automap}&usecache=${usecache}`, { csv_text: text }),
  start: (id: number) => api.post(`/batch/${id}/start`),
  abort: (id: number) => api.post(`/batch/${id}/abort`),
  get: (id: number) => api.get(`/batch/${id}`),
}

// Telegram
export interface TelegramSettings {
  enabled: boolean
  has_token: boolean
  allowed_chat_ids: string
  user_id: number | null
  notify_on_success: boolean
  notify_on_failure: boolean
  notify_on_progress: boolean
  progress_step: number
}

export interface TelegramStatus {
  running: boolean
  configured: boolean
  bot_username: string | null
  error: string | null
}

export const telegramApi = {
  get: () => api.get<TelegramSettings>('/telegram/settings'),
  update: (data: Record<string, unknown>) => api.put<TelegramSettings>('/telegram/settings', data),
  status: () => api.get<TelegramStatus>('/telegram/status'),
  test: () => api.post<{ status: string; detail?: string }>('/telegram/test'),
}

// API keys
export interface ApiKey {
  id: number
  name: string
  prefix: string
  scopes: string
  rate_limit_per_minute: number
  expires_at: string | null
  last_used_at: string | null
  revoked: boolean
  created_at: string
}

export const apiKeyApi = {
  list: () => api.get<ApiKey[]>('/api-keys'),
  scopes: () => api.get<string[]>('/api-keys/scopes'),
  // The key itself comes back exactly once, here.
  create: (data: { name: string; scopes: string[]; rate_limit_per_minute: number; expires_in_days: number }) =>
    api.post<ApiKey & { key: string }>('/api-keys', data),
  revoke: (id: number) => api.post(`/api-keys/${id}/revoke`),
  delete: (id: number) => api.delete(`/api-keys/${id}`),
}

// Webhooks
export interface Webhook {
  id: number
  url: string
  events: string
  is_active: boolean
  last_status: number | null
  last_called_at: string | null
  created_at: string
}

export const webhookApi = {
  list: () => api.get<Webhook[]>('/webhooks'),
  events: () => api.get<string[]>('/webhooks/events'),
  create: (data: { url: string; events: string[] }) =>
    api.post<Webhook & { secret: string }>('/webhooks', data),
  update: (id: number, data: Record<string, unknown>) => api.put(`/webhooks/${id}`, data),
  delete: (id: number) => api.delete(`/webhooks/${id}`),
  test: (id: number) => api.post<{ status: string; http_status?: number; detail?: string }>(`/webhooks/${id}/test`),
}

// Notifications
export const notificationApi = {
  get: () => api.get('/notifications/settings'),
  update: (data: Record<string, unknown>) => api.put('/notifications/settings', data),
  test: () => api.post('/notifications/test'),
}

export default api
