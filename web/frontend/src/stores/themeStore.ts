import { create } from 'zustand'

type Theme = 'light' | 'dark'

interface ThemeState {
  theme: Theme
  toggle: () => void
  init: () => void
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  theme: 'light',

  toggle: () => {
    const next = get().theme === 'light' ? 'dark' : 'light'
    localStorage.setItem('theme', next)
    document.documentElement.classList.toggle('dark', next === 'dark')
    set({ theme: next })
  },

  init: () => {
    const saved = localStorage.getItem('theme') as Theme | null
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const theme = saved || (media.matches ? 'dark' : 'light')
    document.documentElement.classList.toggle('dark', theme === 'dark')
    set({ theme })

    // Follow the system while the user has not chosen for themselves. Read
    // once, this left someone who switched their OS to dark mid-session
    // staring at a light page.
    media.addEventListener('change', (e) => {
      if (localStorage.getItem('theme')) return
      const next: Theme = e.matches ? 'dark' : 'light'
      document.documentElement.classList.toggle('dark', next === 'dark')
      set({ theme: next })
    })
  },
}))
