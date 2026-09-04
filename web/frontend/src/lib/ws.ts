import { useEffect, useRef, useState, useCallback } from 'react'

export interface WsMessage {
  type: 'log' | 'progress' | 'status'
  data?: string
  percent?: number
  eta?: string
  folder?: string
  status?: string
  exit_code?: number
  messages?: number
  bytes?: number
}

export interface JobProgress {
  percent: number
  eta: string
  folder: string
  messages: number
  bytes: number
}

// A stable empty array, so a stale read does not produce a new reference on
// every render and retrigger effects that depend on logs.
const emptyLogs: string[] = []

export function useJobWebSocket(jobId: number | null) {
  // State is tagged with the job it belongs to, rather than cleared by an
  // effect when jobId changes. Resetting in an effect means a render where the
  // previous job's log is on screen under the new job's heading, and it is the
  // cascading-render pattern React now warns about.
  const [owner, setOwner] = useState<number | null>(null)
  const [rawLogs, setLogs] = useState<string[]>([])
  const [rawProgress, setProgress] = useState<JobProgress | null>(null)
  const [rawStatus, setJobStatus] = useState<string | null>(null)

  const stale = owner !== jobId
  const logs = stale ? emptyLogs : rawLogs
  const progress = stale ? null : rawProgress
  const jobStatus = stale ? null : rawStatus
  // Whether the socket is actually up. Without this a proxy idle timeout left
  // the log frozen while the UI kept pulsing "Live", so a user could watch a
  // dead pane believing they were seeing current output.
  const [rawConnected, setConnected] = useState(false)
  const connected = stale ? false : rawConnected
  const wsRef = useRef<WebSocket | null>(null)

  const connect = useCallback(() => {
    if (!jobId) return

    const token = localStorage.getItem('token')
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/api/ws/jobs/${jobId}?token=${token}`
    )

    // Claim the state for this job here, in an event handler's setup rather
    // than in an effect body, so switching jobs never renders the old job's
    // log under the new job's heading.
    setOwner(jobId)
    setLogs([])
    setProgress(null)
    setJobStatus(null)

    ws.onmessage = (event) => {
      let msg: WsMessage
      try {
        msg = JSON.parse(event.data)
      } catch {
        return // a malformed frame must not throw inside the handler
      }
      switch (msg.type) {
        case 'log':
          setLogs((prev) => [...prev, msg.data || ''])
          break
        case 'progress':
          setProgress({
            percent: msg.percent || 0,
            eta: msg.eta || '',
            folder: msg.folder || '',
            messages: msg.messages || 0,
            bytes: msg.bytes || 0,
          })
          break
        case 'status':
          setJobStatus(msg.status || null)
          break
      }
    }

    ws.onclose = () => {
      // Only clear the ref if this is still the current socket: a cleanup that
      // closed an older one would otherwise null out its replacement.
      if (wsRef.current === ws) wsRef.current = null
      setConnected(false)
    }

    ws.onerror = () => setConnected(false)
    ws.onopen = () => setConnected(true)

    wsRef.current = ws
  }, [jobId])

  useEffect(() => {
    // connect() opens the socket and claims the state for this job id, which is
    // the "subscribe to an external system" case effects exist for. The rule
    // sees the setState transitively and cannot tell the difference.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    connect()
    const ws = wsRef.current
    return () => {
      ws?.close()
    }
  }, [connect])

  const clear = useCallback(() => {
    setLogs([])
    setProgress(null)
    setJobStatus(null)
  }, [])

  return { logs, progress, jobStatus, connected, clear }
}
