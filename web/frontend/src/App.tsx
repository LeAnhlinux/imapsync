import { useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useAuthStore } from '@/stores/authStore'
import { useThemeStore } from '@/stores/themeStore'
import { AppShell } from '@/components/layout/AppShell'
import { LoginPage } from '@/pages/LoginPage'
import { DashboardPage } from '@/pages/DashboardPage'
import { NewSyncPage } from '@/pages/NewSyncPage'
import { RunningJobsPage } from '@/pages/RunningJobsPage'
import { ProfilesPage } from '@/pages/ProfilesPage'
import { HistoryPage } from '@/pages/HistoryPage'
import { SchedulesPage } from '@/pages/SchedulesPage'
import { BatchImportPage } from '@/pages/BatchImportPage'
import { SettingsPage } from '@/pages/SettingsPage'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuthStore()

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <p className="text-muted-foreground">Loading...</p>
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return <AppShell>{children}</AppShell>
}

export default function App() {
  const init = useAuthStore((s) => s.init)
  const initTheme = useThemeStore((s) => s.init)

  useEffect(() => {
    initTheme()
    init()
  }, [init, initTheme])

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<ProtectedRoute><DashboardPage /></ProtectedRoute>} />
        <Route path="/sync/new" element={<ProtectedRoute><NewSyncPage /></ProtectedRoute>} />
        <Route path="/jobs" element={<ProtectedRoute><RunningJobsPage /></ProtectedRoute>} />
        <Route path="/profiles" element={<ProtectedRoute><ProfilesPage /></ProtectedRoute>} />
        <Route path="/schedules" element={<ProtectedRoute><SchedulesPage /></ProtectedRoute>} />
        <Route path="/history" element={<ProtectedRoute><HistoryPage /></ProtectedRoute>} />
        <Route path="/batch" element={<ProtectedRoute><BatchImportPage /></ProtectedRoute>} />
        <Route path="/settings" element={<ProtectedRoute><SettingsPage /></ProtectedRoute>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
