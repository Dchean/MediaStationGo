import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Clock, Play } from 'lucide-react'

import { playbackAPI, type HistoryItem } from '../api/playback'
import { imageURL } from '../api/client'

function fmtDuration(ms: number): string {
  if (!ms || ms <= 0) return '—'
  const sec = Math.floor(ms / 1000)
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

// WatchHistoryPage shows a full paginated view of the user's playback
// history with progress bars and resume links.
export function WatchHistoryPage() {
  const [items, setItems] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    playbackAPI
      .recentHistory()
      .then(setItems)
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <Clock className="h-6 w-6 text-primary-400" />
        <h1 className="font-display text-3xl font-bold text-white">观看历史</h1>
      </header>

      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && items.length === 0 && (
        <p className="text-slate-400">暂无观看记录。</p>
      )}

      <div className="space-y-3">
        {items.map((h) => {
          const m = h.media
          if (!m) return null
          const progress =
            h.duration_ms > 0 ? h.position_ms / h.duration_ms : 0
          return (
            <div
              key={h.id}
              className="glass-panel flex items-center gap-4 !p-3"
            >
              <div className="h-16 w-12 shrink-0 overflow-hidden rounded bg-surface-900">
                {m.poster_url ? (
                  <img
                    src={imageURL(m.poster_url)}
                    alt={m.title}
                    className="h-full w-full object-cover"
                    referrerPolicy="no-referrer"
                  />
                ) : null}
              </div>
              <div className="flex-1 space-y-1">
                <Link
                  to={`/media/${m.id}`}
                  className="font-medium text-white transition hover:text-primary-400"
                >
                  {m.title}
                </Link>
                <div className="flex items-center gap-3 text-xs text-slate-400">
                  <span>{fmtDuration(h.position_ms)} / {fmtDuration(h.duration_ms)}</span>
                  <span>{new Date(h.watched_at).toLocaleString()}</span>
                  {h.completed && (
                    <span className="rounded border border-emerald-400/40 px-1.5 py-0.5 text-emerald-400">
                      已看完
                    </span>
                  )}
                </div>
                <div className="h-1 w-full overflow-hidden rounded bg-white/10">
                  <div
                    className="h-full bg-primary-400"
                    style={{ width: `${Math.round(progress * 100)}%` }}
                  />
                </div>
              </div>
              <Link
                to={`/play/${m.id}`}
                className="neon-button !px-3 !py-1 !text-xs"
              >
                <Play size={12} /> 继续
              </Link>
            </div>
          )
        })}
      </div>
    </div>
  )
}
