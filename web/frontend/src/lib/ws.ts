import { useEffect, useRef, useState, useCallback } from 'react'

export interface WsMessage {
  type: 'log' | 'progress' | 'status'
  data?: string
  percent?: number
  eta?: string
  folder?: string
  status?: string
  exit_code?: number
}

export function useJobWebSocket(jobId: number | null) {
  const [logs, setLogs] = useState<string[]>([])
  const [progress, setProgress] = useState<{ percent: number; eta: string; folder: string } | null>(null)
  const [jobStatus, setJobStatus] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const connect = useCallback(() => {
    if (!jobId) return

    const token = localStorage.getItem('token')
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/ws/jobs/${jobId}?token=${token}`)

    ws.onmessage = (event) => {
      const msg: WsMessage = JSON.parse(event.data)
      switch (msg.type) {
        case 'log':
          setLogs((prev) => [...prev, msg.data || ''])
          break
        case 'progress':
          setProgress({ percent: msg.percent || 0, eta: msg.eta || '', folder: msg.folder || '' })
          break
        case 'status':
          setJobStatus(msg.status || null)
          break
      }
    }

    ws.onclose = () => {
      wsRef.current = null
    }

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

  return { logs, progress, jobStatus, clear }
}
