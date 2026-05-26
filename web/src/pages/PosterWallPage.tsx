import { useEffect, useMemo, useState } from 'react'
import { GalleryHorizontalEnd, Layers } from 'lucide-react'

import { mediaAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import { groupSeries } from '../utils/groupSeries'
import type { Media } from '../types'

// PosterWallPage 把所有媒体的代表海报聚合到同一面墙，便于一目了然
// 浏览整个站点的内容。所有 episode 行会按剧集折叠，避免同一海报刷屏。
export function PosterWallPage() {
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // 拉得多一点（500），折叠后才能凑够展示量；后端 page_size 上限可能
    // 需要后续观察，暂以 500 试探。
    mediaAPI
      .search('', 500)
      .then((d) => setItems(d.items))
      .finally(() => setLoading(false))
  }, [])

  const cards = useMemo(() => groupSeries(items).slice(0, 240), [items])

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <GalleryHorizontalEnd className="h-6 w-6 text-brand-500" />
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">海报墙</h1>
          <p className="text-sm text-ink-50">
            按剧集聚合 · 共 {cards.length} 个条目
          </p>
        </div>
      </header>

      {loading && <p className="text-sand-500">加载中…</p>}
      {!loading && cards.length === 0 && (
        <p className="text-ink-50">暂无媒体。请先添加媒体库并扫描。</p>
      )}
      <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10">
        {cards.map((s) => (
          <div key={s.rep.id} className="relative">
            <MediaCard media={s.rep} />
            {s.count > 1 && (
              <span className="pointer-events-none absolute right-1.5 top-1.5 inline-flex items-center gap-0.5 rounded-md bg-black/60 px-1.5 py-0.5 text-[10px] font-medium text-ink-600 backdrop-blur-sm">
                <Layers size={10} />
                {s.count}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
