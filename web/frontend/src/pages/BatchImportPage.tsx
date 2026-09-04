import { useEffect, useRef, useState } from 'react'
import {
  AlertCircle,
  CheckCircle2,
  FileSpreadsheet,
  Loader2,
  PlayCircle,
  Square,
  Upload,
} from 'lucide-react'
import { batchApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge, StatusBadge } from '@/components/ui/badge'
import { PageHeader, Progress, Stat } from '@/components/ui/data'
import { cn } from '@/lib/utils'

interface BatchRun {
  id: number
  filename: string
  total_rows: number
  completed_rows: number
  failed_rows: number
  status: string
}

const SAMPLE = `# host1;user1;password1;host2;user2;password2
mail.old.com;a@old.com;pw1;mail.new.com;a@new.com;pw2
mail.old.com;b@old.com;pw3;mail.new.com;b@new.com;pw4`

export function BatchImportPage() {
  const [file, setFile] = useState<File | null>(null)
  const [csvText, setCsvText] = useState('')
  const [mode, setMode] = useState<'file' | 'paste'>('paste')
  const [dragging, setDragging] = useState(false)

  const [dryRun, setDryRun] = useState(false)
  const [ssl1, setSsl1] = useState(true)
  const [ssl2, setSsl2] = useState(true)
  const [automap, setAutomap] = useState(true)
  const [usecache, setUsecache] = useState(true)

  const [batch, setBatch] = useState<BatchRun | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [pollWarning, setPollWarning] = useState('')
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Leaving the page mid-run must not leave a timer firing forever.
  useEffect(
    () => () => {
      if (pollRef.current) clearInterval(pollRef.current)
    },
    []
  )

  const upload = async () => {
    setLoading(true)
    setError('')
    try {
      let res
      if (mode === 'paste' && csvText.trim()) {
        res = await batchApi.uploadText(csvText, dryRun, ssl1, ssl2, automap, usecache)
      } else if (mode === 'file' && file) {
        res = await batchApi.upload(file, dryRun, ssl1, ssl2, automap, usecache)
      } else {
        setError('Add some CSV rows first — paste them or drop a file.')
        setLoading(false)
        return
      }
      setBatch(res.data)
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not read that CSV.'
      )
    } finally {
      setLoading(false)
    }
  }

  const start = async () => {
    if (!batch) return
    setLoading(true)
    try {
      const res = await batchApi.start(batch.id)
      setBatch(res.data)
      // A single failed poll used to stop the poll for good: the batch kept
      // running on the server while the page froze at whatever count it had
      // last seen, with no error, so a healthy run looked wedged.
      let misses = 0
      pollRef.current = setInterval(async () => {
        try {
          const r = await batchApi.get(batch.id)
          misses = 0
          setPollWarning('')
          setBatch(r.data)
          if (r.data.status !== 'running' && pollRef.current) clearInterval(pollRef.current)
        } catch {
          misses += 1
          if (misses >= 3) {
            setPollWarning(
              'Lost contact with the server. The batch is probably still running — reload to check.'
            )
            if (pollRef.current) clearInterval(pollRef.current)
          }
        }
      }, 3000)
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not start the batch.'
      )
    } finally {
      setLoading(false)
    }
  }

  const stop = async () => {
    if (!batch) return
    setLoading(true)
    try {
      await batchApi.abort(batch.id)
      setError('')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not stop the batch.'
      )
    } finally {
      setLoading(false)
    }
  }

  const reset = () => {
    setBatch(null)
    setFile(null)
    setCsvText('')
    setError('')
    if (pollRef.current) clearInterval(pollRef.current)
  }

  const done = batch ? batch.completed_rows + batch.failed_rows : 0
  const pct = batch && batch.total_rows ? (done / batch.total_rows) * 100 : 0

  /* ------------------------------------------------ after upload: review */
  if (batch) {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader
          title="Batch Import"
          description={`${batch.filename} — ${batch.total_rows} mailbox(es)`}
          actions={
            <Button variant="outline" onClick={reset}>
              Start over
            </Button>
          }
        />

        <div className="rounded-lg border border-border bg-card">
          <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
            <StatusBadge status={batch.status} />
            {dryRun && <Badge tone="warning">Dry run</Badge>}
            <span className="ml-auto text-xs text-muted-foreground">Batch #{batch.id}</span>
          </header>

          <div className="space-y-4 p-4">
            <div className="grid grid-cols-3 gap-3">
              <Stat label="Total" value={batch.total_rows} />
              <Stat
                label="Completed"
                value={batch.completed_rows}
                icon={CheckCircle2}
                tone={batch.completed_rows > 0 ? 'success' : 'default'}
              />
              <Stat
                label="Failed"
                value={batch.failed_rows}
                icon={AlertCircle}
                tone={batch.failed_rows > 0 ? 'danger' : 'default'}
              />
            </div>

            {batch.status === 'running' && (
              <>
                <Progress value={pct} label={`${done} of ${batch.total_rows} mailboxes`} />
                <Button
                  variant="outline"
                  onClick={stop}
                  disabled={loading}
                  className="text-danger hover:bg-danger-soft"
                >
                  <Square className="mr-1.5 h-3 w-3" />
                  Stop after this mailbox
                </Button>
                <p className="text-xs text-muted-foreground">
                  Stopping aborts the mailbox in flight. Mailboxes already copied stay copied.
                </p>
              </>
            )}

            {batch.status === 'aborted' && (
              <p className="text-sm text-muted-foreground">
                Stopped. {batch.completed_rows} mailbox(es) finished before it was cancelled — each
                one is its own job in History.
              </p>
            )}

            {batch.status === 'pending' && (
              <>
                <p className="rounded-md bg-warning-soft px-3 py-2 text-sm text-warning">
                  Nothing has run yet. Starting will process {batch.total_rows} mailbox
                  {batch.total_rows === 1 ? '' : 'es'}, one at a time
                  {dryRun ? ', writing nothing.' : ', writing into each destination.'}
                </p>
                <Button onClick={start} disabled={loading}>
                  {loading ? (
                    <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
                  ) : (
                    <PlayCircle className="mr-1.5 h-4 w-4" />
                  )}
                  Run batch
                </Button>
              </>
            )}

            {batch.status === 'completed' && (
              <p className="text-sm text-muted-foreground">
                Finished. Each mailbox is a separate job in History.
              </p>
            )}

            {pollWarning && (
              <p role="alert" className="rounded-md bg-warning-soft px-3 py-2 text-sm text-warning">
                {pollWarning}
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
        </div>
      </div>
    )
  }

  /* ---------------------------------------------------- before upload */
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="Batch Import" description="Migrate many mailboxes from one CSV." />

      <div className="mb-4 rounded-lg border border-border bg-card">
        <header className="flex items-center gap-1 border-b border-border px-2 py-2">
          {(['paste', 'file'] as const).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              aria-pressed={mode === m}
              className={cn(
                'rounded-md px-3 py-1.5 text-sm transition-colors',
                mode === m
                  ? 'bg-primary-soft font-medium text-primary'
                  : 'text-muted-foreground hover:bg-accent'
              )}
            >
              {m === 'paste' ? 'Paste rows' : 'Upload file'}
            </button>
          ))}
        </header>

        <div className="space-y-3 p-4">
          {mode === 'paste' ? (
            <>
              <textarea
                value={csvText}
                onChange={(e) => setCsvText(e.target.value)}
                rows={8}
                spellCheck={false}
                placeholder={SAMPLE}
                className="w-full rounded-md border border-input bg-card px-3 py-2 font-mono text-xs leading-relaxed"
                aria-label="CSV rows"
              />
              {!csvText && (
                <Button variant="outline" size="sm" onClick={() => setCsvText(SAMPLE)}>
                  Insert example
                </Button>
              )}
            </>
          ) : (
            <div
              onDragOver={(e) => {
                e.preventDefault()
                setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragging(false)
                const f = e.dataTransfer.files[0]
                if (f) setFile(f)
              }}
              onClick={() => document.getElementById('csv-input')?.click()}
              // A div with onClick and a hidden input is unreachable by
              // keyboard, so the whole "Upload file" tab was mouse-only.
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  document.getElementById('csv-input')?.click()
                }
              }}
              aria-label="Choose a CSV file, or drop one here"
              className={cn(
                'cursor-pointer rounded-lg border-2 border-dashed p-8 text-center transition-colors',
                'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary',
                dragging ? 'border-primary bg-primary-soft' : 'border-border hover:border-primary'
              )}
            >
              <input
                id="csv-input"
                type="file"
                accept=".csv,.txt"
                className="hidden"
                onChange={(e) => setFile(e.target.files?.[0] || null)}
              />
              <Upload className="mx-auto mb-2 h-6 w-6 text-muted-foreground" aria-hidden />
              {file ? (
                <p className="text-sm font-medium">{file.name}</p>
              ) : (
                <>
                  <p className="text-sm font-medium">Drop a .csv here</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">or click to choose a file</p>
                </>
              )}
            </div>
          )}

          <div className="rounded-md bg-subtle p-3 text-xs text-muted-foreground">
            <p className="mb-1 font-medium text-foreground">Format</p>
            <code className="font-mono">host1;user1;password1;host2;user2;password2</code>
            <p className="mt-1.5">
              One mailbox per line. Lines starting with <code className="font-mono">#</code> are
              comments. A seventh column can carry extra imapsync options.
            </p>
          </div>
        </div>
      </div>

      <div className="mb-4 rounded-lg border border-border bg-card p-4">
        <p className="mb-3 text-sm font-medium">Applies to every row</p>
        <div className="grid gap-2.5 sm:grid-cols-2">
          {[
            {
              v: dryRun,
              set: setDryRun,
              label: 'Dry run',
              hint: 'Report only, write nothing',
            },
            {
              v: automap,
              set: setAutomap,
              label: 'Automap folders',
              hint: 'Match Sent/Drafts/Trash',
            },
            {
              v: usecache,
              set: setUsecache,
              label: 'Use cache',
              hint: 'Skip already-copied messages',
            },
            {
              v: ssl1,
              set: setSsl1,
              label: 'TLS on source',
              hint: 'Encrypt the source connection',
            },
            {
              v: ssl2,
              set: setSsl2,
              label: 'TLS on destination',
              hint: 'Encrypt the destination',
            },
          ].map((o) => (
            <label key={o.label} className="flex items-start gap-2.5 text-sm">
              <input
                type="checkbox"
                checked={o.v}
                onChange={(e) => o.set(e.target.checked)}
                className="mt-0.5 h-4 w-4"
              />
              <span>
                {o.label}
                <span className="block text-xs text-muted-foreground">{o.hint}</span>
              </span>
            </label>
          ))}
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="mb-4 flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger"
        >
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Uploading only parses and previews; nothing runs until you confirm on
          the next screen. */}
      <Button onClick={upload} disabled={loading}>
        {loading ? (
          <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
        ) : (
          <FileSpreadsheet className="mr-1.5 h-4 w-4" />
        )}
        Parse rows
      </Button>
    </div>
  )
}
