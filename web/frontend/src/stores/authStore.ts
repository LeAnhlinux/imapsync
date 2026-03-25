import { create } from 'zustand'
import { authApi } from '../lib/api'

interface User {
  id: number
  username: string
  email: string
  is_admin: boolean
}

interface AuthState {
  user: User | null
  token: string | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  register: (username: string, email: string, password: string) => Promise<void>
  logout: () => void
  fetchMe: () => Promise<void>
  init: () => Promise<void>
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  token: localStorage.getItem('token'),
  loading: true,

  login: async (username, password) => {
    const res = await authApi.login({ username, password })
    const token = res.data.access_token
    localStorage.setItem('token', token)
    set({ token })
    const me = await authApi.me()
    set({ user: me.data })
  },

  register: async (username, email, password) => {
    await authApi.register({ username, email, password })
  },

  logout: () => {
    localStorage.removeItem('token')
    set({ user: null, token: null })
  },

  fetchMe: async () => {
    try {
      const res = await authApi.me()
      set({ user: res.data })
    } catch {
      localStorage.removeItem('token')
      set({ user: null, token: null })
    }
  },

  init: async () => {
    const token = localStorage.getItem('token')
    if (token) {
      try {
        const res = await authApi.me()
        set({ user: res.data, token, loading: false })
      } catch {
        localStorage.removeItem('token')
        set({ user: null, token: null, loading: false })
      }
    } else {
      set({ loading: false })
    }
  },
}))
