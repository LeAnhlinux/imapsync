import { useState } from 'react'
import { batchApi } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

interface BatchRun {
  id: number
  filename: string
  total_rows: number
  completed_rows: number
  failed_rows: number
  status: string
}

export function BatchImportPage() {
  const [file, setFile] = useState<File | null>(null)
  const [batch, setBatch] = useState<BatchRun | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleUpload = async () => {
    if (!file) return
    setLoading(true)
    setError('')
    try {
      const res = await batchApi.upload(file)
      setBatch(res.data)
    } catch (err: unknown) {
      setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Upload failed')
    } finally {
      setLoading(false)
    }
  }

  const handleStart = async () => {
    if (!batch) return
    setLoading(true)
    try {
      const res = await batchApi.start(batch.id)
      setBatch(res.data)
      // Poll for updates
      const interval = setInterval(async () => {
        const r = await batchApi.get(batch.id)
        setBatch(r.data)
        if (r.data.status !== 'running') clearInterval(interval)
      }, 3000)
    } catch (err: unknown) {
      setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Start failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-2xl mx-auto">
      <h2 className="text-2xl font-bold mb-6">Batch Import</h2>

      <Card className="mb-6">
        <CardHeader>
          <CardTitle>Upload CSV</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            CSV format (semicolon separated):<br />
            <code className="text-xs">host1;user1;password1;host2;user2;password2;extra_options;</code>
          </p>

          <div
            className="border-2 border-dashed border-border rounded-lg p-8 text-center cursor-pointer hover:border-primary transition-colors"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault()
              const f = e.dataTransfer.files[0]
              if (f) setFile(f)
            }}
            onClick={() => document.getElementById('csv-input')?.click()}
          >
            <input
              id="csv-input"
              type="file"
              accept=".csv,.txt"
              className="hidden"
              onChange={(e) => setFile(e.target.files?.[0] || null)}
            />
            {file ? (
              <p className="font-medium">{file.name} ({(file.size / 1024).toFixed(1)} KB)</p>
            ) : (
              <p className="text-muted-foreground">Drop CSV file here or click to browse</p>
            )}
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {!batch && (
            <Button onClick={handleUpload} disabled={!file || loading}>
              {loading ? 'Uploading...' : 'Upload'}
            </Button>
          )}
        </CardContent>
      </Card>

      {batch && (
        <Card>
          <CardHeader>
            <CardTitle>Batch: {batch.filename}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div><strong>Status:</strong> {batch.status}</div>
              <div><strong>Total rows:</strong> {batch.total_rows}</div>
              <div><strong>Completed:</strong> {batch.completed_rows}</div>
              <div><strong>Failed:</strong> {batch.failed_rows}</div>
            </div>

            {batch.status === 'running' && (
              <div className="w-full bg-muted rounded-full h-2">
                <div
                  className="bg-primary h-2 rounded-full transition-all"
                  style={{ width: `${batch.total_rows > 0 ? ((batch.completed_rows + batch.failed_rows) / batch.total_rows) * 100 : 0}%` }}
                />
              </div>
            )}

            {batch.status === 'pending' && (
              <Button onClick={handleStart} disabled={loading}>
                {loading ? 'Starting...' : 'Start Batch'}
              </Button>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
