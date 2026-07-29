import { useEffect, useState, useRef } from 'react'
import { syncApi } from '@/lib/api'
import { useJobWebSocket } from '@/lib/ws'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

interface Job {
  id: number
  status: string
  pid: number | null
  started_at: string | null
  profile_id: number | null
  messages_transferred: number
  bytes_transferred: number
}

export function RunningJobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [selectedJob, setSelectedJob] = useState<number | null>(null)

  useEffect(() => {
    const load = () => {
      syncApi.running().then((r) => setJobs(r.data)).catch(() => {})
    }
    load()
    const interval = setInterval(load, 5000)
    return () => clearInterval(interval)
  }, [])

  return (
    <div>
      <h2 className="text-2xl font-bold mb-6">Running Jobs</h2>

      {jobs.length === 0 ? (
        <Card>
          <CardContent className="p-6">
            <p className="text-muted-foreground">No jobs currently running.</p>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {jobs.map((job) => (
            <Card key={job.id} className={selectedJob === job.id ? 'ring-2 ring-primary' : ''}>
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-lg">Job #{job.id}</CardTitle>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setSelectedJob(selectedJob === job.id ? null : job.id)}
                    >
                      {selectedJob === job.id ? 'Hide Logs' : 'View Logs'}
                    </Button>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={async () => {
                        await syncApi.abort(job.id)
                        setJobs((prev) => prev.filter((j) => j.id !== job.id))
                      }}
                    >
                      Abort
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="flex gap-4 text-sm text-muted-foreground">
                  <span>PID: {job.pid}</span>
                  <span>Started: {job.started_at ? new Date(job.started_at).toLocaleTimeString() : '-'}</span>
                  <span>Status: <span className="text-foreground font-medium">{job.status}</span></span>
                </div>
                {selectedJob === job.id && <LogViewer jobId={job.id} />}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function LogViewer({ jobId }: { jobId: number }) {
  const { logs, progress, jobStatus } = useJobWebSocket(jobId)
  const logEndRef = useRef<HTMLDivElement>(null)
  const [fallbackLogs, setFallbackLogs] = useState<string[]>([])

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs, fallbackLogs])

  // Fallback: fetch log via REST if WebSocket has no logs after 3s
  useEffect(() => {
    const timer = setTimeout(() => {
      if (logs.length === 0) {
        syncApi.log(jobId).then((r) => {
          const logText = r.data?.log || ''
          if (logText) setFallbackLogs(logText.split('\n'))
        }).catch(() => {})
      }
    }, 3000)
    return () => clearTimeout(timer)
  }, [jobId, logs.length])

  const displayLogs = logs.length > 0 ? logs : fallbackLogs

  return (
    <div className="mt-4 space-y-3">
      {/* Progress bar */}
      {progress && (
        <div>
          <div className="flex justify-between text-sm mb-1">
            <span>Folder {progress.folder}</span>
            <span>{progress.percent}%{progress.eta ? ` - ETA: ${progress.eta}` : ''}</span>
          </div>
          <div className="w-full bg-muted rounded-full h-2">
            <div
              className="bg-primary h-2 rounded-full transition-all duration-300"
              style={{ width: `${progress.percent}%` }}
            />
          </div>
        </div>
      )}

      {jobStatus && (
        <div className={`text-sm font-medium ${jobStatus === 'completed' ? 'text-green-600' : 'text-destructive'}`}>
          Job {jobStatus}
        </div>
      )}

      {/* Log output */}
      <div className="bg-muted rounded-md p-3 max-h-96 overflow-y-auto font-mono text-xs">
        {displayLogs.length === 0 ? (
          <p className="text-muted-foreground">Waiting for output...</p>
        ) : (
          displayLogs.map((line, i) => (
            <div key={i} className="whitespace-pre-wrap">{line}</div>
          ))
        )}
        <div ref={logEndRef} />
      </div>
    </div>
  )
}
