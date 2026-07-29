import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertCircle, Loader2, Mail } from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export function LoginPage() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { login } = useAuthStore()
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await login(username, password)
      navigate('/')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not sign in. Check your connection and try again.',
      )
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="grid min-h-svh lg:grid-cols-2">
      <div className="flex items-center justify-center p-6">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex items-center gap-2.5">
            <span className="rounded-md bg-primary p-2" aria-hidden>
              <Mail className="h-5 w-5 text-primary-foreground" />
            </span>
            <div className="leading-tight">
              <p className="font-semibold">imapsync</p>
              <p className="text-xs text-muted-foreground">Web UI</p>
            </div>
          </div>

          <h1 className="text-xl font-semibold tracking-tight">Sign in</h1>
          <p className="mb-6 mt-1 text-sm text-muted-foreground">
            Use the account created with <code className="font-mono text-xs">createadmin</code>.
          </p>

          <form onSubmit={handleSubmit} className="space-y-4" noValidate>
            <div>
              <label className="mb-1.5 block text-sm font-medium" htmlFor="username">
                Username
              </label>
              <Input
                id="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                autoFocus
                autoComplete="username"
                aria-invalid={!!error}
              />
            </div>
            <div>
              <label className="mb-1.5 block text-sm font-medium" htmlFor="password">
                Password
              </label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                autoComplete="current-password"
                aria-invalid={!!error}
              />
            </div>

            {/* role="alert" so a screen reader announces the failure, and the
                icon means the message does not depend on colour alone. */}
            {error && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <Button type="submit" className="w-full" disabled={loading || !username || !password}>
              {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {loading ? 'Signing in…' : 'Sign in'}
            </Button>
          </form>
        </div>
      </div>

      {/* Decorative panel, hidden on small screens where it would only push the
          form below the fold. */}
      <div className="relative hidden overflow-hidden border-l border-border bg-sidebar lg:block">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,var(--color-primary-soft),transparent_55%)]" />
        <div className="relative flex h-full flex-col justify-center p-12">
          <p className="max-w-sm text-2xl font-semibold leading-snug tracking-tight">
            Move mailboxes between IMAP servers, and watch it happen.
          </p>
          <ul className="mt-6 space-y-2 text-sm text-muted-foreground">
            <li>Live logs and progress for every running sync</li>
            <li>Batch imports, schedules and profiles</li>
            <li>REST API, webhooks and a Telegram bot</li>
          </ul>
        </div>
      </div>
    </div>
  )
}
