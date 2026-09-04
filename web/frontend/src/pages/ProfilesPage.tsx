import { useEffect, useState } from 'react'
import { AlertCircle, ArrowRight, FolderSync, Lock, Plus, ShieldOff, X } from 'lucide-react'
import { profileApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { EmptyState, PageHeader, TableSkeleton } from '@/components/ui/data'

interface Profile {
  id: number
  name: string
  host1: string
  user1: string
  ssl1: boolean
  host2: string
  user2: string
  ssl2: boolean
  extra_options: Record<string, unknown>
  created_at: string
}

const EMPTY = {
  name: '', host1: '', user1: '', password1: '', ssl1: true,
  host2: '', user2: '', password2: '', ssl2: true,
}

export function ProfilesPage() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState(EMPTY)
  const [error, setError] = useState('')
  const [deleteError, setDeleteError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<Profile | null>(null)

  const load = () => {
    profileApi
      .list()
      .then((r) => setProfiles(r.data))
      .catch(() => {})
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const resetForm = () => {
    setForm(EMPTY)
    setEditId(null)
    setShowForm(false)
    setError('')
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      if (editId) {
        const data: Record<string, unknown> = { ...form }
        // An untouched password field means "keep the stored one".
        if (!form.password1) delete data.password1
        if (!form.password2) delete data.password2
        await profileApi.update(editId, data)
      } else {
        await profileApi.create(form)
      }
      resetForm()
      load()
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not save the profile.',
      )
    }
  }

  const handleEdit = (p: Profile) => {
    setForm({
      name: p.name, host1: p.host1, user1: p.user1, password1: '', ssl1: p.ssl1,
      host2: p.host2, user2: p.user2, password2: '', ssl2: p.ssl2,
    })
    setEditId(p.id)
    setShowForm(true)
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    setDeleteError('')
    try {
      await profileApi.delete(confirmDelete.id)
      setConfirmDelete(null)
      load()
    } catch (err: unknown) {
      // Unhandled, this failed silently: the dialog stayed open, unchanged,
      // and the user clicked Delete again.
      setDeleteError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not delete that profile.',
      )
    }
  }

  const side = (which: 1 | 2) => {
    const host = which === 1 ? 'host1' : 'host2'
    const user = which === 1 ? 'user1' : 'user2'
    const pass = which === 1 ? 'password1' : 'password2'
    const ssl = which === 1 ? 'ssl1' : 'ssl2'
    return (
      <div className="rounded-lg border border-border p-3">
        <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {which === 1 ? 'Source (copied from)' : 'Destination (copied to)'}
        </p>
        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={host}>Host</label>
            <Input
              id={host}
              value={form[host]}
              onChange={(e) => setForm({ ...form, [host]: e.target.value })}
              placeholder={which === 1 ? 'imap.gmail.com' : 'mail.example.com'}
              required
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={user}>Username</label>
            <Input
              id={user}
              value={form[user]}
              onChange={(e) => setForm({ ...form, [user]: e.target.value })}
              placeholder="me@example.com"
              required
              autoComplete="off"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={pass}>Password</label>
            <Input
              id={pass}
              type="password"
              value={form[pass]}
              onChange={(e) => setForm({ ...form, [pass]: e.target.value })}
              placeholder={editId ? 'Leave empty to keep current' : ''}
              required={!editId}
              autoComplete="new-password"
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={form[ssl]}
              onChange={(e) => setForm({ ...form, [ssl]: e.target.checked })}
              className="h-4 w-4"
            />
            Use SSL/TLS
          </label>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-5xl">
      <PageHeader
        title="Profiles"
        description="Saved mailbox pairs you can sync or schedule."
        actions={
          !showForm && (
            <Button onClick={() => setShowForm(true)}>
              <Plus className="mr-1.5 h-4 w-4" />
              New Profile
            </Button>
          )
        }
      />

      {showForm && (
        <div className="mb-4 rounded-lg border border-border bg-card">
          <header className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">
              {editId ? 'Edit profile' : 'New profile'}
            </h2>
            <Button variant="ghost" size="icon" onClick={resetForm} aria-label="Cancel">
              <X className="h-4 w-4" />
            </Button>
          </header>
          <form onSubmit={handleSubmit} className="space-y-4 p-4">
            <div>
              <label className="mb-1 block text-sm font-medium" htmlFor="name">Profile name</label>
              <Input
                id="name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Company A — staff mailboxes"
                required
                autoFocus
              />
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              {side(1)}
              {side(2)}
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
              <Button type="submit">{editId ? 'Save changes' : 'Create profile'}</Button>
              <Button type="button" variant="outline" onClick={resetForm}>Cancel</Button>
            </div>
          </form>
        </div>
      )}

      <div className="rounded-lg border border-border bg-card">
        {loading ? (
          <TableSkeleton rows={3} cols={3} />
        ) : profiles.length === 0 ? (
          <EmptyState
            icon={FolderSync}
            title="No profiles yet"
            description="A profile stores one mailbox pair with its passwords encrypted, so you can sync or schedule it without retyping them."
            action={
              !showForm && (
                <Button onClick={() => setShowForm(true)}>
                  <Plus className="mr-1.5 h-4 w-4" />
                  Create your first profile
                </Button>
              )
            }
          />
        ) : (
          <ul className="divide-y divide-border">
            {profiles.map((p) => (
              <li key={p.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{p.name}</p>
                  <p className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="truncate">{p.user1} on {p.host1}</span>
                    <ArrowRight className="h-3 w-3 shrink-0" aria-hidden />
                    <span className="truncate">{p.user2} on {p.host2}</span>
                  </p>
                </div>

                {/* An unencrypted leg is worth seeing at a glance. */}
                {p.ssl1 && p.ssl2 ? (
                  <Badge tone="neutral">
                    <Lock className="h-3 w-3" aria-hidden /> TLS
                  </Badge>
                ) : (
                  <Badge tone="warning">
                    <ShieldOff className="h-3 w-3" aria-hidden /> No TLS
                  </Badge>
                )}

                <div className="flex gap-1">
                  <Button variant="outline" size="sm" onClick={() => handleEdit(p)}>Edit</Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-danger hover:bg-danger-soft"
                    onClick={() => setConfirmDelete(p)}
                  >
                    Delete
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Deleting a profile is not undoable, so it asks first and names what
          it is about to remove. */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <button
            className="absolute inset-0 bg-black/60"
            onClick={() => setConfirmDelete(null)}
            aria-label="Cancel"
          />
          <div
            role="dialog"
            aria-modal="true"
            className="relative w-full max-w-sm rounded-lg border border-border bg-card p-5 shadow-xl"
          >
            <h2 className="text-sm font-semibold">Delete this profile?</h2>
            <p className="mt-1.5 text-sm text-muted-foreground">
              <span className="font-medium text-foreground">{confirmDelete.name}</span> and its
              stored credentials will be removed. Jobs already run from it stay in History,
              and any schedule that used it is removed with it.
            </p>
            {deleteError && (
              <p
                role="alert"
                className="mt-3 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                {deleteError}
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <Button
                variant="outline"
                onClick={() => {
                  setConfirmDelete(null)
                  setDeleteError('')
                }}
              >
                Cancel
              </Button>
              <Button variant="destructive" onClick={doDelete}>Delete profile</Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
