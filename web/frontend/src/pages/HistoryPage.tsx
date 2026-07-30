import { useEffect, useRef, useState } from 'react'
import { History, Search, Trash2, X } from 'lucide-react'
import { historyApi, syncApi } from '@/lib/api'
import { useJobWebSocket } from '@/lib/ws'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/ui/badge'
import {
  EmptyState,
  PageHeader,
  Table,
  TableSkeleton,
  Td,
  Th,
  Tr,
  formatBytes,
  formatDateTime,
  formatDuration,
} from '@/components/ui/data'

interface Job {
  id: number
  status: string
  source_account: string | null
  dest_account: string | null
  started_at: string | null
  finished_at: string | null
  messages_transferred: number
  bytes_transferred: number
  errors_count: number
  summary: string | null
  profile_id: number | null
}

const STATUSES = ['completed', 'failed', 'aborted', 'interrupted', 'running']

// A finished job's log never changes, so it is fetched once. A running job's
// does, and the fetch used to be the only thing that ever happened — the modal
// showed whatever existed at the moment it opened and then sat there.
function LogViewer({
  id,
  initial,
  live,
  onClose,
}: {
  id: number
  initial: string
  live: boolean
  onClose: () => void
}) {
  const { logs, jobStatus } = useJobWebSocket(live ? id : null)
  const boxRef = useRef<HTMLPreElement>(null)
  const [follow, setFollow] = useState(true)

  // The socket replays the log it has, so once it has said anything it is the
  // more current of the two and the initial fetch would only duplicate it.
  const text = logs.length > 0 ? logs.join('\n') : initial
  const streaming = live && jobStatus === null

  useEffect(() => {
    if (follow && boxRef.current) {
      boxRef.current.scrollTop = boxRef.current.scrollHeight
    }
  }, [text, follow])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button className="absolute inset-0 bg-black/60" onClick={onClose} aria-label="Close log" />
      <div className="relative flex max-h-[85vh] w-full max-w-4xl flex-col rounded-lg border border-border bg-card shadow-xl">
        <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">Log for job #{id}</h2>
          {streaming && (
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-info" aria-hidden />
              Live
            </span>
          )}
          <label className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground">
            {streaming && (
              <>
                <input
                  type="checkbox"
                  checked={follow}
                  onChange={(e) => setFollow(e.target.checked)}
                  className="h-3.5 w-3.5"
                />
                Follow
              </>
            )}
          </label>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </Button>
        </header>
        <pre
          ref={boxRef}
          className="flex-1 overflow-auto bg-subtle p-4 font-mono text-xs leading-relaxed"
        >
          {text || 'No log recorded for this job.'}
        </pre>
      </div>
    </div>
  )
}

