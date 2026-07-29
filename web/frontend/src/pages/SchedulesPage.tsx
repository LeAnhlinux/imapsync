import { useEffect, useState } from 'react'
import { profileApi, scheduleApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

interface Profile { id: number; name: string }
interface Schedule {
  id: number
  profile_id: number
  cron_expression: string
  is_active: boolean
  last_run_at: string | null
  next_run_at: string | null
}

export function SchedulesPage() {
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [showForm, setShowForm] = useState(false)
  const [profileId, setProfileId] = useState<number>(0)
  const [cron, setCron] = useState('0 2 * * *')
  const [error, setError] = useState('')

  const load = () => {
    scheduleApi.list().then((r) => setSchedules(r.data)).catch(() => {})
    profileApi.list().then((r) => setProfiles(r.data)).catch(() => {})
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!profileId) { setError('Select a profile'); return }
    try {
      await scheduleApi.create({ profile_id: profileId, cron_expression: cron })
      setShowForm(false)
      setCron('0 2 * * *')
      load()
    } catch (err: unknown) {
      setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Error')
    }
  }

  const handleToggle = async (s: Schedule) => {
    await scheduleApi.update(s.id, { is_active: !s.is_active })
    load()
  }

  const handleDelete = async (id: number) => {
    await scheduleApi.delete(id)
    load()
  }

  const profileName = (id: number) => profiles.find((p) => p.id === id)?.name || `Profile #${id}`

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Schedules</h1>
        <Button onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : 'New Schedule'}
        </Button>
      </div>

      {showForm && (
        <Card className="mb-6">
          <CardHeader><CardTitle>New Schedule</CardTitle></CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="text-sm font-medium">Profile</label>
                <select
                  className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  value={profileId}
                  onChange={(e) => setProfileId(Number(e.target.value))}
                >
                  <option value={0}>-- Select profile --</option>
                  {profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </div>
              <div>
                <label className="text-sm font-medium">Cron Expression</label>
                <Input value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 2 * * *" />
                <p className="text-xs text-muted-foreground mt-1">
                  Format: minute hour day month weekday (e.g., "0 2 * * *" = daily at 2:00 AM)
                </p>
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
              <Button type="submit">Create Schedule</Button>
            </form>
          </CardContent>
        </Card>
      )}

      {schedules.length === 0 ? (
        <p className="text-muted-foreground">No schedules yet. Create a profile first, then schedule it.</p>
      ) : (
        <div className="space-y-3">
          {schedules.map((s) => (
            <Card key={s.id}>
              <CardContent className="p-4 flex items-center justify-between">
                <div>
                  <div className="font-medium">{profileName(s.profile_id)}</div>
                  <div className="text-sm text-muted-foreground">
                    <code className="bg-muted px-1 rounded">{s.cron_expression}</code>
                    {' '}&middot;{' '}
                    {s.is_active ? <span className="text-green-600">Active</span> : <span className="text-muted-foreground">Paused</span>}
                    {s.last_run_at && <> &middot; Last: {new Date(s.last_run_at).toLocaleString()}</>}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => handleToggle(s)}>
                    {s.is_active ? 'Pause' : 'Resume'}
                  </Button>
                  <Button size="sm" variant="destructive" onClick={() => handleDelete(s.id)}>Delete</Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
