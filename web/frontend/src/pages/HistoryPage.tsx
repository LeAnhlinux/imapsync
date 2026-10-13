import { useCallback, useEffect, useRef, useState } from 'react'
import { History, Search, Trash2, X } from 'lucide-react'
import { historyApi, syncApi } from '@/lib/api'
import { useJobWebSocket } from '@/lib/ws'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/ui/badge'
import {
  Alert,
  Chip,
  EmptyState,
  Page,
  Section,
  Table,
  TableSkeleton,
  Td,
  Th,
  Tr,
} from '@/components/ui/data'
import { formatBytes, formatDateTime, formatDuration } from '@/lib/format'

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
  const { logs, jobStatus, connected } = useJobWebSocket(live ? id : null)
  const boxRef = useRef<HTMLPreElement>(null)
  const [follow, setFollow] = useState(true)

  // The socket replays the log it has, so once it has said anything it is the
  // more current of the two and the initial fetch would only duplicate it.
  const text = logs.length > 0 ? logs.join('\n') : initial
  const streaming = live && jobStatus === null && connected
  const lostContact = live && jobStatus === null && !connected && logs.length > 0

  useEffect(() => {
    if (follow && boxRef.current) {
      boxRef.current.scrollTop = boxRef.current.scrollHeight
    }
  }, [text, follow])

  return (
    <Dialog size="lg" onClose={onClose} labelledBy={`log-title-${id}`}>
      <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
        <h2 id={`log-title-${id}`} className="text-sm font-semibold">
          Log for job #{id}
        </h2>
        {streaming && (
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-info" aria-hidden />
            Live
          </span>
        )}
        {lostContact && (
          <span className="flex items-center gap-1.5 text-xs text-warning">
            <span className="h-1.5 w-1.5 rounded-full bg-warning" aria-hidden />
            Disconnected — reopen to catch up
          </span>
        )}
        <div className="ml-auto flex items-center gap-3">
          {streaming && (
            <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={follow}
                onChange={(e) => setFollow(e.target.checked)}
                className="h-4 w-4 cursor-pointer accent-primary"
              />
              Follow
            </label>
          )}
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </Button>
        </div>
      </header>
      <pre
        ref={boxRef}
        className="flex-1 overflow-auto bg-subtle p-4 font-mono text-xs leading-relaxed"
      >
        {text || 'No log recorded for this job.'}
      </pre>
    </Dialog>
  )
}

