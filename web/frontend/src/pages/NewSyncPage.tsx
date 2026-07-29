import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { PageHeader } from '@/components/ui/data'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { profileApi, syncApi } from '@/lib/api'

interface Profile {
  id: number
  name: string
  host1: string
  user1: string
  host2: string
  user2: string
}

type Step = 'credentials' | 'options' | 'review'

export function NewSyncPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('credentials')
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [selectedProfile, setSelectedProfile] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Credentials
  const [host1, setHost1] = useState('')
  const [user1, setUser1] = useState('')
  const [password1, setPassword1] = useState('')
  const [ssl1, setSsl1] = useState(true)
  const [host2, setHost2] = useState('')
  const [user2, setUser2] = useState('')
  const [password2, setPassword2] = useState('')
  const [ssl2, setSsl2] = useState(true)

  // Options
  const [dryRun, setDryRun] = useState(false)
  const [automap, setAutomap] = useState(true)
  const [usecache, setUsecache] = useState(true)
  const [foldersText, setFoldersText] = useState('')

  useEffect(() => {
    profileApi.list().then((r) => setProfiles(r.data)).catch(() => {})
  }, [])

  const handleProfileSelect = (profileId: number) => {
    setSelectedProfile(profileId)
    const p = profiles.find((pr) => pr.id === profileId)
    if (p) {
      setHost1(p.host1)
      setUser1(p.user1)
      setHost2(p.host2)
      setUser2(p.user2)
    }
  }

  const handleStart = async () => {
    setLoading(true)
    setError('')
    try {
      const folders = foldersText.split('\n').map((f) => f.trim()).filter(Boolean)
      const extra_options: Record<string, boolean> = {}
      if (automap) extra_options.automap = true
      if (usecache) extra_options.usecache = true
      const data = selectedProfile
        ? { profile_id: selectedProfile, dry_run: dryRun, folders, extra_options }
        : { host1, user1, password1, ssl1, host2, user2, password2, ssl2, dry_run: dryRun, folders, extra_options }
      const res = await syncApi.start(data)
      navigate(`/jobs?highlight=${res.data.id}`)
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Failed to start sync'
      setError(msg)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl">
      <PageHeader title="New Sync" description="Copy one mailbox to another, now." />

      {/* Step indicators */}
      <div className="flex gap-2 mb-6">
        {(['credentials', 'options', 'review'] as Step[]).map((s, i) => (
          <div
            key={s}
            className={`flex-1 h-2 rounded ${
              s === step ? 'bg-primary' : i < ['credentials', 'options', 'review'].indexOf(step) ? 'bg-primary/50' : 'bg-muted'
            }`}
          />
        ))}
      </div>

      {step === 'credentials' && (
        <Card>
          <CardHeader>
            <CardTitle>Step 1: Credentials</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {profiles.length > 0 && (
              <div>
                <label className="text-sm font-medium">Use saved profile</label>
                <select
                  className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  value={selectedProfile || ''}
                  onChange={(e) => e.target.value ? handleProfileSelect(Number(e.target.value)) : setSelectedProfile(null)}
                >
                  <option value="">-- Manual input --</option>
                  {profiles.map((p) => (
                    <option key={p.id} value={p.id}>{p.name} ({p.host1})</option>
                  ))}
                </select>
              </div>
            )}

            {!selectedProfile && (
              <>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <h3 className="font-medium mb-2">Source (Host 1)</h3>
                    <div className="space-y-2">
                      <Input placeholder="IMAP Host" value={host1} onChange={(e) => setHost1(e.target.value)} />
                      <Input placeholder="Username" value={user1} onChange={(e) => setUser1(e.target.value)} />
                      <Input placeholder="Password" type="password" value={password1} onChange={(e) => setPassword1(e.target.value)} />
                      <label className="flex items-center gap-2 text-sm">
                        <input type="checkbox" checked={ssl1} onChange={(e) => setSsl1(e.target.checked)} />
                        SSL/TLS
                      </label>
                    </div>
                  </div>
                  <div>
                    <h3 className="font-medium mb-2">Destination (Host 2)</h3>
                    <div className="space-y-2">
                      <Input placeholder="IMAP Host" value={host2} onChange={(e) => setHost2(e.target.value)} />
                      <Input placeholder="Username" value={user2} onChange={(e) => setUser2(e.target.value)} />
                      <Input placeholder="Password" type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} />
                      <label className="flex items-center gap-2 text-sm">
                        <input type="checkbox" checked={ssl2} onChange={(e) => setSsl2(e.target.checked)} />
                        SSL/TLS
                      </label>
                    </div>
                  </div>
                </div>
              </>
            )}

            <div className="flex justify-end">
              <Button onClick={() => setStep('options')}>Next</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 'options' && (
        <Card>
          <CardHeader>
            <CardTitle>Step 2: Options</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap gap-x-6 gap-y-3">
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" checked={automap} onChange={(e) => setAutomap(e.target.checked)} className="w-4 h-4" />
                <span className="font-medium">Automap</span>
                <span className="text-xs text-muted-foreground">(auto map Sent/Drafts/Junk)</span>
              </label>
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" checked={usecache} onChange={(e) => setUsecache(e.target.checked)} className="w-4 h-4" />
                <span className="font-medium">Use Cache</span>
                <span className="text-xs text-muted-foreground">(faster re-syncs)</span>
              </label>
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} className="w-4 h-4" />
                <span className="font-medium">Dry Run</span>
                <span className="text-xs text-muted-foreground">(simulate only)</span>
              </label>
            </div>

            <div>
              <label className="text-sm font-medium">Specific folders (one per line, leave empty for all)</label>
              <textarea
                className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm min-h-[100px]"
                placeholder="INBOX&#10;Sent&#10;Drafts"
                value={foldersText}
                onChange={(e) => setFoldersText(e.target.value)}
              />
            </div>

            <div className="flex justify-between">
              <Button variant="outline" onClick={() => setStep('credentials')}>Back</Button>
              <Button onClick={() => setStep('review')}>Next</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 'review' && (
        <Card>
          <CardHeader>
            <CardTitle>Step 3: Review & Start</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="bg-muted rounded-md p-4 text-sm space-y-1">
              {selectedProfile ? (
                <p><strong>Profile:</strong> {profiles.find((p) => p.id === selectedProfile)?.name}</p>
              ) : (
                <>
                  <p><strong>Source:</strong> {user1}@{host1} {ssl1 ? '(SSL)' : ''}</p>
                  <p><strong>Destination:</strong> {user2}@{host2} {ssl2 ? '(SSL)' : ''}</p>
                </>
              )}
              <p><strong>Automap:</strong> {automap ? 'Yes' : 'No'} | <strong>Cache:</strong> {usecache ? 'Yes' : 'No'} | <strong>Dry run:</strong> {dryRun ? 'Yes' : 'No'}</p>
              {foldersText && <p><strong>Folders:</strong> {foldersText.split('\n').filter(Boolean).join(', ')}</p>}
            </div>

            {error && <div className="text-sm text-destructive">{error}</div>}

            <div className="flex justify-between">
              <Button variant="outline" onClick={() => setStep('options')}>Back</Button>
              <Button onClick={handleStart} disabled={loading}>
                {loading ? 'Starting...' : 'Start Sync'}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
