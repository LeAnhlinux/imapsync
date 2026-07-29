import { useEffect, useState } from 'react'
import { History, Search, X } from 'lucide-react'
import { historyApi, syncApi } from '@/lib/api'
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

export function HistoryPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [log, setLog] = useState<{ id: number; text: string } | null>(null)

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
  }, [page, status, query])

  const viewLog = async (id: number) => {
    const res = await syncApi.log(id)
    setLog({ id, text: res.data.log })
  }

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader title="History" description="Every sync this account has run." />

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
                    <Button variant="ghost" size="sm" onClick={() => viewLog(j.id)}>
                      Log
                    </Button>
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

      {log && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <button
            className="absolute inset-0 bg-black/60"
            onClick={() => setLog(null)}
            aria-label="Close log"
          />
          <div className="relative flex max-h-[85vh] w-full max-w-4xl flex-col rounded-lg border border-border bg-card shadow-xl">
            <header className="flex items-center justify-between border-b border-border px-4 py-3">
              <h2 className="text-sm font-semibold">Log for job #{log.id}</h2>
              <Button variant="ghost" size="icon" onClick={() => setLog(null)} aria-label="Close">
                <X className="h-4 w-4" />
              </Button>
            </header>
            <pre className="flex-1 overflow-auto bg-subtle p-4 font-mono text-xs leading-relaxed">
              {log.text || 'No log recorded for this job.'}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