export function HistoryPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [log, setLog] = useState<{ id: number; text: string; live: boolean } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Job | null>(null)
  const [showPurge, setShowPurge] = useState(false)
  const [purgeDays, setPurgeDays] = useState(30)
  const [notice, setNotice] = useState('')
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    setLoading(true)
    const params: Record<string, unknown> = { page, per_page: 20 }
    if (status) params.status = status
    if (query) params.q = query
    historyApi
      .list(params)
      .then((r) => setJobs(r.data))
      .catch(() => setJobs([]))
      .finally(() => setLoading(false))
  }, [page, status, query, reloadKey])

  // A migration runs for hours. Without this the row that matters most sits at
  // 0 messages and a frozen clock until someone thinks to reload the page.
  const anyRunning = jobs.some((j) => j.status === 'running')
  useEffect(() => {
    if (!anyRunning) return
    const t = setInterval(() => {
      const params: Record<string, unknown> = { page, per_page: 20 }
      if (status) params.status = status
      if (query) params.q = query
      historyApi
        .list(params)
        .then((r) => setJobs(r.data))
        .catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [anyRunning, page, status, query])

  const viewLog = async (id: number, live: boolean) => {
    const res = await syncApi.log(id)
    setLog({ id, text: res.data.log, live })
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await syncApi.delete(confirmDelete.id)
      setNotice(`Job #${confirmDelete.id} and its log were deleted.`)
    } catch (err: unknown) {
      setNotice(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          'Could not delete that job.',
      )
    }
    setConfirmDelete(null)
    setReloadKey((k) => k + 1)
  }

  const doPurge = async () => {
    try {
      const r = await historyApi.purge(purgeDays)
      setNotice(
        r.data.deleted === 0
          ? 'Nothing was old enough to clear.'
          : `Cleared ${r.data.deleted} job(s) and their logs.`,
      )
    } catch {
      setNotice('Could not clear the history.')
    }
    setShowPurge(false)
    setPage(1)
    setReloadKey((k) => k + 1)
  }

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader
        title="History"
        description="Every sync this account has run."
        actions={
          <Button variant="outline" onClick={() => setShowPurge(true)}>
            <Trash2 className="mr-1.5 h-4 w-4" />
            Clear old
          </Button>
        }
      />

      {notice && (
        <div
          role="status"
          className="mb-3 flex items-center justify-between gap-2 rounded-md bg-subtle px-3 py-2 text-sm"
        >
          <span>{notice}</span>
          <button onClick={() => setNotice('')} aria-label="Dismiss">
            <X className="h-4 w-4 text-muted-foreground" />
          </button>
        </div>
      )}

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-0 flex-1 sm:max-w-xs">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            className="pl-8"
            placeholder="Search account or summary…"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setPage(1)
            }}
            aria-label="Search history"
          />
        </div>

        {/* Status is a small closed set, so chips beat a select: one tap, and
            the active filter is visible without opening anything. */}
        <div className="flex flex-wrap gap-1">
          <button
            onClick={() => {
              setStatus('')
              setPage(1)
            }}
            className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
              status === ''
                ? 'bg-primary text-primary-foreground'
                : 'bg-subtle text-muted-foreground hover:bg-accent'
            }`}
          >
            All
          </button>
          {STATUSES.map((s) => (
            <button
              key={s}
              onClick={() => {
                setStatus(s)
                setPage(1)
              }}
              className={`rounded-full px-2.5 py-1 text-xs capitalize transition-colors ${
                status === s
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-subtle text-muted-foreground hover:bg-accent'
              }`}
            >
              {s}
            </button>
          ))}
        </div>
      </div>

      <div className="rounded-lg border border-border bg-card">
        {loading ? (
          <TableSkeleton rows={6} cols={5} />
        ) : jobs.length === 0 ? (
          <EmptyState
            icon={History}
            title={status || query ? 'No jobs match that filter' : 'No jobs yet'}
            description={
              status || query
                ? 'Try a different status or clear the search.'
                : 'Once you run a sync it will be recorded here.'
            }
            action={
              (status || query) && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setStatus('')
                    setQuery('')
                  }}
                >
                  Clear filters
                </Button>
              )
            }
          />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th className="w-14" align="right">
                  #
                </Th>
                <Th>Route</Th>
                <Th>Status</Th>
                <Th align="right">Messages</Th>
                <Th align="right">Data</Th>
                <Th align="right">Took</Th>
                <Th>Started</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {jobs.map((j) => (
                <Tr key={j.id}>
                  <Td align="right" mono className="text-muted-foreground">
                    {j.id}
                  </Td>
                  <Td>
                    <p className="truncate text-sm">{j.source_account ?? '—'}</p>
                    <p className="truncate text-xs text-muted-foreground">→ {j.dest_account ?? '—'}</p>
                  </Td>
                  <Td>
                    <StatusBadge status={j.status} />
                    {j.errors_count > 0 && (
                      <span className="ml-1.5 text-xs text-danger">{j.errors_count} err</span>
                    )}
                  </Td>
                  <Td align="right" className="tabular">
                    {j.messages_transferred.toLocaleString()}
                  </Td>
                  <Td align="right" className="tabular text-muted-foreground">
                    {formatBytes(j.bytes_transferred)}
                  </Td>
                  <Td align="right" className="tabular text-muted-foreground">
                    {formatDuration(j.started_at, j.finished_at)}
                  </Td>
                  <Td mono className="whitespace-nowrap text-muted-foreground">
                    {formatDateTime(j.started_at)}
                  </Td>
                  <Td align="right">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => viewLog(j.id, j.status === 'running')}
                      >
                        Log
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-9 w-9 text-muted-foreground hover:bg-danger-soft hover:text-danger"
                        onClick={() => setConfirmDelete(j)}
                        aria-label={`Delete job ${j.id}`}
                        disabled={j.status === 'running'}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
      </div>

      <div className="mt-3 flex items-center justify-between">
        <p className="text-xs text-muted-foreground">Page {page}</p>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page === 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={jobs.length < 20}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </Button>
        </div>
      </div>

      {/* Deleting a job also deletes its log, which is the part that takes real
          space, so the dialog says so rather than implying only a row goes. */}
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
            <h2 className="text-sm font-semibold">Delete job #{confirmDelete.id}?</h2>
            <p className="mt-1.5 text-sm text-muted-foreground">
              The record and its saved log are removed. Messages already copied into the
              destination mailbox are not touched.
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="destructive" onClick={doDelete}>Delete job</Button>
            </div>
          </div>
        </div>
      )}

      {showPurge && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <button
            className="absolute inset-0 bg-black/60"
            onClick={() => setShowPurge(false)}
            aria-label="Cancel"
          />
          <div
            role="dialog"
            aria-modal="true"
            className="relative w-full max-w-sm rounded-lg border border-border bg-card p-5 shadow-xl"
          >
            <h2 className="text-sm font-semibold">Clear old history</h2>
            <p className="mt-1.5 text-sm text-muted-foreground">
              Removes finished jobs and their logs. Running jobs are never touched.
            </p>
            <div className="mt-4">
              <label className="mb-1 block text-sm font-medium" htmlFor="days">
                Older than
              </label>
              <div className="flex flex-wrap gap-1.5">
                {[7, 30, 90, 0].map((d) => (
                  <button
                    key={d}
                    onClick={() => setPurgeDays(d)}
                    className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
                      purgeDays === d
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-subtle text-muted-foreground hover:bg-accent'
                    }`}
                  >
                    {d === 0 ? 'Everything' : `${d} days`}
                  </button>
                ))}
              </div>
            </div>
            {purgeDays === 0 && (
              <p className="mt-3 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">
                This clears every finished job in your history.
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setShowPurge(false)}>Cancel</Button>
              <Button variant="destructive" onClick={doPurge}>
                {purgeDays === 0 ? 'Clear everything' : `Clear older than ${purgeDays} days`}
              </Button>
            </div>
          </div>
        </div>
      )}

      {log && <LogViewer id={log.id} initial={log.text} live={log.live} onClose={() => setLog(null)} />}
    </div>
  )
}
