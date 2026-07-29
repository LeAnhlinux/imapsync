import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertCircle, ArrowRight, Check, FolderSearch, Loader2, PlayCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { PageHeader, formatBytes } from '@/components/ui/data'
import { folderApi, profileApi, syncApi, type FolderInfo } from '@/lib/api'
import { cn } from '@/lib/utils'

interface Profile {
  id: number
  name: string
  host1: string
  user1: string
  ssl1: boolean
  host2: string
  user2: string
  ssl2: boolean
}

const STEPS = [
  { key: 'credentials', label: 'Mailboxes' },
  { key: 'options', label: 'Options' },
  { key: 'review', label: 'Review' },
] as const

type Step = (typeof STEPS)[number]['key']

export function NewSyncPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('credentials')
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [selectedProfile, setSelectedProfile] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const [host1, setHost1] = useState('')
  const [user1, setUser1] = useState('')
  const [password1, setPassword1] = useState('')
  const [ssl1, setSsl1] = useState(true)
  const [host2, setHost2] = useState('')
  const [user2, setUser2] = useState('')
  const [password2, setPassword2] = useState('')
  const [ssl2, setSsl2] = useState(true)

  const [dryRun, setDryRun] = useState(false)
  const [automap, setAutomap] = useState(true)
  const [usecache, setUsecache] = useState(true)
  const [foldersText, setFoldersText] = useState('')

  const [available, setAvailable] = useState<FolderInfo[] | null>(null)
  const [browsing, setBrowsing] = useState(false)
  const [browseError, setBrowseError] = useState('')

  const chosen = new Set(foldersText.split('\n').map((f) => f.trim()).filter(Boolean))

  const toggleFolder = (name: string) => {
    const next = new Set(chosen)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    setFoldersText([...next].join('\n'))
  }

  const browse = async () => {
    setBrowsing(true)
    setBrowseError('')
    try {
      // With a profile the server holds the password, so it browses by id
      // rather than asking the client for a credential it does not have.
      const body = selectedProfile
        ? { profile_id: selectedProfile }
        : { host: host1, user: user1, password: password1, ssl: ssl1 }
      const r = await folderApi.browse(body)
      setAvailable(r.data)
      if (r.data.length === 0) setBrowseError('The server returned no folders.')
    } catch (err: unknown) {
      setBrowseError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not list the folders.',
      )
    } finally {
      setBrowsing(false)
    }
  }

  useEffect(() => {
    profileApi.list().then((r) => setProfiles(r.data)).catch(() => {})
  }, [])

  const pickProfile = (id: number | null) => {
    setSelectedProfile(id)
    const p = profiles.find((pr) => pr.id === id)
    if (p) {
      setHost1(p.host1)
      setUser1(p.user1)
      setHost2(p.host2)
      setUser2(p.user2)
      // The TLS flags come from the profile too, or the review step would
      // claim an encrypted connection the sync is not going to make.
      setSsl1(p.ssl1)
      setSsl2(p.ssl2)
    }
  }

  // With a profile chosen the credentials come from the server, so the form
  // only needs to be complete when there is no profile behind it.
  const credentialsReady =
    selectedProfile !== null ||
    (host1 && user1 && password1 && host2 && user2 && password2)

  const start = async () => {
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
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not start the sync.',
      )
    } finally {
      setLoading(false)
    }
  }

  const stepIndex = STEPS.findIndex((s) => s.key === step)

  const mailbox = (which: 1 | 2) => {
    const isSource = which === 1
    return (
      <div className="rounded-lg border border-border p-3">
        <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {isSource ? 'Source (copied from)' : 'Destination (copied to)'}
        </p>
        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={`h${which}`}>Host</label>
            <Input
              id={`h${which}`}
              value={isSource ? host1 : host2}
              onChange={(e) => (isSource ? setHost1 : setHost2)(e.target.value)}
              placeholder={isSource ? 'imap.gmail.com' : 'mail.example.com'}
              disabled={selectedProfile !== null}
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={`u${which}`}>Username</label>
            <Input
              id={`u${which}`}
              value={isSource ? user1 : user2}
              onChange={(e) => (isSource ? setUser1 : setUser2)(e.target.value)}
              placeholder="me@example.com"
              disabled={selectedProfile !== null}
              autoComplete="off"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium" htmlFor={`p${which}`}>Password</label>
            <Input
              id={`p${which}`}
              type="password"
              value={isSource ? password1 : password2}
              onChange={(e) => (isSource ? setPassword1 : setPassword2)(e.target.value)}
              placeholder={selectedProfile !== null ? 'Stored in the profile' : ''}
              disabled={selectedProfile !== null}
              autoComplete="new-password"
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={isSource ? ssl1 : ssl2}
              onChange={(e) => (isSource ? setSsl1 : setSsl2)(e.target.checked)}
              disabled={selectedProfile !== null}
              className="h-4 w-4"
            />
            Use SSL/TLS
          </label>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="New Sync" description="Copy one mailbox to another, now." />

      {/* Numbered steps rather than three bars: the bars showed progress but
          never said what the steps were. */}
      <ol className="mb-6 flex items-center gap-2">
        {STEPS.map((s, i) => {
          const done = i < stepIndex
          const current = i === stepIndex
          return (
            <li key={s.key} className="flex flex-1 items-center gap-2">
              <span
                className={cn(
                  'grid h-6 w-6 shrink-0 place-items-center rounded-full text-xs font-medium',
                  done && 'bg-primary text-primary-foreground',
                  current && 'bg-primary-soft text-primary ring-2 ring-primary',
                  !done && !current && 'bg-subtle text-muted-foreground',
                )}
              >
                {done ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <span
                className={cn(
                  'text-sm',
                  current ? 'font-medium' : 'text-muted-foreground',
                )}
              >
                {s.label}
              </span>
              {i < STEPS.length - 1 && (
                <span
                  aria-hidden
                  className={cn('h-px flex-1', done ? 'bg-primary' : 'bg-border')}
                />
              )}
            </li>
          )
        })}
      </ol>

      <div className="rounded-lg border border-border bg-card">
        {step === 'credentials' && (
          <div className="space-y-4 p-4">
            {profiles.length > 0 && (
              <div>
                <label className="mb-1 block text-sm font-medium" htmlFor="profile">
                  Use a saved profile
                </label>
                <select
                  id="profile"
                  className="flex h-10 w-full rounded-md border border-input bg-card px-3 text-sm"
                  value={selectedProfile ?? ''}
                  onChange={(e) => pickProfile(e.target.value ? Number(e.target.value) : null)}
                >
                  <option value="">Enter credentials manually…</option>
                  {profiles.map((p) => (
                    <option key={p.id} value={p.id}>{p.name}</option>
                  ))}
                </select>
                {selectedProfile !== null && (
                  <p className="mt-1.5 text-xs text-muted-foreground">
                    Credentials come from the profile; the fields below are read-only.
                  </p>
                )}
              </div>
            )}

            <div className="grid gap-3 md:grid-cols-2">
              {mailbox(1)}
              {mailbox(2)}
            </div>
          </div>
        )}

        {step === 'options' && (
          <div className="space-y-4 p-4">
            <fieldset className="space-y-2.5">
              <legend className="mb-1 text-sm font-medium">Behaviour</legend>
              <label className="flex items-start gap-2.5 text-sm">
                <input
                  type="checkbox"
                  checked={dryRun}
                  onChange={(e) => setDryRun(e.target.checked)}
                  className="mt-0.5 h-4 w-4"
                />
                <span>
                  Dry run
                  <span className="block text-xs text-muted-foreground">
                    Connect and report what would move, without writing anything.
                  </span>
                </span>
              </label>
              <label className="flex items-start gap-2.5 text-sm">
                <input
                  type="checkbox"
                  checked={automap}
                  onChange={(e) => setAutomap(e.target.checked)}
                  className="mt-0.5 h-4 w-4"
                />
                <span>
                  Automap folders
                  <span className="block text-xs text-muted-foreground">
                    Match Sent, Drafts and Trash across servers that name them differently.
                  </span>
                </span>
              </label>
              <label className="flex items-start gap-2.5 text-sm">
                <input
                  type="checkbox"
                  checked={usecache}
                  onChange={(e) => setUsecache(e.target.checked)}
                  className="mt-0.5 h-4 w-4"
                />
                <span>
                  Use cache
                  <span className="block text-xs text-muted-foreground">
                    Skip messages a previous run already copied. Makes re-syncs much faster.
                  </span>
                </span>
              </label>
            </fieldset>

            <div>
              <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                <label className="text-sm font-medium" htmlFor="folders">
                  Folders <span className="font-normal text-muted-foreground">(optional)</span>
                </label>
                <Button type="button" variant="outline" size="sm" onClick={browse} disabled={browsing}>
                  {browsing ? (
                    <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
                  ) : (
                    <FolderSearch className="mr-1.5 h-4 w-4" />
                  )}
                  {browsing ? 'Listing…' : 'Browse source'}
                </Button>
              </div>

              {browsing && (
                <p className="mb-2 rounded-md bg-warning-soft px-3 py-2 text-xs text-warning">
                  This opens a real IMAP connection and inspects every folder. A large mailbox can
                  take several minutes — leave this tab open.
                </p>
              )}
              {browseError && (
                <p role="alert" className="mb-2 text-xs text-danger">{browseError}</p>
              )}

              {available && available.length > 0 && (
                <div className="mb-2 max-h-56 overflow-y-auto rounded-md border border-border">
                  {available.map((f) => (
                    <label
                      key={f.name}
                      className="flex cursor-pointer items-center gap-2.5 border-b border-border px-3 py-1.5 text-sm last:border-b-0 hover:bg-subtle"
                    >
                      <input
                        type="checkbox"
                        checked={chosen.has(f.name)}
                        onChange={() => toggleFolder(f.name)}
                        className="h-4 w-4"
                      />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{f.name}</span>
                      <span className="tabular shrink-0 text-xs text-muted-foreground">
                        {f.messages.toLocaleString()} msg
                      </span>
                      <span className="tabular w-20 shrink-0 text-right text-xs text-muted-foreground">
                        {formatBytes(f.size)}
                      </span>
                    </label>
                  ))}
                </div>
              )}

              <textarea
                id="folders"
                value={foldersText}
                onChange={(e) => setFoldersText(e.target.value)}
                rows={3}
                placeholder={'INBOX\nSent'}
                className="w-full rounded-md border border-input bg-card px-3 py-2 font-mono text-sm"
              />
              <p className="mt-1 text-xs text-muted-foreground">
                One per line. Leave empty to sync every folder.
              </p>
            </div>
          </div>
        )}

        {step === 'review' && (
          <div className="space-y-4 p-4">
            <div className="rounded-lg border border-border">
              <div className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center">
                <div className="min-w-0 flex-1">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">From</p>
                  <p className="truncate text-sm font-medium">{user1 || '—'}</p>
                  <p className="truncate text-xs text-muted-foreground">{host1 || '—'}</p>
                </div>
                <ArrowRight className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">To</p>
                  <p className="truncate text-sm font-medium">{user2 || '—'}</p>
                  <p className="truncate text-xs text-muted-foreground">{host2 || '—'}</p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-1.5">
              {dryRun && <Badge tone="warning">Dry run — nothing is written</Badge>}
              {automap && <Badge tone="neutral">automap</Badge>}
              {usecache && <Badge tone="neutral">usecache</Badge>}
              {!ssl1 && <Badge tone="warning">Source without TLS</Badge>}
              {!ssl2 && <Badge tone="warning">Destination without TLS</Badge>}
              <Badge tone="neutral">
                {foldersText.trim()
                  ? `${foldersText.split('\n').filter((f) => f.trim()).length} folder(s)`
                  : 'All folders'}
              </Badge>
            </div>

            {!dryRun && (
              <p className="rounded-md bg-warning-soft px-3 py-2 text-sm text-warning">
                This writes messages into the destination mailbox. Run a dry run first if you are
                not certain.
              </p>
            )}

            {error && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-3">
          <Button
            variant="outline"
            onClick={() => setStep(STEPS[Math.max(0, stepIndex - 1)].key)}
            disabled={stepIndex === 0}
          >
            Back
          </Button>

          {step === 'review' ? (
            <Button onClick={start} disabled={loading}>
              {loading ? (
                <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
              ) : (
                <PlayCircle className="mr-1.5 h-4 w-4" />
              )}
              {loading ? 'Starting…' : dryRun ? 'Run dry run' : 'Start sync'}
            </Button>
          ) : (
            <Button
              onClick={() => setStep(STEPS[stepIndex + 1].key)}
              disabled={step === 'credentials' && !credentialsReady}
            >
              Continue
              <ArrowRight className="ml-1.5 h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
