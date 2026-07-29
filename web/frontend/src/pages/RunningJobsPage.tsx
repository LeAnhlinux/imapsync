import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight, Inbox, Square } from 'lucide-react'
import { syncApi } from '@/lib/api'
import { useJobWebSocket } from '@/lib/ws'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/ui/badge'
import {
  EmptyState,
  PageHeader,
  Progress,
  formatBytes,
  formatDuration,
} from '@/components/ui/data'

interface Job {
  id: number
  status: string
  pid: number | null
  started_at: string | null
  source_account: string | null
  dest_account: string | null
  profile_id: number | null
  messages_transferred: number
  bytes_transferred: number
}

export function RunningJobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [open, setOpen] = useState<number | null>(null)
  const [aborting, setAborting] = useState<number | null>(null)

  useEffect(() => {
    const load = () => {
      syncApi.running().then((r) => setJobs(r.data)).catch(() => {})
    }
    load()
    const interval = setInterval(load, 5000)
    return () => clearInterval(interval)
  }, [])

  const abort = async (id: number) => {
    setAborting(id)
    try {
      await syncApi.abort(id)
      setJobs((prev) => prev.filter((j) => j.id !== id))
    } finally {
      setAborting(null)
    }
  }

  return (
    <div className="mx-auto max-w-5xl">
      <PageHeader
        title="Running Jobs"
        description="Live progress and output for syncs in flight."
      />

      {jobs.length === 0 ? (
        <div className="rounded-lg border border-border bg-card">
          <EmptyState
            icon={Inbox}
            title="Nothing running"
            description="Syncs you start will appear here until they finish."
            action={
              <Link
                to="/sync/new"
                className="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm transition-colors hover:bg-accent"
              >
                Start a sync
              </Link>
            }
          />
        </div>
      ) : (
        <div className="space-y-3">
          {jobs.map((job) => {
            const expanded = open === job.id
            return (
              <div key={job.id} className="overflow-hidden rounded-lg border border-border bg-card">
                <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <button
                    onClick={() => setOpen(expanded ? null : job.id)}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                    aria-expanded={expanded}
                  >
                    {expanded ? (
                      <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                    )}
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">
                        {job.source_account ?? `Job #${job.id}`}
                      </p>
                      <p className="truncate text-xs text-muted-foreground">
                        → {job.dest_account ?? '—'}
                      </p>
                    </div>
                  </button>

                  <StatusBadge status={job.status} />
                  <span className="tabular hidden text-xs text-muted-foreground sm:inline">
                    {formatDuration(job.started_at)}
                  </span>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => abort(job.id)}
                    disabled={aborting === job.id}
                    className="text-danger hover:bg-danger-soft"
                  >
                    <Square className="mr-1.5 h-3 w-3" />
                    {aborting === job.id ? 'Stopping…' : 'Stop'}
                  </Button>
                </div>

                <div className="flex flex-wrap gap-x-5 gap-y-1 border-t border-border bg-subtle px-4 py-2 text-xs text-muted-foreground">
                  <span>
                    Job <span className="tabular font-mono">#{job.id}</span>
                  </span>
                  <span>
                    PID <span className="tabular font-mono">{job.pid ?? '—'}</span>
                  </span>
                  <span>
                    Messages <span className="tabular">{job.messages_transferred.toLocaleString()}</span>
                  </span>
                  <span>
                    Data <span className="tabular">{formatBytes(job.bytes_transferred)}</span>
                  </span>
                </div>

                {expanded && <LiveLog jobId={job.id} />}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function LiveLog({ jobId }: { jobId: number }) {
  const { logs, progress, jobStatus } = useJobWebSocket(jobId)
  const endRef = useRef<HTMLDivElement>(null)
  const [fallback, setFallback] = useState<string[]>([])
  const [stick, setStick] = useState(true)

  // Only follow the tail while the user is already at the bottom; yanking the
  // view back down while they are reading further up is worse than not
  // following at all.
  useEffect(() => {
    if (stick) endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs, fallback, stick])

  // If the socket produced nothing after 3s, fall back to the REST log so an
  // already-finished job still shows its output.
  useEffect(() => {
    const timer = setTimeout(() => {
      if (logs.length === 0) {
        syncApi
          .log(jobId)
          .then((r) => {
            const text = r.data?.log || ''
            if (text) setFallback(text.split('\n'))
          })
          .catch(() => {})
      }
    }, 3000)
    return () => clearTimeout(timer)
  }, [jobId, logs.length])

  const lines = logs.length > 0 ? logs : fallback

  return (
    <div className="border-t border-border p-4">
      {progress && (
        <div className="mb-3">
          <Progress
            value={progress.percent}
            label={`Folder ${progress.folder}${progress.eta ? ` · ETA ${progress.eta}` : ''}`}
          />
        </div>
      )}

      {jobStatus && (
        <p className="mb-2">
          <StatusBadge status={jobStatus} />
        </p>
      )}

      <div
        onScroll={(e) => {
          const el = e.currentTarget
          setStick(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
        }}
        className="max-h-96 overflow-y-auto rounded-md bg-subtle p-3 font-mono text-xs leading-relaxed"
      >
        {lines.length === 0 ? (
          <p className="text-muted-foreground">Waiting for output…</p>
        ) : (
          lines.map((line, i) => (
            <div key={i} className="whitespace-pre-wrap break-all">
              {line}
            </div>
          ))
        )}
        <div ref={endRef} />
      </div>
    </div>
  )
}
