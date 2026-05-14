import { useEffect, useState } from 'react'

import { playbackAPI } from '../api/playback'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

export function FavouritesPage() {
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    playbackAPI
      .listFavourites()
      .then(setItems)
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-white">我的收藏</h1>
      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && items.length === 0 && (
        <p className="text-slate-400">还没有任何收藏,点击媒体详情页的「收藏」按钮添加。</p>
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </div>
    </div>
  )
}
