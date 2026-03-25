import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  PlayCircle,
  ListChecks,
  FolderSync,
  Clock,
  FileSpreadsheet,
  Settings,
  LogOut,
  Sun,
  Moon,
  Mail,
} from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'
import { useThemeStore } from '@/stores/themeStore'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

const navItems = [
  { path: '/', label: 'Dashboard', icon: LayoutDashboard },
  { path: '/sync/new', label: 'New Sync', icon: PlayCircle },
  { path: '/jobs', label: 'Running Jobs', icon: ListChecks },
  { path: '/profiles', label: 'Profiles', icon: FolderSync },
  { path: '/schedules', label: 'Schedules', icon: Clock },
  { path: '/history', label: 'History', icon: FileSpreadsheet },
  { path: '/batch', label: 'Batch Import', icon: FileSpreadsheet },
  { path: '/settings', label: 'Settings', icon: Settings },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { user, logout } = useAuthStore()
  const { theme, toggle } = useThemeStore()

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="w-64 border-r border-border bg-card flex flex-col">
        <div className="p-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Mail className="h-6 w-6 text-primary" />
            <h1 className="text-lg font-bold">imapsync</h1>
          </div>
          <p className="text-xs text-muted-foreground mt-1">Web UI</p>
        </div>

        <nav className="flex-1 p-2 space-y-1">
          {navItems.map(({ path, label, icon: Icon }) => (
            <Link
              key={path}
              to={path}
              className={cn(
                'flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors',
                location.pathname === path
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              )}
            >
              <Icon className="h-4 w-4" />
              {label}
            </Link>
          ))}
        </nav>

        <div className="p-4 border-t border-border space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">{user?.username}</span>
            <div className="flex gap-1">
              <Button variant="ghost" size="icon" onClick={toggle} className="h-8 w-8">
                {theme === 'light' ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
              </Button>
              <Button variant="ghost" size="icon" onClick={handleLogout} className="h-8 w-8">
                <LogOut className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 p-6 overflow-auto">
        {children}
      </main>
    </div>
  )
}
