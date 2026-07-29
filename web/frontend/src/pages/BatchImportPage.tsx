import { useState, useRef } from 'react'
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
  const [csvText, setCsvText] = useState('')
  const [inputMode, setInputMode] = useState<'file' | 'paste'>('paste')
  const [dryRun, setDryRun] = useState(false)
  const [ssl1, setSsl1] = useState(true)
  const [ssl2, setSsl2] = useState(true)
  const [automap, setAutomap] = useState(true)
  const [usecache, setUsecache] = useState(true)
  const [batch, setBatch] = useState<BatchRun | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const handleUpload = async () => {
    setLoading(true)
    setError('')
    try {
      let res
      if (inputMode === 'paste' && csvText.trim()) {
        res = await batchApi.uploadText(csvText, dryRun, ssl1, ssl2, automap, usecache)
      } else if (inputMode === 'file' && file) {
        res = await batchApi.upload(file, dryRun, ssl1, ssl2, automap, usecache)
      } else {
        setError('Please provide CSV data')
        setLoading(false)
        return
      }
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
      pollRef.current = setInterval(async () => {
        try {
          const r = await batchApi.get(batch.id)
          setBatch(r.data)
          if (r.data.status !== 'running') {
            if (pollRef.current) clearInterval(pollRef.current)
          }
        } catch {
          if (pollRef.current) clearInterval(pollRef.current)
        }
      }, 3000)
    } catch (err: unknown) {
      setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Start failed')
    } finally {
      setLoading(false)
    }
  }

  const handleReset = () => {
    setBatch(null)
    setFile(null)
    setCsvText('')
    setError('')
    if (pollRef.current) clearInterval(pollRef.current)
  }

  return (
    <div className="max-w-3xl mx-auto">
      <h2 className="text-2xl font-bold mb-6">Batch Import</h2>

      <Card className="mb-6">
        <CardHeader>
          <CardTitle>CSV Data</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Format (semicolon separated):{' '}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">host1;user1;password1;host2;user2;password2</code>
            <br />
            Lines starting with <code className="text-xs bg-muted px-1 py-0.5 rounded">#</code> are comments. Empty lines are skipped.
          </p>

          {/* Input mode toggle */}
          <div className="flex gap-2">
            <Button
              size="sm"
              variant={inputMode === 'paste' ? 'default' : 'outline'}
              onClick={() => setInputMode('paste')}
            >
              Paste Text
            </Button>
            <Button
              size="sm"
              variant={inputMode === 'file' ? 'default' : 'outline'}
              onClick={() => setInputMode('file')}
            >
              Upload File
            </Button>
          </div>

          {inputMode === 'paste' ? (
            <textarea
              className="w-full h-48 p-3 border border-border rounded-md bg-background font-mono text-sm resize-y focus:outline-none focus:ring-2 focus:ring-primary"
              placeholder={`# Example:\nimap.gmail.com;user1@gmail.com;pass1;imap.other.com;user2@other.com;pass2\nimap.yandex.com;user3@yandex.com;pass3;mail.server.com;user3@server.com;pass3`}
              value={csvText}
              onChange={(e) => setCsvText(e.target.value)}
            />
          ) : (
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
          )}

          {/* Options */}
          <div className="flex flex-wrap gap-x-6 gap-y-3 pt-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={automap}
                onChange={(e) => setAutomap(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">Automap</span>
              <span className="text-xs text-muted-foreground">(auto map Sent/Drafts/Junk folders)</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={usecache}
                onChange={(e) => setUsecache(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">Use Cache</span>
              <span className="text-xs text-muted-foreground">(faster re-syncs)</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={ssl1}
                onChange={(e) => setSsl1(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">SSL Host1</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={ssl2}
                onChange={(e) => setSsl2(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">SSL Host2</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={dryRun}
                onChange={(e) => setDryRun(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">Dry Run</span>
              <span className="text-xs text-muted-foreground">(simulate only)</span>
            </label>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {!batch && (
            <Button onClick={handleUpload} disabled={loading || (inputMode === 'paste' ? !csvText.trim() : !file)}>
              {loading ? 'Processing...' : 'Upload & Parse'}
            </Button>
          )}
        </CardContent>
      </Card>

      {batch && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>Batch: {batch.filename}</CardTitle>
              {(batch.status === 'completed' || batch.status === 'failed') && (
                <Button size="sm" variant="outline" onClick={handleReset}>
                  New Batch
                </Button>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <strong>Status:</strong>{' '}
                <span className={
                  batch.status === 'completed' ? 'text-green-600' :
                  batch.status === 'failed' ? 'text-destructive' :
                  batch.status === 'running' ? 'text-blue-600' : ''
                }>
                  {batch.status}
                </span>
              </div>
              <div><strong>Total rows:</strong> {batch.total_rows}</div>
              <div><strong>Completed:</strong> <span className="text-green-600">{batch.completed_rows}</span></div>
              <div><strong>Failed:</strong> <span className="text-destructive">{batch.failed_rows}</span></div>
            </div>

            {dryRun && (
              <div className="text-xs text-amber-600 bg-amber-50 dark:bg-amber-950 px-3 py-2 rounded">
                ⚡ Dry Run mode — no messages will be transferred
              </div>
            )}

            {batch.status === 'running' && (
              <div>
                <div className="flex justify-between text-xs text-muted-foreground mb-1">
                  <span>Progress</span>
                  <span>{batch.completed_rows + batch.failed_rows} / {batch.total_rows}</span>
                </div>
                <div className="w-full bg-muted rounded-full h-2.5">
                  <div
                    className="bg-primary h-2.5 rounded-full transition-all duration-500"
                    style={{ width: `${batch.total_rows > 0 ? ((batch.completed_rows + batch.failed_rows) / batch.total_rows) * 100 : 0}%` }}
                  />
                </div>
              </div>
            )}

            {batch.status === 'pending' && (
              <Button onClick={handleStart} disabled={loading}>
                {loading ? 'Starting...' : `Start Batch (${batch.total_rows} jobs)`}
              </Button>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
