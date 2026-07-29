import { useEffect, useState } from 'react'
import { profileApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

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

export function ProfilesPage() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState({
    name: '', host1: '', user1: '', password1: '', ssl1: true,
    host2: '', user2: '', password2: '', ssl2: true,
  })
  const [error, setError] = useState('')

  const load = () => {
    profileApi.list().then((r) => setProfiles(r.data)).catch(() => {})
  }

  useEffect(() => { load() }, [])

  const resetForm = () => {
    setForm({ name: '', host1: '', user1: '', password1: '', ssl1: true, host2: '', user2: '', password2: '', ssl2: true })
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
        if (!form.password1) delete data.password1
        if (!form.password2) delete data.password2
        await profileApi.update(editId, data)
      } else {
        await profileApi.create(form)
      }
      resetForm()
      load()
    } catch (err: unknown) {
      setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Error')
    }
  }

  const handleEdit = (p: Profile) => {
    setForm({ name: p.name, host1: p.host1, user1: p.user1, password1: '', ssl1: p.ssl1, host2: p.host2, user2: p.user2, password2: '', ssl2: p.ssl2 })
    setEditId(p.id)
    setShowForm(true)
  }

  const handleDelete = async (id: number) => {
    await profileApi.delete(id)
    load()
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Profiles</h1>
        <Button onClick={() => { resetForm(); setShowForm(!showForm) }}>
          {showForm ? 'Cancel' : 'New Profile'}
        </Button>
      </div>

      {showForm && (
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>{editId ? 'Edit Profile' : 'New Profile'}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-4">
              <Input placeholder="Profile name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <h4 className="font-medium text-sm">Source</h4>
                  <Input placeholder="IMAP Host" value={form.host1} onChange={(e) => setForm({ ...form, host1: e.target.value })} required />
                  <Input placeholder="Username" value={form.user1} onChange={(e) => setForm({ ...form, user1: e.target.value })} required />
                  <Input placeholder={editId ? 'Password (leave empty to keep)' : 'Password'} type="password" value={form.password1} onChange={(e) => setForm({ ...form, password1: e.target.value })} required={!editId} />
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={form.ssl1} onChange={(e) => setForm({ ...form, ssl1: e.target.checked })} /> SSL
                  </label>
                </div>
                <div className="space-y-2">
                  <h4 className="font-medium text-sm">Destination</h4>
                  <Input placeholder="IMAP Host" value={form.host2} onChange={(e) => setForm({ ...form, host2: e.target.value })} required />
                  <Input placeholder="Username" value={form.user2} onChange={(e) => setForm({ ...form, user2: e.target.value })} required />
                  <Input placeholder={editId ? 'Password (leave empty to keep)' : 'Password'} type="password" value={form.password2} onChange={(e) => setForm({ ...form, password2: e.target.value })} required={!editId} />
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={form.ssl2} onChange={(e) => setForm({ ...form, ssl2: e.target.checked })} /> SSL
                  </label>
                </div>
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
              <Button type="submit">{editId ? 'Update' : 'Create'}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      {profiles.length === 0 ? (
        <p className="text-muted-foreground">No profiles yet.</p>
      ) : (
        <div className="space-y-3">
          {profiles.map((p) => (
            <Card key={p.id}>
              <CardContent className="p-4 flex items-center justify-between">
                <div>
                  <div className="font-medium">{p.name}</div>
                  <div className="text-sm text-muted-foreground">
                    {p.user1}@{p.host1} → {p.user2}@{p.host2}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => handleEdit(p)}>Edit</Button>
                  <Button size="sm" variant="destructive" onClick={() => handleDelete(p.id)}>Delete</Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
