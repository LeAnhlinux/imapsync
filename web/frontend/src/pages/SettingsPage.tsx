import { useEffect, useState } from 'react'
import { notificationApi } from '@/lib/api'
import { useThemeStore } from '@/stores/themeStore'
import { useAuthStore } from '@/stores/authStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

export function SettingsPage() {
  const { theme, toggle } = useThemeStore()
  const { user } = useAuthStore()

  const [smtp, setSmtp] = useState({
    smtp_host: '', smtp_port: 587, smtp_user: '', smtp_password: '',
    from_email: '', to_email: '', notify_on_success: false, notify_on_failure: true,
  })
  const [saved, setSaved] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)

  useEffect(() => {
    notificationApi.get().then((r) => {
      if (r.data) {
        setSmtp({
          smtp_host: r.data.smtp_host || '',
          smtp_port: r.data.smtp_port || 587,
          smtp_user: r.data.smtp_user || '',
          smtp_password: '',
          from_email: r.data.from_email || '',
          to_email: r.data.to_email || '',
          notify_on_success: r.data.notify_on_success,
          notify_on_failure: r.data.notify_on_failure,
        })
      }
    }).catch(() => {})
  }, [])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    const data: Record<string, unknown> = { ...smtp }
    if (!smtp.smtp_password) delete data.smtp_password
    await notificationApi.update(data)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const handleTest = async () => {
    setTestResult(null)
    const res = await notificationApi.test()
    setTestResult(res.data.status === 'ok' ? 'Test email sent!' : 'Failed to send test email')
  }

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      <h2 className="text-2xl font-bold">Settings</h2>

      {/* Account */}
      <Card>
        <CardHeader><CardTitle>Account</CardTitle></CardHeader>
        <CardContent className="text-sm space-y-1">
          <p><strong>Username:</strong> {user?.username}</p>
          <p><strong>Email:</strong> {user?.email}</p>
          <p><strong>Role:</strong> {user?.is_admin ? 'Admin' : 'User'}</p>
        </CardContent>
      </Card>

      {/* Theme */}
      <Card>
        <CardHeader><CardTitle>Appearance</CardTitle></CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <span className="text-sm">Theme: {theme}</span>
            <Button variant="outline" size="sm" onClick={toggle}>
              Switch to {theme === 'light' ? 'Dark' : 'Light'}
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Notifications */}
      <Card>
        <CardHeader><CardTitle>Email Notifications</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleSave} className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium">SMTP Host</label>
                <Input value={smtp.smtp_host} onChange={(e) => setSmtp({ ...smtp, smtp_host: e.target.value })} placeholder="smtp.gmail.com" />
              </div>
              <div>
                <label className="text-sm font-medium">SMTP Port</label>
                <Input type="number" value={smtp.smtp_port} onChange={(e) => setSmtp({ ...smtp, smtp_port: Number(e.target.value) })} />
              </div>
              <div>
                <label className="text-sm font-medium">SMTP User</label>
                <Input value={smtp.smtp_user} onChange={(e) => setSmtp({ ...smtp, smtp_user: e.target.value })} />
              </div>
              <div>
                <label className="text-sm font-medium">SMTP Password</label>
                <Input type="password" value={smtp.smtp_password} onChange={(e) => setSmtp({ ...smtp, smtp_password: e.target.value })} placeholder="Leave empty to keep current" />
              </div>
              <div>
                <label className="text-sm font-medium">From Email</label>
                <Input value={smtp.from_email} onChange={(e) => setSmtp({ ...smtp, from_email: e.target.value })} />
              </div>
              <div>
                <label className="text-sm font-medium">To Email</label>
                <Input value={smtp.to_email} onChange={(e) => setSmtp({ ...smtp, to_email: e.target.value })} />
              </div>
            </div>
            <div className="flex gap-4">
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={smtp.notify_on_success} onChange={(e) => setSmtp({ ...smtp, notify_on_success: e.target.checked })} />
                Notify on success
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={smtp.notify_on_failure} onChange={(e) => setSmtp({ ...smtp, notify_on_failure: e.target.checked })} />
                Notify on failure
              </label>
            </div>
            <div className="flex gap-2">
              <Button type="submit">{saved ? 'Saved!' : 'Save'}</Button>
              <Button type="button" variant="outline" onClick={handleTest}>Send Test Email</Button>
            </div>
            {testResult && <p className="text-sm text-muted-foreground">{testResult}</p>}
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
