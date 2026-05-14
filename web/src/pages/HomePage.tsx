import { useEffect, useState } from 'react'

import { mediaAPI } from '../api/library'
import { playbackAPI, type HistoryItem } from '../api/playback'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

// Landing page composes three rows:
//   1. Continue Watching   — items with a positive position from the user's
//                            playback history (excluding completed ones).
//   2. Recently Added      — most recent media across every library.
//   3. Empty state hint    — shown when both rows are empty.
export function HomePage() {
  const [recent, setRecent] = useState<Media[]>([])
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      mediaAPI.search('', 24).then((d) => d.items),
      playbackAPI.recentHistory().catch(() => [] as HistoryItem[]),
    ])
      .then(([items, hist]) => {
        setRecent(items)
        setHistory(hist.filter((h) => !h.completed && !!h.media))
      })
      .finally(() => setLoading(false))
  }, [])

  const empty = !loading && recent.length === 0 && history.length === 0

  return (
    <div className="space-y-10">
      <header>
        <h1 className="font-display text-3xl font-bold text-white">主页</h1>
        <p className="text-sm text-slate-400">从你上次离开的地方继续</p>
      </header>

      {history.length > 0 && (
        <Row title="继续观看">
          {history.map((h) => {
            const m = h.media!
            const progress = h.duration_ms > 0 ? h.position_ms / h.duration_ms : 0
            return <MediaCard key={h.id} media={m} progress={progress} />
          })}
        </Row>
      )}

      {recent.length > 0 && (
        <Row title="最近添加">
          {recent.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </Row>
      )}

      {loading && <p className="text-slate-500">加载中…</p>}
      {empty && (
        <div className="glass-panel">
          <p className="text-slate-300">
            还没有任何媒体。前往 <span className="text-primary-400">管理后台</span>{' '}
            创建媒体库,然后触发一次扫描。
          </p>
        </div>
      )}
    </div>
  )
}

function Row({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="font-display text-xl font-semibold text-white">{title}</h2>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {children}
      </div>
    </section>
  )
}
