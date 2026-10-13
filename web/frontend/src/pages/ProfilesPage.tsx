import { useEffect, useState } from 'react'
import { ArrowRight, FolderSync, Lock, Plus, ShieldOff, X } from 'lucide-react'
import { profileApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Alert, EmptyState, Page, Section, TableSkeleton } from '@/components/ui/data'
import { Field } from '@/components/ui/form'
import { MailboxFields } from '@/components/MailboxFields'

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

  const side = (which: 1 | 2) => (
    <MailboxFields
      which={which}
      value={{
        host: form[`host${which}`],
        user: form[`user${which}`],
        password: form[`password${which}`],
        ssl: form[`ssl${which}`],
      }}
      onChange={(patch) =>
        setForm({
          ...form,
          ...Object.fromEntries(Object.entries(patch).map(([k, v]) => [`${k}${which}`, v])),
        })
      }
      passwordPlaceholder={editId ? 'Leave empty to keep current' : undefined}
      passwordRequired={!editId}
    />
  )

  return (
    <Page
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
    >
      {showForm && (
        <Section
          title={editId ? 'Edit profile' : 'New profile'}
          aside={
            <Button
              variant="ghost"
              size="icon"
              className="-mr-2 h-8 w-8"
              onClick={resetForm}
              aria-label="Cancel"
            >
              <X className="h-4 w-4" />
            </Button>
          }
        >
          <form onSubmit={handleSubmit} className="space-y-4">
            <Field label="Profile name" htmlFor="name" className="max-w-md">
              <Input
                id="name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Company A — staff mailboxes"
                required
                autoFocus
              />
            </Field>

            <div className="grid gap-4 md:grid-cols-2">
              {side(1)}
              {side(2)}
            </div>

            {error && (
              <Alert>{error}</Alert>
            )}

            <div className="flex gap-2">
              <Button type="submit">{editId ? 'Save changes' : 'Create profile'}</Button>
              <Button type="button" variant="outline" onClick={resetForm}>Cancel</Button>
            </div>
          </form>
        </Section>
      )}

      <Section flush>
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

                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => handleEdit(p)}>Edit</Button>
                  <Button variant="danger" size="sm" onClick={() => setConfirmDelete(p)}>
                    Delete
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>

      {/* Deleting a profile is not undoable, so it asks first and names what
          it is about to remove. */}
      {confirmDelete && (
        <Dialog
          title="Delete this profile?"
          onClose={() => {
            setConfirmDelete(null)
            setDeleteError('')
          }}
          footer={
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setConfirmDelete(null)
                  setDeleteError('')
                }}
              >
                Cancel
              </Button>
              <Button variant="destructive" onClick={doDelete}>
                Delete profile
              </Button>
            </>
          }
        >
          <p className="mt-1.5 text-sm text-muted-foreground">
            <span className="font-medium text-foreground">{confirmDelete.name}</span> and its
            stored credentials will be removed. Jobs already run from it stay in History, and any
            schedule that used it is removed with it.
          </p>
          {deleteError && (
            <Alert className="mt-3">{deleteError}</Alert>
          )}
        </Dialog>
      )}
    </Page>
  )
}
