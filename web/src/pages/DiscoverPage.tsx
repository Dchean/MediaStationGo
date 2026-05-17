import { useEffect, useState } from 'react'
import { Sparkles } from 'lucide-react'

import { discoverAPI, type DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'

// DiscoverPage shows TMDb trending + popular rails. Rows are empty when
// no TMDb API key is configured (the backend simply returns []).
export function DiscoverPage() {
  const [trending, setTrending] = useState<DiscoverItem[]>([])
  const [popular, setPopular] = useState<DiscoverItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      discoverAPI.trending().catch(() => [] as DiscoverItem[]),
      discoverAPI.popular().catch(() => [] as DiscoverItem[]),
    ])
      .then(([t, p]) => {
        setTrending(t)
        setPopular(p)
      })
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-10">
      <header className="flex items-center gap-3">
        <Sparkles className="h-6 w-6 text-primary-400" />
        <div>
          <h1 className="font-display text-3xl font-bold text-white">发现</h1>
            <p className="text-sm text-slate-400">
            来自 TMDb 的当日热门与流行榜单（需在外部 API 中配置 TMDb API Key）。
          </p>
        </div>
      </header>

      {loading && <p className="text-slate-500">加载中…</p>}

      {trending.length > 0 && <Row title="今日趋势" items={trending} />}
      {popular.length > 0 && <Row title="热门电影" items={popular} />}
      {!loading && trending.length === 0 && popular.length === 0 && (
        <div className="glass-panel">
            <p className="text-slate-300">
            还没有可用的发现数据。请前往 <span className="text-primary-400">管理后台 → 外部API</span>
            ，配置 TMDb API Key 后刷新页面。
          </p>
        </div>
      )}
    </div>
  )
}

function Row({ title, items }: { title: string; items: DiscoverItem[] }) {
  return (
    <section className="space-y-3">
      <h2 className="font-display text-xl font-semibold text-white">{title}</h2>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((m) => (
          <DiscoverCard key={m.tmdb_id} item={m} />
        ))}
      </div>
    </section>
  )
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
  return (
    <div className="overflow-hidden rounded-xl border border-white/5 bg-surface-800/60">
      <div className="aspect-[2/3] w-full bg-surface-900">
        {item.poster_url ? (
          <img
            src={imageURL(item.poster_url)}
            alt={item.title}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-slate-600">
            无海报
          </div>
        )}
      </div>
      <div className="px-3 py-2">
        <p className="truncate text-sm font-medium text-white">{item.title}</p>
        <div className="flex items-center justify-between text-xs text-slate-400">
          {item.year > 0 && <span>{item.year}</span>}
          {item.rating > 0 && (
            <span className="rounded border border-yellow-400/40 px-1.5 py-0.5 text-yellow-400">
              ★ {item.rating.toFixed(1)}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}
