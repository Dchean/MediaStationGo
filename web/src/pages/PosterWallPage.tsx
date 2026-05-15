import { useEffect, useState } from 'react'
import { GalleryHorizontalEnd } from 'lucide-react'

import { mediaAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

// PosterWallPage shows a dense poster grid of all media across every
// library — a visual "poster wall" that matches the original MediaStation's
// PosterWallView.vue. Loads the first 200 items sorted by rating.
export function PosterWallPage() {
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    mediaAPI
      .search('', 200)
      .then((d) => setItems(d.items))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <GalleryHorizontalEnd className="h-6 w-6 text-primary-400" />
        <div>
          <h1 className="font-display text-3xl font-bold text-white">海报墙</h1>
          <p className="text-sm text-slate-400">
            全部媒体的海报展示(最多 200 项)。
          </p>
        </div>
      </header>

      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && items.length === 0 && (
        <p className="text-slate-400">暂无媒体。请先添加媒体库并扫描。</p>
      )}
      <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-8">
        {items.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </div>
    </div>
  )
}
