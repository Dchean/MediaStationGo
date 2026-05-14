import { useEffect, useRef } from 'react'

import { useAuthStore } from '../stores/auth'

// useWebSocket opens a single connection to /api/ws and dispatches every
// message to the supplied handler. Auto-reconnects with a 3 s back-off
// while the auth token is present.
export function useWebSocket(onEvent: (topic: string, payload: unknown) => void) {
  const ref = useRef<WebSocket | null>(null)
  const token = useAuthStore((s) => s.token)

  useEffect(() => {
    if (!token) return
    let closed = false
    let timer: number | undefined

    const open = () => {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const url = `${proto}//${window.location.host}/api/ws?token=${encodeURIComponent(token)}`
      const ws = new WebSocket(url)
      ref.current = ws
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data)
          if (msg && typeof msg.topic === 'string') {
            onEvent(msg.topic, msg.payload)
          }
        } catch {
          // ignore malformed frames
        }
      }
      ws.onclose = () => {
        if (closed) return
        timer = window.setTimeout(open, 3_000)
      }
    }

    open()
    return () => {
      closed = true
      if (timer) window.clearTimeout(timer)
      ref.current?.close()
    }
  }, [token, onEvent])
}
