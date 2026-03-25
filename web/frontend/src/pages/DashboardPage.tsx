import { useEffect, useState } from 'react'
import { historyApi, syncApi } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

interface Stats {
  total_syncs: number
  successful: number
  failed: number
  total_messages: number
  total_bytes: number
  success_rate: number
}

export function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [runningCount, setRunningCount] = useState(0)

  useEffect(() => {
    historyApi.stats().then((r) => setStats(r.data)).catch(() => {})
    syncApi.running().then((r) => setRunningCount(r.data.length)).catch(() => {})
  }, [])

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
  }

  return (
    <div>
      <h2 className="text-2xl font-bold mb-6">Dashboard</h2>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total Syncs</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{stats?.total_syncs ?? '-'}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Success Rate</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">
              {stats ? `${stats.success_rate.toFixed(1)}%` : '-'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Messages Transferred</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{stats?.total_messages?.toLocaleString() ?? '-'}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Data Transferred</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{stats ? formatBytes(stats.total_bytes) : '-'}</div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Running Jobs</CardTitle>
        </CardHeader>
        <CardContent>
          {runningCount > 0 ? (
            <p className="text-muted-foreground">{runningCount} job(s) currently running</p>
          ) : (
            <p className="text-muted-foreground">No jobs currently running</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
