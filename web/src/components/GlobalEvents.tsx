import { useCallback } from 'react'
import toast from 'react-hot-toast'

import { useWebSocket } from '../hooks/useWebSocket'

// GlobalEvents subscribes to the WS hub and surfaces interesting events
// as toasts. Lives at the top of the component tree so every page sees
// the same stream without re-opening connections.
export function GlobalEvents() {
  const onEvent = useCallback((topic: string, payload: unknown) => {
    if (!payload || typeof payload !== 'object') return
    const p = payload as Record<string, unknown>
    if (topic === 'scan' && p.finished) {
      toast.success(`扫描完成:已添加 ${p.added ?? 0} 项,已探测 ${p.probed ?? 0} 项`)
    }
    if (topic === 'scrape' && p.finished) {
      toast.success(`刮削完成:成功匹配 ${p.matched ?? 0} 项`)
    }
    if (topic === 'subscription') {
      const queued = (p.queued as number | undefined) ?? 0
      if (queued > 0) toast.success(`订阅「${p.name}」已加入 ${queued} 项下载`)
    }
  }, [])

  useWebSocket(onEvent)
  return null
}
