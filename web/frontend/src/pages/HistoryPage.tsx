import { useEffect, useState } from 'react'
import { historyApi, syncApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

interface Job {
  id: number
  status: string
  started_at: string | null
  finished_at: string | null
  messages_transferred: number
  bytes_transferred: number
  errors_count: number
  summary: string | null
  profile_id: number | null
}

export function HistoryPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [page, setPage] = useState(1)
  const [filter, setFilter] = useState('')
  const [selectedLog, setSelectedLog] = useState<{ id: number; log: string } | null>(null)

  const load = () => {
    const params: Record<string, unknown> = { page, per_page: 20 }
    if (filter) params.status = filter
    historyApi.list(params).then((r) => setJobs(r.data)).catch(() => {})
  }

  useEffect(() => { load() }, [page, filter])

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
  }

  const viewLog = async (jobId: number) => {
    const res = await syncApi.log(jobId)
    setSelectedLog({ id: jobId, log: res.data.log })
  }

  const statusColor = (s: string) => {
    switch (s) {
      case 'completed': return 'text-green-600'
      case 'failed': return 'text-red-600'
      case 'aborted': return 'text-yellow-600'
      default: return 'text-muted-foreground'
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold">History</h2>
        <select
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          value={filter}
          onChange={(e) => { setFilter(e.target.value); setPage(1) }}
        >
          <option value="">All</option>
          <option value="completed">Completed</option>
          <option value="failed">Failed</option>
          <option value="aborted">Aborted</option>
        </select>
      </div>

      {jobs.length === 0 ? (
        <p className="text-muted-foreground">No sync history yet.</p>
      ) : (
        <div className="space-y-2">
          <div className="grid grid-cols-7 gap-2 px-4 text-sm font-medium text-muted-foreground">
            <span>ID</span><span>Status</span><span>Started</span><span>Duration</span>
            <span>Messages</span><span>Data</span><span>Actions</span>
          </div>
          {jobs.map((j) => (
            <Card key={j.id}>
              <CardContent className="p-4 grid grid-cols-7 gap-2 items-center text-sm">
                <span>#{j.id}</span>
                <span className={`font-medium ${statusColor(j.status)}`}>{j.status}</span>
                <span>{j.started_at ? new Date(j.started_at).toLocaleString() : '-'}</span>
                <span>
                  {j.started_at && j.finished_at
                    ? `${Math.round((new Date(j.finished_at).getTime() - new Date(j.started_at).getTime()) / 1000)}s`
                    : '-'}
                </span>
                <span>{j.messages_transferred}</span>
                <span>{formatBytes(j.bytes_transferred)}</span>
                <Button size="sm" variant="outline" onClick={() => viewLog(j.id)}>Log</Button>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <div className="flex justify-center gap-2 mt-4">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</Button>
        <span className="text-sm py-2">Page {page}</span>
        <Button variant="outline" size="sm" disabled={jobs.length < 20} onClick={() => setPage(page + 1)}>Next</Button>
      </div>

      {/* Log modal */}
      {selectedLog && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={() => setSelectedLog(null)}>
          <div className="bg-card rounded-lg w-[90vw] max-w-4xl max-h-[80vh] overflow-hidden" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between p-4 border-b border-border">
              <h3 className="font-bold">Job #{selectedLog.id} Log</h3>
              <Button size="sm" variant="ghost" onClick={() => setSelectedLog(null)}>Close</Button>
            </div>
            <div className="p-4 overflow-auto max-h-[70vh]">
              <pre className="text-xs font-mono whitespace-pre-wrap">{selectedLog.log || 'No log available.'}</pre>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
