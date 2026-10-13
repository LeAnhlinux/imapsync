import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowRight, Check, FolderSearch, Loader2, PlayCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, Page, Section } from '@/components/ui/data'
import { Checkbox, Field, Select, Textarea } from '@/components/ui/form'
import { MailboxFields } from '@/components/MailboxFields'
import { formatBytes } from '@/lib/format'
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

  // Mirrors ApplyPresets in the runner: a Gmail source going anywhere else is
  // run with --gmail1, which imapsync cannot combine with the cache.
  const isGmail = (host: string) =>
    /^imap\.(gmail|googlemail)\.com(:\d+)?$/i.test(host.trim())
  const gmailSource = isGmail(host1) && !isGmail(host2)
  const cacheUsed = usecache && !gmailSource

  const chosen = new Set(
    foldersText
      .split('\n')
      .map((f) => f.trim())
      .filter(Boolean)
  )

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
          'Could not list the folders.'
      )
    } finally {
      setBrowsing(false)
    }
  }

  useEffect(() => {
    profileApi
      .list()
      .then((r) => setProfiles(r.data))
      .catch(() => {})
  }, [])

  const pickProfile = (id: number | null) => {
    // A folder list browsed from the previous source is not a list of this
    // one's folders. Leaving it up let someone tick "Archive" from mailbox A
    // and run it against mailbox B, where it may not exist.
    setAvailable(null)
    setBrowseError('')
    setFoldersText('')
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
    selectedProfile !== null || (host1 && user1 && password1 && host2 && user2 && password2)

  const start = async () => {
    setLoading(true)
    setError('')
    try {
      const folders = foldersText
        .split('\n')
        .map((f) => f.trim())
        .filter(Boolean)
      // Send the off state too, not just the on state. A profile can have
      // usecache saved as on, and "unticked" has to be able to override that —
      // omitting the key just means "whatever the profile says", which is how
      // the review screen came to describe a run that never happened.
      const extra_options: Record<string, boolean> = {}
      if (automap) extra_options.automap = true
      if (usecache) extra_options.usecache = true
      else extra_options.nousecache = true
      const data = selectedProfile
        ? { profile_id: selectedProfile, dry_run: dryRun, folders, extra_options }
        : {
            host1,
            user1,
            password1,
            ssl1,
            host2,
            user2,
            password2,
            ssl2,
            dry_run: dryRun,
            folders,
            extra_options,
          }
      const res = await syncApi.start(data)
      navigate(`/jobs?highlight=${res.data.id}`)
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not start the sync.'
      )
    } finally {
      setLoading(false)
    }
  }

  const stepIndex = STEPS.findIndex((s) => s.key === step)

  const mailbox = (which: 1 | 2) => {
    const isSource = which === 1
    return (
      <MailboxFields
        which={which}
        value={
          isSource
            ? { host: host1, user: user1, password: password1, ssl: ssl1 }
            : { host: host2, user: user2, password: password2, ssl: ssl2 }
        }
        onChange={(patch) => {
          if (patch.host !== undefined) (isSource ? setHost1 : setHost2)(patch.host)
          if (patch.user !== undefined) (isSource ? setUser1 : setUser2)(patch.user)
          if (patch.password !== undefined) (isSource ? setPassword1 : setPassword2)(patch.password)
          if (patch.ssl !== undefined) (isSource ? setSsl1 : setSsl2)(patch.ssl)
        }}
        passwordPlaceholder={selectedProfile !== null ? 'Stored in the profile' : undefined}
        disabled={selectedProfile !== null}
      />
    )
  }

  return (
    <Page narrow title="New Sync" description="Copy one mailbox to another, now.">
      {/* Numbered steps rather than three bars: the bars showed progress but
          never said what the steps were. */}
      <ol className="flex items-center gap-2" aria-label="Progress">
        {STEPS.map((s, i) => {
          const done = i < stepIndex
          const current = i === stepIndex
          return (
            <li
              key={s.key}
              aria-current={current ? 'step' : undefined}
              className={cn('flex items-center gap-2', i < STEPS.length - 1 && 'flex-1')}
            >
              <span
                className={cn(
                  'tabular grid h-7 w-7 shrink-0 place-items-center rounded-full text-xs font-semibold',
                  done && 'bg-primary text-primary-foreground',
                  current && 'bg-primary-soft text-primary ring-2 ring-primary',
                  !done && !current && 'bg-subtle text-muted-foreground'
                )}
              >
                {done ? <Check className="h-3.5 w-3.5" aria-hidden /> : i + 1}
              </span>
              <span className={cn('text-sm', current ? 'font-medium' : 'text-muted-foreground')}>
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

      <Section flush>
        {step === 'credentials' && (
          <div className="space-y-4 p-4">
            {profiles.length > 0 && (
              <Field
                label="Use a saved profile"
                htmlFor="profile"
                className="max-w-md"
                hint={
                  selectedProfile !== null &&
                  'Credentials come from the profile; the fields below are read-only.'
                }
              >
                <Select
                  id="profile"
                  value={selectedProfile ?? ''}
                  onChange={(e) => pickProfile(e.target.value ? Number(e.target.value) : null)}
                >
                  <option value="">Enter credentials manually…</option>
                  {profiles.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </Select>
              </Field>
            )}

            <div className="grid gap-4 md:grid-cols-2">
              {mailbox(1)}
              {mailbox(2)}
            </div>
          </div>
        )}

        {step === 'options' && (
          <div className="space-y-4 p-4">
            <fieldset className="space-y-3">
              <legend className="mb-2 text-sm font-medium">Behaviour</legend>
              <Checkbox
                label="Dry run"
                hint="Connect and report what would move, without writing anything."
                checked={dryRun}
                onChange={(e) => setDryRun(e.target.checked)}
              />
              <Checkbox
                label="Automap folders"
                hint="Match Sent, Drafts and Trash across servers that name them differently."
                checked={automap}
                onChange={(e) => setAutomap(e.target.checked)}
              />
              <Checkbox
                label="Use cache"
                hint={
                  gmailSource
                    ? 'Not available with a Gmail source.'
                    : 'Skip messages a previous run already copied. Makes re-syncs much faster.'
                }
                checked={cacheUsed}
                disabled={gmailSource}
                onChange={(e) => setUsecache(e.target.checked)}
              />
              {gmailSource && (
                <Alert tone="info">
                  Gmail lists every label as a folder. Each message is copied once, into the first
                  folder it appears in; All Mail only receives archived messages. Folders lose
                  their “[Gmail]/” prefix on the destination, and a folder left empty is not
                  created.
                </Alert>
              )}
            </fieldset>

            <div>
              <div className="mb-1.5 flex flex-wrap items-center justify-between gap-2">
                <label className="text-sm font-medium" htmlFor="folders">
                  Folders <span className="font-normal text-muted-foreground">(optional)</span>
                </label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={browse}
                  disabled={browsing}
                >
                  {browsing ? (
                    <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
                  ) : (
                    <FolderSearch className="mr-1.5 h-4 w-4" />
                  )}
                  {browsing ? 'Listing…' : 'Browse source'}
                </Button>
              </div>

              {browsing && (
                <Alert tone="warning" className="mb-2">
                  This opens a real IMAP connection and inspects every folder. A large mailbox can
                  take several minutes — leave this tab open.
                </Alert>
              )}
              {browseError && (
                <Alert className="mb-2">{browseError}</Alert>
              )}

              {available && available.length > 0 && (
                <div className="mb-2 max-h-56 overflow-y-auto rounded-md border border-border">
                  {available.map((f) => (
                    <label
                      key={f.name}
                      className="flex min-h-10 cursor-pointer items-center gap-2.5 border-b border-border px-3 py-1.5 text-sm last:border-b-0 hover:bg-subtle"
                    >
                      <input
                        type="checkbox"
                        checked={chosen.has(f.name)}
                        onChange={() => toggleFolder(f.name)}
                        className="h-4 w-4 cursor-pointer accent-primary"
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

              <Textarea
                id="folders"
                value={foldersText}
                onChange={(e) => setFoldersText(e.target.value)}
                rows={3}
                placeholder={'INBOX\nSent'}
              />
              <p className="mt-1.5 text-xs text-muted-foreground">
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
              {cacheUsed && <Badge tone="neutral">usecache</Badge>}
              {gmailSource && <Badge tone="neutral">gmail1 — one copy per message</Badge>}
              {!ssl1 && <Badge tone="warning">Source without TLS</Badge>}
              {!ssl2 && <Badge tone="warning">Destination without TLS</Badge>}
              <Badge tone="neutral">
                {foldersText.trim()
                  ? `${foldersText.split('\n').filter((f) => f.trim()).length} folder(s)`
                  : 'All folders'}
              </Badge>
            </div>

            {!dryRun && (
              <Alert tone="warning">
                This writes messages into the destination mailbox. Run a dry run first if you are
                not certain.
              </Alert>
            )}

            {error && (
              <Alert>{error}</Alert>
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
      </Section>
    </Page>
  )
}
