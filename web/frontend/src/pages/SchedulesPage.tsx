import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, Clock, Pause, Play, Plus, X } from 'lucide-react'
import { profileApi, scheduleApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { EmptyState, PageHeader } from '@/components/ui/data'
import { formatDateTime } from '@/lib/format'

interface Profile {
  id: number
  name: string
}
interface Schedule {
  id: number
  profile_id: number
  cron_expression: string
  is_active: boolean
  last_run_at: string | null
  next_run_at: string | null
}

// Most schedules are one of a handful of shapes; offering them saves people
// looking up crontab syntax for the common case.
const PRESETS = [
  { label: 'Hourly', cron: '0 * * * *' },
  { label: 'Daily 2am', cron: '0 2 * * *' },
  { label: 'Every 6h', cron: '0 */6 * * *' },
  { label: 'Weekly Sun', cron: '0 3 * * 0' },
]

export function SchedulesPage() {
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [showForm, setShowForm] = useState(false)
  const [profileId, setProfileId] = useState(0)
  const [cron, setCron] = useState('0 2 * * *')
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<Schedule | null>(null)

  const act = async (fn: () => Promise<unknown>, whatFailed: string) => {
    setError('')
    try {
      await fn()
      load()
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || whatFailed
      )
    }
  }

  const load = () => {
    scheduleApi
      .list()
      .then((r) => setSchedules(r.data))
      .catch(() => {})
    profileApi
      .list()
      .then((r) => setProfiles(r.data))
      .catch(() => {})
  }

  useEffect(() => {
    load()
  }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!profileId) {
      setError('Pick the profile this schedule should run.')
      return
    }
    try {
      await scheduleApi.create({
        profile_id: profileId,
        cron_expression: cron,
      })
      setShowForm(false)
      setCron('0 2 * * *')
      setProfileId(0)
      load()
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not create the schedule.'
      )
    }
  }

  const toggle = (s: Schedule) =>
    act(
      () => scheduleApi.update(s.id, { is_active: !s.is_active }),
      'Could not change that schedule.'
    )

  const remove = (id: number) =>
    act(() => scheduleApi.delete(id), 'Could not delete that schedule.')

  const profileName = (id: number) => profiles.find((p) => p.id === id)?.name || `Profile #${id}`

  return (
    <div className="mx-auto max-w-4xl">
      <PageHeader
        title="Schedules"
        description="Run a profile automatically on a cron schedule."
        actions={
          !showForm &&
          profiles.length > 0 && (
            <Button onClick={() => setShowForm(true)}>
              <Plus className="mr-1.5 h-4 w-4" />
              New Schedule
            </Button>
          )
        }
      />

      {showForm && (
        <div className="mb-4 rounded-lg border border-border bg-card">
          <header className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">New schedule</h2>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setShowForm(false)}
              aria-label="Cancel"
            >
              <X className="h-4 w-4" />
            </Button>
          </header>
          <form onSubmit={handleCreate} className="space-y-4 p-4">
            <div>
              <label className="mb-1 block text-sm font-medium" htmlFor="profile">
                Profile
              </label>
              <select
                id="profile"
                className="flex h-10 w-full rounded-md border border-input bg-card px-3 text-sm"
                value={profileId}
                onChange={(e) => setProfileId(Number(e.target.value))}
              >
                <option value={0}>Select a profile…</option>
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="mb-1 block text-sm font-medium" htmlFor="cron">
                Cron expression
              </label>
              <div className="mb-2 flex flex-wrap gap-1.5">
                {PRESETS.map((p) => (
                  <button
                    key={p.cron}
                    type="button"
                    onClick={() => setCron(p.cron)}
                    className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
                      cron === p.cron
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-subtle text-muted-foreground hover:bg-accent'
                    }`}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
              <Input
                id="cron"
                value={cron}
                onChange={(e) => setCron(e.target.value)}
                placeholder="0 2 * * *"
                className="font-mono"
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Five fields: minute hour day month weekday. Times are UTC+7.
              </p>
            </div>

            {error && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <div className="flex gap-2">
              <Button type="submit">Create schedule</Button>
              <Button type="button" variant="outline" onClick={() => setShowForm(false)}>
                Cancel
              </Button>
            </div>
          </form>
        </div>
      )}

      <div className="rounded-lg border border-border bg-card">
        {schedules.length === 0 ? (
          <EmptyState
            icon={Clock}
            title={profiles.length === 0 ? 'Create a profile first' : 'No schedules yet'}
            description={
              profiles.length === 0
                ? 'A schedule runs a saved profile, so you need one before you can schedule anything.'
                : 'Schedule a profile and it will run without anyone opening this page.'
            }
            action={
              profiles.length === 0 ? (
                <Link
                  to="/profiles"
                  className="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm transition-colors hover:bg-accent"
                >
                  Go to Profiles
                </Link>
              ) : (
                !showForm && <Button onClick={() => setShowForm(true)}>New schedule</Button>
              )
            }
          />
        ) : (
          <ul className="divide-y divide-border">
            {schedules.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{profileName(s.profile_id)}</p>
                  <p className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <code className="rounded bg-subtle px-1.5 py-0.5 font-mono">
                      {s.cron_expression}
                    </code>
                    {s.last_run_at && <span>Last run {formatDateTime(s.last_run_at)}</span>}
                  </p>
                </div>

                <Badge tone={s.is_active ? 'success' : 'neutral'} dot>
                  {s.is_active ? 'Active' : 'Paused'}
                </Badge>

                <div className="flex gap-1">
                  <Button variant="outline" size="sm" onClick={() => toggle(s)}>
                    {s.is_active ? (
                      <>
                        <Pause className="mr-1.5 h-3 w-3" />
                        Pause
                      </>
                    ) : (
                      <>
                        <Play className="mr-1.5 h-3 w-3" />
                        Resume
                      </>
                    )}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-danger hover:bg-danger-soft"
                    onClick={() => setConfirmDelete(s)}
                  >
                    Delete
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {confirmDelete && (
        <Dialog
          title="Delete this schedule?"
          onClose={() => setConfirmDelete(null)}
          footer={
            <>
              <Button variant="outline" onClick={() => setConfirmDelete(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  const id = confirmDelete.id
                  setConfirmDelete(null)
                  remove(id)
                }}
              >
                Delete schedule
              </Button>
            </>
          }
        >
          <p className="mt-1.5 text-sm text-muted-foreground">
            It will stop running. Syncs it has already done stay in History.
          </p>
        </Dialog>
      )}
    </div>
  )
}