export function HistoryPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [log, setLog] = useState<{
    id: number
    text: string
    live: boolean
  } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Job | null>(null)
  const [showPurge, setShowPurge] = useState(false)
  const [purgeDays, setPurgeDays] = useState(30)
  const [notice, setNotice] = useState('')
  const [reloadKey, setReloadKey] = useState(0)
  const [total, setTotal] = useState<number | null>(null)

  // Every keystroke used to fire a request, and every response was applied
  // unconditionally — so if the response for "a" landed after the one for
  // "acme", the table showed every job while the box read "acme", with nothing
  // to say the list was stale.
  const [debouncedQuery, setDebouncedQuery] = useState('')
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query), 300)
    return () => clearTimeout(t)
  }, [query])

  // Both the filter effect and the poll below write to `jobs`. A sequence
  // number lets a response be dropped when a newer request has already gone
  // out, whichever of the two issued it.
  const seq = useRef(0)
  const fetchJobs = useCallback(
    (opts: { showSpinner?: boolean } = {}) => {
      const mine = ++seq.current
      if (opts.showSpinner) setLoading(true)
      const params: Record<string, unknown> = { page, per_page: 20 }
      if (status) params.status = status
      if (debouncedQuery) params.q = debouncedQuery
      historyApi
        .list(params)
        .then((r) => {
          if (mine !== seq.current) return
          setJobs(r.data)
          // The server has been sending this all along and nothing read it, so
          // the pager said "Page 3" with no idea how many there were and
          // guessed at "Next" from the row count.
          const n = Number(r.headers['x-total-count'])
          setTotal(Number.isFinite(n) ? n : null)
        })
        .catch(() => {
          if (mine === seq.current && opts.showSpinner) setJobs([])
        })
        .finally(() => {
          if (mine === seq.current && opts.showSpinner) setLoading(false)
        })
    },
    [page, status, debouncedQuery]
  )

  // Fetch when the page, status filter or debounced query changes. The rule
  // below objects to the setLoading inside fetchJobs, but showing a spinner
  // while a filtered request is in flight is the point: without it the table
  // silently displays the previous filter's rows until the response lands.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchJobs({ showSpinner: true })
  }, [fetchJobs, reloadKey])

  // A migration runs for hours. Without this the row that matters most sits at
  // 0 messages and a frozen clock until someone thinks to reload the page.
  const anyRunning = jobs.some((j) => j.status === 'running')
  useEffect(() => {
    if (!anyRunning) return
    const t = setInterval(() => fetchJobs(), 5000)
    return () => clearInterval(t)
  }, [anyRunning, fetchJobs])

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
          'Could not delete that job.'
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
          : `Cleared ${r.data.deleted} job(s) and their logs.`
      )
    } catch {
      setNotice('Could not clear the history.')
    }
    setShowPurge(false)
    setPage(1)
    setReloadKey((k) => k + 1)
  }

  return (
    <Page
      title="History"
      description="Every sync this account has run."
      actions={
        <Button variant="outline" onClick={() => setShowPurge(true)}>
          <Trash2 className="mr-1.5 h-4 w-4" />
          Clear old
        </Button>
      }
    >
      {notice && (
        <Alert tone="success">
          <div className="flex items-start justify-between gap-2">
            <span>{notice}</span>
            <button
              onClick={() => setNotice('')}
              aria-label="Dismiss"
              className="-my-1 -mr-1.5 grid h-7 w-7 shrink-0 cursor-pointer place-items-center rounded-md hover:bg-foreground/10"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </Alert>
      )}

      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="relative w-full min-w-0 sm:w-72">
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
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Filter by status">
          <Chip
            active={status === ''}
            onClick={() => {
              setStatus('')
              setPage(1)
            }}
          >
            All
          </Chip>
          {STATUSES.map((s) => (
            <Chip
              key={s}
              active={status === s}
              onClick={() => {
                setStatus(s)
                setPage(1)
              }}
            >
              <span className="capitalize">{s}</span>
            </Chip>
          ))}
        </div>
      </div>

      <Section flush>
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
                  <Td className="w-full min-w-56 max-w-0">
                    <p className="truncate text-sm" title={j.source_account ?? undefined}>
                      {j.source_account ?? '—'}
                    </p>
                    <p
                      className="truncate text-xs text-muted-foreground"
                      title={j.dest_account ?? undefined}
                    >
                      → {j.dest_account ?? '—'}
                    </p>
                  </Td>
                  <Td className="whitespace-nowrap">
                    <StatusBadge status={j.status} />
                    {j.errors_count > 0 && (
                      <span className="ml-1.5 text-xs text-danger">
                        {j.errors_count} {j.errors_count === 1 ? 'error' : 'errors'}
                      </span>
                    )}
                  </Td>
                  <Td align="right" className="tabular">
                    {j.messages_transferred.toLocaleString()}
                  </Td>
                  <Td align="right" className="tabular whitespace-nowrap text-muted-foreground">
                    {formatBytes(j.bytes_transferred)}
                  </Td>
                  <Td align="right" className="tabular whitespace-nowrap text-muted-foreground">
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
      </Section>

      <div className="flex items-center justify-between">
        <p className="tabular text-xs text-muted-foreground">
          {total === null
            ? `Page ${page}`
            : total === 0
              ? 'No jobs'
              : `${(page - 1) * 20 + 1}\u2013${Math.min(page * 20, total)} of ${total}`}
        </p>
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
            disabled={total === null ? jobs.length < 20 : page * 20 >= total}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </Button>
        </div>
      </div>

      {/* Deleting a job also deletes its log, which is the part that takes real
          space, so the dialog says so rather than implying only a row goes. */}
      {confirmDelete && (
        <Dialog
          title={`Delete job #${confirmDelete.id}?`}
          onClose={() => setConfirmDelete(null)}
          footer={
            <>
              <Button variant="outline" onClick={() => setConfirmDelete(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={doDelete}>
                Delete job
              </Button>
            </>
          }
        >
          <p className="mt-1.5 text-sm text-muted-foreground">
            The record and its saved log are removed. Messages already copied into the destination
            mailbox are not touched.
          </p>
        </Dialog>
      )}

      {showPurge && (
        <Dialog
          title="Clear old history"
          onClose={() => setShowPurge(false)}
          footer={
            <>
              <Button variant="outline" onClick={() => setShowPurge(false)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={doPurge}>
                {purgeDays === 0 ? 'Clear everything' : `Clear older than ${purgeDays} days`}
              </Button>
            </>
          }
        >
          <p className="mt-1.5 text-sm text-muted-foreground">
            Removes finished jobs and their logs. Running jobs are never touched.
          </p>
          <div className="mt-4">
            <span className="mb-1.5 block text-sm font-medium">Older than</span>
            <div className="flex flex-wrap gap-1.5">
              {[7, 30, 90, 0].map((d) => (
                <Chip key={d} active={purgeDays === d} onClick={() => setPurgeDays(d)}>
                  {d === 0 ? 'Everything' : `${d} days`}
                </Chip>
              ))}
            </div>
          </div>
          {purgeDays === 0 && (
            <Alert className="mt-3">This clears every finished job in your history.</Alert>
          )}
        </Dialog>
      )}

      {log && (
        <LogViewer id={log.id} initial={log.text} live={log.live} onClose={() => setLog(null)} />
      )}
    </Page>
  )
}
