import { useEffect, useState } from 'react'
import { AlertCircle, Loader2, Moon, Send, Sun, Trash2 } from 'lucide-react'
import {
  historyApi,
  notificationApi,
  telegramApi,
  type Retention,
  type TelegramSettings,
  type TelegramStatus,
} from '@/lib/api'
import { useThemeStore } from '@/stores/themeStore'
import { useAuthStore } from '@/stores/authStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { PageHeader } from '@/components/ui/data'

/** One settings block: a titled panel with an optional right-hand status. */
function Section({
  title,
  description,
  aside,
  children,
}: {
  title: string
  description?: string
  aside?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="rounded-lg border border-border bg-card">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
        </div>
        {aside}
      </header>
      <div className="p-4">{children}</div>
    </section>
  )
}

function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string
  hint?: string
  htmlFor?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function SettingsPage() {
  const { theme, toggle } = useThemeStore()
  const { user } = useAuthStore()

  const [smtp, setSmtp] = useState({
    smtp_host: '',
    smtp_port: 587,
    smtp_user: '',
    smtp_password: '',
    from_email: '',
    to_email: '',
    notify_on_success: false,
    notify_on_failure: true,
  })
  const [saved, setSaved] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)

  const [tg, setTg] = useState<TelegramSettings>({
    enabled: false,
    has_token: false,
    allowed_chat_ids: '',
    user_id: null,
    notify_on_success: true,
    notify_on_failure: true,
    notify_on_progress: true,
    progress_step: 25,
  })
  const [tgToken, setTgToken] = useState('')
  const [tgStatus, setTgStatus] = useState<TelegramStatus | null>(null)
  const [tgSaved, setTgSaved] = useState(false)
  const [tgError, setTgError] = useState<string | null>(null)
  const [tgTest, setTgTest] = useState<string | null>(null)
  const [tgBusy, setTgBusy] = useState(false)

  const [ret, setRet] = useState<Retention>({
    enabled: false,
    keep_days: 30,
    last_run_at: null,
    last_deleted: 0,
  })
  const [retSaved, setRetSaved] = useState(false)
  const [retNotice, setRetNotice] = useState<string | null>(null)
  const [retBusy, setRetBusy] = useState(false)

  const refreshTgStatus = () => {
    telegramApi
      .status()
      .then((r) => setTgStatus(r.data))
      .catch(() => {})
  }

  useEffect(() => {
    notificationApi
      .get()
      .then((r) => {
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
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!user?.is_admin) return
    telegramApi
      .get()
      .then((r) => {
        setTg(r.data.user_id === null ? { ...r.data, user_id: user.id } : r.data)
      })
      .catch(() => {})
    refreshTgStatus()
  }, [user])

  useEffect(() => {
    historyApi
      .getRetention()
      .then((r) => setRet(r.data))
      .catch(() => {})
  }, [])

  const handleRetSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setRetNotice(null)
    setRetBusy(true)
    try {
      const r = await historyApi.saveRetention({
        enabled: ret.enabled,
        keep_days: ret.keep_days,
      })
      setRet(r.data)
      setRetSaved(true)
      setTimeout(() => setRetSaved(false), 2000)
    } catch (err: unknown) {
      setRetNotice(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not save the policy.'
      )
    } finally {
      setRetBusy(false)
    }
  }

  const handleRetRun = async () => {
    setRetNotice(null)
    setRetBusy(true)
    try {
      const r = await historyApi.runRetention()
      setRetNotice(
        r.data.deleted === 0
          ? 'Nothing was old enough to remove.'
          : `Removed ${r.data.deleted} job(s) and their logs.`
      )
      historyApi
        .getRetention()
        .then((x) => setRet(x.data))
        .catch(() => {})
    } catch (err: unknown) {
      setRetNotice(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not run the cleanup.'
      )
    } finally {
      setRetBusy(false)
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setTestResult(null)
    const data: Record<string, unknown> = { ...smtp }
    // An untouched password field means "keep the stored one".
    if (!smtp.smtp_password) delete data.smtp_password
    try {
      await notificationApi.update(data)
      setDirty(false)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (err: unknown) {
      // Unhandled, this failed silently: the button stayed on "Save" and the
      // user had no reason to think the settings had not been stored.
      setTestResult(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not save these settings.'
      )
    }
  }

  const handleTest = async () => {
    // The endpoint reads the *stored* settings, but the button was enabled by
    // the *typed* host. Someone who fixed a broken SMTP host and pressed Test
    // before Save was told the old host worked.
    setTestResult(null)
    try {
      const res = await notificationApi.test()
      setTestResult(
        res.data.status === 'ok'
          ? 'Test email sent.'
          : res.data.detail || 'Could not send the test email.'
      )
    } catch (err: unknown) {
      setTestResult(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not send the test email.'
      )
    }
  }

  const handleTgSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setTgError(null)
    setTgTest(null)
    setTgBusy(true)
    try {
      const payload: Record<string, unknown> = {
        enabled: tg.enabled,
        allowed_chat_ids: tg.allowed_chat_ids,
        user_id: tg.user_id,
        notify_on_success: tg.notify_on_success,
        notify_on_failure: tg.notify_on_failure,
        notify_on_progress: tg.notify_on_progress,
        progress_step: tg.progress_step,
      }
      // The server never returns the token, so an empty box means "keep it".
      if (tgToken) payload.bot_token = tgToken
      const r = await telegramApi.update(payload)
      setTg(r.data)
      setTgToken('')
      setTgSaved(true)
      setTimeout(() => setTgSaved(false), 2000)
      refreshTgStatus()
    } catch (err: unknown) {
      setTgError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not save the settings.'
      )
    } finally {
      setTgBusy(false)
    }
  }

  const handleTgTest = async () => {
    setTgTest(null)
    setTgBusy(true)
    try {
      const r = await telegramApi.test()
      setTgTest(
        r.data.status === 'ok'
          ? `Test message sent. ${r.data.detail || ''}`
          : `Failed: ${r.data.detail || ''}`
      )
    } catch {
      setTgTest('Could not send the test message.')
    } finally {
      setTgBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <PageHeader title="Settings" description="Account, appearance and notifications." />

      <Section title="Account">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted-foreground">Username</dt>
          <dd className="font-medium">{user?.username}</dd>
          <dt className="text-muted-foreground">Email</dt>
          <dd>{user?.email}</dd>
          <dt className="text-muted-foreground">Role</dt>
          <dd>
            <Badge tone={user?.is_admin ? 'info' : 'neutral'}>
              {user?.is_admin ? 'Admin' : 'User'}
            </Badge>
          </dd>
        </dl>
      </Section>

      <Section title="Appearance">
        <div className="flex items-center justify-between gap-3">
          <p className="text-sm">
            Currently using the <span className="font-medium">{theme}</span> theme.
          </p>
          <Button variant="outline" size="sm" onClick={toggle}>
            {theme === 'light' ? (
              <>
                <Moon className="mr-1.5 h-4 w-4" />
                Switch to dark
              </>
            ) : (
              <>
                <Sun className="mr-1.5 h-4 w-4" />
                Switch to light
              </>
            )}
          </Button>
        </div>
      </Section>

      <Section
        title="History cleanup"
        description="Remove old finished jobs, and the imapsync logs stored with them."
      >
        <form onSubmit={handleRetSave} className="space-y-4">
          <label className="flex items-start gap-2.5 text-sm">
            <input
              type="checkbox"
              checked={ret.enabled}
              onChange={(e) => setRet({ ...ret, enabled: e.target.checked })}
              className="mt-0.5 h-4 w-4"
            />
            <span>
              Delete old history automatically
              <span className="block text-xs text-muted-foreground">
                Runs every night at 03:30. Running jobs are never touched.
              </span>
            </span>
          </label>

          <Field
            label="Keep finished jobs for"
            htmlFor="keep_days"
            hint="Anything older is removed with its log. Messages already copied are not affected."
          >
            <div className="mb-2 flex flex-wrap gap-1.5">
              {[7, 30, 90, 365].map((d) => (
                <button
                  key={d}
                  type="button"
                  onClick={() => setRet({ ...ret, keep_days: d })}
                  className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
                    ret.keep_days === d
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-subtle text-muted-foreground hover:bg-accent'
                  }`}
                >
                  {d === 365 ? '1 year' : `${d} days`}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <Input
                id="keep_days"
                type="number"
                min={1}
                max={3650}
                value={ret.keep_days}
                onChange={(e) => setRet({ ...ret, keep_days: Number(e.target.value) })}
                className="w-28"
                disabled={!ret.enabled}
              />
              <span className="text-sm text-muted-foreground">days</span>
            </div>
          </Field>

          {ret.last_run_at && (
            <p className="text-xs text-muted-foreground">
              Last run {new Date(ret.last_run_at).toLocaleString()} — removed {ret.last_deleted}{' '}
              job(s).
            </p>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" disabled={retBusy}>
              {retBusy && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
              {retSaved ? 'Saved' : 'Save'}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleRetRun}
              disabled={retBusy || !ret.enabled}
            >
              <Trash2 className="mr-1.5 h-4 w-4" />
              Run now
            </Button>
            {retNotice && <p className="text-sm text-muted-foreground">{retNotice}</p>}
          </div>
        </form>
      </Section>

      <Section
        title="Email notifications"
        description="Sent when a job finishes, if SMTP is configured."
      >
        <form onSubmit={handleSave} className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="SMTP host" htmlFor="smtp_host">
              <Input
                id="smtp_host"
                value={smtp.smtp_host}
                onChange={(e) => {
                  setSmtp({ ...smtp, smtp_host: e.target.value })
                  setDirty(true)
                }}
                placeholder="smtp.gmail.com"
              />
            </Field>
            <Field label="Port" htmlFor="smtp_port">
              <Input
                id="smtp_port"
                type="number"
                value={smtp.smtp_port}
                onChange={(e) => {
                  setSmtp({ ...smtp, smtp_port: Number(e.target.value) })
                  setDirty(true)
                }}
              />
            </Field>
            <Field label="SMTP user" htmlFor="smtp_user">
              <Input
                id="smtp_user"
                value={smtp.smtp_user}
                onChange={(e) => {
                  setSmtp({ ...smtp, smtp_user: e.target.value })
                  setDirty(true)
                }}
                autoComplete="off"
              />
            </Field>
            <Field
              label="SMTP password"
              htmlFor="smtp_password"
              hint="Leave empty to keep the stored one"
            >
              <Input
                id="smtp_password"
                type="password"
                value={smtp.smtp_password}
                onChange={(e) => {
                  setSmtp({ ...smtp, smtp_password: e.target.value })
                  setDirty(true)
                }}
                autoComplete="new-password"
              />
            </Field>
            <Field label="From address" htmlFor="from_email">
              <Input
                id="from_email"
                type="email"
                value={smtp.from_email}
                onChange={(e) => {
                  setSmtp({ ...smtp, from_email: e.target.value })
                  setDirty(true)
                }}
              />
            </Field>
            <Field label="Send to" htmlFor="to_email">
              <Input
                id="to_email"
                type="email"
                value={smtp.to_email}
                onChange={(e) => {
                  setSmtp({ ...smtp, to_email: e.target.value })
                  setDirty(true)
                }}
              />
            </Field>
          </div>

          <div className="flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={smtp.notify_on_success}
                onChange={(e) => {
                  setSmtp({ ...smtp, notify_on_success: e.target.checked })
                  setDirty(true)
                }}
                className="h-4 w-4"
              />
              Notify on success
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={smtp.notify_on_failure}
                onChange={(e) => {
                  setSmtp({ ...smtp, notify_on_failure: e.target.checked })
                  setDirty(true)
                }}
                className="h-4 w-4"
              />
              Notify on failure
            </label>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit">{saved ? 'Saved' : 'Save'}</Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleTest}
              disabled={!smtp.smtp_host || dirty}
              title={dirty ? 'Save first — the test uses the stored settings' : undefined}
            >
              <Send className="mr-1.5 h-4 w-4" />
              Send test email
            </Button>
            {dirty && (
              <p className="text-sm text-muted-foreground">
                Save first — the test sends with the stored settings.
              </p>
            )}
            {testResult && <p className="text-sm text-muted-foreground">{testResult}</p>}
          </div>
        </form>
      </Section>

      {user?.is_admin && (
        <Section
          title="Telegram bot"
          description="Drive syncs from a chat, and get told when they finish."
          aside={
            tgStatus?.running ? (
              <Badge tone="success" dot>
                Running
                {tgStatus.bot_username ? ` as @${tgStatus.bot_username}` : ''}
              </Badge>
            ) : tgStatus?.error ? (
              <Badge tone="danger" dot>
                Error
              </Badge>
            ) : (
              <Badge tone="neutral" dot>
                Stopped
              </Badge>
            )
          }
        >
          <form onSubmit={handleTgSave} className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Get a token from{' '}
              <code className="rounded bg-subtle px-1 py-0.5 font-mono text-xs">@BotFather</code>,
              then message{' '}
              <code className="rounded bg-subtle px-1 py-0.5 font-mono text-xs">@userinfobot</code>{' '}
              for your chat id.
            </p>

            <Field
              label="Bot token"
              htmlFor="bot_token"
              hint={tg.has_token ? 'A token is saved. Leave empty to keep it.' : undefined}
            >
              <Input
                id="bot_token"
                type="password"
                value={tgToken}
                onChange={(e) => setTgToken(e.target.value)}
                placeholder={tg.has_token ? '••••••••' : '123456789:AA…'}
                autoComplete="off"
              />
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Allowed chat IDs" htmlFor="chat_ids" hint="Comma separated">
                <Input
                  id="chat_ids"
                  value={tg.allowed_chat_ids}
                  onChange={(e) => setTg({ ...tg, allowed_chat_ids: e.target.value })}
                  placeholder="123456789, 987654321"
                  className="font-mono"
                />
              </Field>
              <Field label="Progress step (%)" htmlFor="step" hint="How often to report progress">
                <Input
                  id="step"
                  type="number"
                  min={1}
                  max={50}
                  value={tg.progress_step}
                  onChange={(e) => setTg({ ...tg, progress_step: Number(e.target.value) })}
                />
              </Field>
            </div>

            <p className="rounded-md bg-warning-soft px-3 py-2 text-sm text-warning">
              Anyone in that chat-id list can start syncs, upload batches and read job summaries as{' '}
              <strong>{user.username}</strong>. Keep it to yourself.
            </p>

            <div className="flex flex-wrap gap-4">
              {[
                { k: 'enabled' as const, label: 'Enable bot' },
                { k: 'notify_on_success' as const, label: 'On success' },
                { k: 'notify_on_failure' as const, label: 'On failure' },
                { k: 'notify_on_progress' as const, label: 'Progress updates' },
              ].map((o) => (
                <label key={o.k} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={tg[o.k]}
                    onChange={(e) => setTg({ ...tg, [o.k]: e.target.checked })}
                    className="h-4 w-4"
                  />
                  {o.label}
                </label>
              ))}
            </div>

            {tgError && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{tgError}</span>
              </div>
            )}
            {tgStatus?.error && !tgStatus.running && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>Bot error: {tgStatus.error}</span>
              </div>
            )}

            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={tgBusy}>
                {tgBusy && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
                {tgSaved ? 'Saved' : 'Save'}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={tgBusy || !tg.has_token}
                onClick={handleTgTest}
              >
                <Send className="mr-1.5 h-4 w-4" />
                Send test message
              </Button>
              {tgTest && <p className="text-sm text-muted-foreground">{tgTest}</p>}
            </div>

            <div className="border-t border-border pt-3 text-sm text-muted-foreground">
              <p className="mb-1 font-medium text-foreground">Commands</p>
              <p className="font-mono text-xs">
                /profiles /sync &lt;id&gt; /jobs /status &lt;id&gt; /stop &lt;id&gt; /history
              </p>
              <p className="mt-2">
                Sync a single mailbox without a profile — a space separates the two sides:
              </p>
              <pre className="mt-1 overflow-x-auto rounded bg-subtle p-2 font-mono text-xs">
                {`/run old.com;me@old.com;pw new.com;me@new.com;pw`}
              </pre>
              <p className="mt-2">
                Or send a .csv with caption <code className="font-mono text-xs">/batch</code>, then{' '}
                <code className="font-mono text-xs">/batchgo &lt;id&gt;</code> to run it.
              </p>
            </div>
          </form>
        </Section>
      )}
    </div>
  )
}
