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

export function useJobWebSocket(jobId: number | null) {
  const [logs, setLogs] = useState<string[]>([])
  const [progress, setProgress] = useState<JobProgress | null>(null)
  const [jobStatus, setJobStatus] = useState<string | null>(null)
  // Whether the socket is actually up. Without this a proxy idle timeout left
  // the log frozen while the UI kept pulsing "Live", so a user could watch a
  // dead pane believing they were seeing current output.
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)

  const connect = useCallback(() => {
    if (!jobId) return

    const token = localStorage.getItem('token')
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/ws/jobs/${jobId}?token=${token}`)

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
    connect()
    return () => {
      wsRef.current?.close()
    }
  }, [connect])

  const clear = useCallback(() => {
    setLogs([])
    setProgress(null)
    setJobStatus(null)
  }, [])

  return { logs, progress, jobStatus, connected, clear }
}
