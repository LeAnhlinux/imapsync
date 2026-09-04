import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Activity,
  ArrowRight,
  CheckCircle2,
  Database,
  Inbox,
  Mail,
  PlayCircle,
  XCircle,
} from 'lucide-react'
import { historyApi, syncApi } from '@/lib/api'
import { StatusBadge } from '@/components/ui/badge'
import { EmptyState, PageHeader, Skeleton, Stat, Table, Td, Th, Tr } from '@/components/ui/data'
import { formatBytes, formatDuration } from '@/lib/format'

interface Stats {
  total_syncs: number
  successful: number
  failed: number
  total_messages: number
  total_bytes: number
  success_rate: number
}

interface Job {
  id: number
  status: string
  source_account: string | null
  dest_account: string | null
  started_at: string | null
  finished_at: string | null
  messages_transferred: number
}

export function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [running, setRunning] = useState<Job[]>([])
  const [recent, setRecent] = useState<Job[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const load = () => {
      Promise.allSettled([
        historyApi.stats(),
        syncApi.running(),
        historyApi.list({ per_page: 5 }),
      ]).then(([s, r, h]) => {
        if (s.status === 'fulfilled') setStats(s.value.data)
        if (r.status === 'fulfilled') setRunning(r.value.data)
        if (h.status === 'fulfilled') setRecent(h.value.data)
        setLoading(false)
      })
    }
    load()
    // Running jobs move on their own, so the dashboard refreshes itself rather
    // than going stale until someone reloads the page.
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  }, [])

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader
        title="Dashboard"
        description="Migration activity across all your mailboxes."
        actions={
          <Link
            to="/sync/new"
            className="inline-flex h-10 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            <PlayCircle className="mr-1.5 h-4 w-4" />
            New Sync
          </Link>
        }
      />

      <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {loading ? (
          Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-[92px]" />)
        ) : (
          <>
            <Stat
              label="Total syncs"
              value={stats?.total_syncs ?? 0}
              hint={`${stats?.successful ?? 0} completed · ${stats?.failed ?? 0} failed`}
              icon={Activity}
            />
            <Stat
              label="Success rate"
              value={`${(stats?.success_rate ?? 0).toFixed(1)}%`}
              // Icon and tone from the same condition. They disagreed, so a
              // 95% rate with one failure drew a red X inside a green tile.
              icon={
                stats && stats.total_syncs > 0 && stats.success_rate < 90 ? XCircle : CheckCircle2
              }
              tone={
                !stats?.total_syncs ? 'default' : stats.success_rate >= 90 ? 'success' : 'danger'
              }
            />
            <Stat
              label="Messages"
              value={(stats?.total_messages ?? 0).toLocaleString()}
              hint="transferred in total"
              icon={Mail}
            />
            <Stat
              label="Data"
              value={formatBytes(stats?.total_bytes ?? 0)}
              hint="transferred in total"
              icon={Database}
            />
          </>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="rounded-lg border border-border bg-card">
          <header className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">Running now</h2>
            {running.length > 0 && (
              <Link to="/jobs" className="text-xs text-primary hover:underline">
                View all
              </Link>
            )}
          </header>
          {running.length === 0 ? (
            <EmptyState
              icon={Inbox}
              title="Nothing running"
              description="Start a sync and it will appear here with live progress."
              action={
                <Link
                  to="/sync/new"
                  className="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm transition-colors hover:bg-accent"
                >
                  Start a sync
                </Link>
              }
            />
          ) : (
            <ul className="divide-y divide-border">
              {running.map((j) => (
                <li key={j.id} className="flex items-center gap-3 px-4 py-3">
                  <StatusBadge status={j.status} />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm">{j.source_account}</p>
                    <p className="truncate text-xs text-muted-foreground">→ {j.dest_account}</p>
                  </div>
                  <span className="tabular shrink-0 text-xs text-muted-foreground">
                    {formatDuration(j.started_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="rounded-lg border border-border bg-card">
          <header className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">Recent jobs</h2>
            <Link
              to="/history"
              className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
            >
              History <ArrowRight className="h-3 w-3" />
            </Link>
          </header>
          {recent.length === 0 ? (
            <EmptyState
              icon={Activity}
              title="No jobs yet"
              description="Finished syncs will be listed here."
            />
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>Job</Th>
                  <Th>Status</Th>
                  <Th align="right">Messages</Th>
                </tr>
              </thead>
              <tbody>
                {recent.map((j) => (
                  <Tr key={j.id}>
                    <Td>
                      <p className="truncate text-sm">{j.source_account ?? `Job #${j.id}`}</p>
                      <p className="truncate text-xs text-muted-foreground">→ {j.dest_account}</p>
                    </Td>
                    <Td>
                      <StatusBadge status={j.status} />
                    </Td>
                    <Td align="right" className="tabular">
                      {j.messages_transferred.toLocaleString()}
                    </Td>
                  </Tr>
                ))}
              </tbody>
            </Table>
          )}
        </section>
      </div>
    </div>
  )
}
