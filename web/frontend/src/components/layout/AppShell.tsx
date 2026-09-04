import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  PlayCircle,
  ListChecks,
  FolderSync,
  Clock,
  History,
  FileSpreadsheet,
  KeyRound,
  Settings,
  LogOut,
  Sun,
  Moon,
  Mail,
  Menu,
  X,
} from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'
import { useThemeStore } from '@/stores/themeStore'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

// Grouped so the sidebar reads as three jobs — run a sync, set one up, admin —
// rather than nine equally weighted links.
const navGroups = [
  {
    label: 'Operate',
    items: [
      { path: '/', label: 'Dashboard', icon: LayoutDashboard },
      { path: '/sync/new', label: 'New Sync', icon: PlayCircle },
      { path: '/jobs', label: 'Running Jobs', icon: ListChecks },
      { path: '/history', label: 'History', icon: History },
    ],
  },
  {
    label: 'Configure',
    items: [
      { path: '/profiles', label: 'Profiles', icon: FolderSync },
      { path: '/schedules', label: 'Schedules', icon: Clock },
      { path: '/batch', label: 'Batch Import', icon: FileSpreadsheet },
    ],
  },
  {
    label: 'Admin',
    items: [
      { path: '/api-access', label: 'API Access', icon: KeyRound },
      { path: '/settings', label: 'Settings', icon: Settings },
    ],
  },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { user, logout } = useAuthStore()
  const { theme, toggle } = useThemeStore()
  const [mobileOpen, setMobileOpen] = useState(false)

  // Navigating closes the drawer; leaving it open hides the page just asked for.
  // This is exactly the "reset state when a prop changes" case React documents,
  // and the alternative -- a key on the drawer -- would remount it mid-animation.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setMobileOpen(false), [location.pathname])

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  const sidebar = (
    <div className="flex h-full flex-col bg-sidebar">
      <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
        <span className="rounded-md bg-primary p-1.5" aria-hidden>
          <Mail className="h-4 w-4 text-primary-foreground" />
        </span>
        <div className="leading-tight">
          <p className="text-sm font-semibold">imapsync</p>
          <p className="text-[11px] text-muted-foreground">Web UI</p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto h-8 w-8 lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-label="Close navigation"
        >
          <X className="h-4 w-4" />
        </Button>
      </div>

      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-3" aria-label="Main">
        {navGroups.map((group) => (
          <div key={group.label}>
            <p className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
              {group.label}
            </p>
            <div className="space-y-0.5">
              {group.items.map(({ path, label, icon: Icon }) => {
                const active = location.pathname === path
                return (
                  <Link
                    key={path}
                    to={path}
                    aria-current={active ? 'page' : undefined}
                    className={cn(
                      // A tint plus a left rule marks the current page without a
                      // heavy solid fill drowning the icon.
                      'relative flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors',
                      active
                        ? 'bg-primary-soft font-medium text-primary'
                        : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                    )}
                  >
                    {active && (
                      <span
                        aria-hidden
                        className="absolute bottom-1.5 left-0 top-1.5 w-0.5 rounded-full bg-primary"
                      />
                    )}
                    <Icon className="h-4 w-4 shrink-0" />
                    {label}
                  </Link>
                )
              })}
            </div>
          </div>
        ))}
      </nav>

      <div className="border-t border-border p-3">
        <div className="flex items-center gap-2">
          <span
            className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-primary-soft text-xs font-semibold text-primary"
            aria-hidden
          >
            {user?.username?.slice(0, 2).toUpperCase()}
          </span>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{user?.username}</p>
            <p className="truncate text-[11px] text-muted-foreground">
              {user?.is_admin ? 'Admin' : 'User'}
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={toggle}
            className="h-8 w-8"
            aria-label={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'}
          >
            {theme === 'light' ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={handleLogout}
            className="h-8 w-8"
            aria-label="Sign out"
          >
            <LogOut className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  )

  return (
    <div className="flex min-h-svh">
      <aside className="hidden w-60 shrink-0 border-r border-border lg:block">
        <div className="sticky top-0 h-svh">{sidebar}</div>
      </aside>

      {mobileOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            className="absolute inset-0 bg-black/50"
            onClick={() => setMobileOpen(false)}
            aria-label="Close navigation"
          />
          <div className="relative h-full w-64 border-r border-border shadow-xl">{sidebar}</div>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-2 border-b border-border bg-card px-4 lg:hidden">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setMobileOpen(true)}
            aria-label="Open navigation"
          >
            <Menu className="h-5 w-5" />
          </Button>
          <span className="text-sm font-semibold">imapsync</span>
        </header>

        <main className="min-w-0 flex-1 p-4 sm:p-6">{children}</main>
      </div>
    </div>
  )
}
