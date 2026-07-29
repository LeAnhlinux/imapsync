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
    if (err.response?.status === 401) {
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
}

// History
export const historyApi = {
  list: (params?: Record<string, unknown>) => api.get('/history', { params }),
  stats: () => api.get('/history/stats'),
}

// Schedules
export const scheduleApi = {
  list: () => api.get('/schedules'),
  create: (data: Record<string, unknown>) => api.post('/schedules', data),
  update: (id: number, data: Record<string, unknown>) => api.put(`/schedules/${id}`, data),
  delete: (id: number) => api.delete(`/schedules/${id}`),
}

// Folders
export const folderApi = {
  browse: (data: { host: string; user: string; password: string; ssl: boolean }) =>
    api.post('/folders/browse', data),
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
  get: (id: number) => api.get(`/batch/${id}`),
}

// Notifications
export const notificationApi = {
  get: () => api.get('/notifications/settings'),
  update: (data: Record<string, unknown>) => api.put('/notifications/settings', data),
  test: () => api.post('/notifications/test'),
}

export default api
