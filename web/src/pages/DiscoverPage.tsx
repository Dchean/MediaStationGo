import { useEffect, useState } from 'react'
import { Sparkles, AlertTriangle, ExternalLink, Wifi } from 'lucide-react'
import { Link } from 'react-router-dom'

import { discoverAPI, type DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'

// 判断后端返回的错误是不是"未配置 API key"。其它（网络超时/被墙/上游 5xx）
// 都归为"网络/上游故障"，避免误导用户去再配一次 key。
function isMissingKey(err?: string): boolean {
  if (!err) return false
  const low = err.toLowerCase()
  return low.includes('api key') || low.includes('apikey') || low.includes('not configured')
}

function isNetworkError(err?: string): boolean {
  if (!err) return false
  const low = err.toLowerCase()
  return (
    low.includes('deadline exceeded') ||
    low.includes('timeout') ||
    low.includes('no such host') ||
    low.includes('connection refused') ||
    low.includes('eof') ||
    low.includes('tls') ||
    low.includes('reset')
  )
}

export function DiscoverPage() {
  const [trending, setTrending] = useState<DiscoverItem[]>([])
  const [popular, setPopular] = useState<DiscoverItem[]>([])
  const [trendingErr, setTrendingErr] = useState<string | undefined>()
  const [popularErr, setPopularErr] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    Promise.all([
      discoverAPI.trending().catch((err) => ({
        items: [] as DiscoverItem[],
        error: err instanceof Error ? err.message : String(err),
      })),
      discoverAPI.popular().catch((err) => ({
        items: [] as DiscoverItem[],
        error: err instanceof Error ? err.message : String(err),
      })),
    ])
      .then(([t, p]) => {
        setTrending(t.items)
        setTrendingErr(t.error)
        setPopular(p.items)
        setPopularErr(p.error)
      })
      .finally(() => setLoading(false))
  }, [])

  const anyErr = trendingErr || popularErr
  const missingKey = isMissingKey(anyErr)
  const networkErr = !missingKey && isNetworkError(anyErr)
  const otherErr = !missingKey && !networkErr && anyErr

  return (
    <div className="space-y-8 px-4 py-6 max-w-7xl mx-auto">
      {/* Header */}
      <header className="flex items-center gap-4 mb-8">
        <div className="p-3 rounded-2xl bg-gradient-to-br from-primary-500/20 to-primary-600/10 border border-primary-500/20">
          <Sparkles className="h-8 w-8 text-brand-500" />
        </div>
        <div>
          <h1 className="font-display text-4xl font-bold text-ink-600 tracking-tight">发现</h1>
          <p className="mt-1 text-base text-ink-50">
            来自 TMDb 的当日热门与流行榜单
          </p>
        </div>
      </header>

      {loading && <DiscoverSkeleton />}

      {/* TMDb API Key 未配置 */}
      {!loading && missingKey && (
        <div className="rounded-2xl bg-amber-500/10 border border-amber-500/20 p-6 text-center space-y-4">
          <div className="mx-auto w-16 h-16 rounded-full bg-amber-500/10 flex items-center justify-center">
            <AlertTriangle className="h-8 w-8 text-amber-400" />
          </div>
          <h3 className="text-lg font-semibold text-ink-600">TMDb API Key 未配置</h3>
          <p className="text-sm text-ink-50 max-w-md mx-auto">
            您需要在管理后台填入 TMDb API Key 才能查看发现内容。
          </p>
          <Link
            to="/admin"
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl bg-primary-500/20 text-brand-500 hover:bg-primary-500/30 transition-colors font-medium"
          >
            前往管理后台
            <ExternalLink className="h-4 w-4" />
          </Link>
        </div>
      )}

      {/* 网络无法访问 TMDb */}
      {!loading && networkErr && (
        <div className="rounded-2xl bg-orange-500/10 border border-orange-500/20 p-6 text-center space-y-4">
          <div className="mx-auto w-16 h-16 rounded-full bg-orange-500/10 flex items-center justify-center">
            <Wifi className="h-8 w-8 text-orange-400" />
          </div>
          <h3 className="text-lg font-semibold text-ink-600">无法连接到 TMDb</h3>
          <p className="text-sm text-ink-50 max-w-lg mx-auto">
            服务器到 <code className="font-mono text-orange-300">api.themoviedb.org</code> 的连接超时。
            通常是因为部署机器没有走代理。可以在系统环境变量里设置
            <code className="font-mono text-orange-300 mx-1">HTTPS_PROXY</code>，
            或在「外部 API」配置里填写自建反代地址（tmdb_api_proxy / tmdb_image_proxy）。
          </p>
          <details className="text-xs text-sand-500 max-w-lg mx-auto text-left">
            <summary className="cursor-pointer hover:text-ink-50">查看原始错误</summary>
            <pre className="mt-2 p-2 rounded bg-black/40 overflow-x-auto whitespace-pre-wrap">{anyErr}</pre>
          </details>
        </div>
      )}

      {/* 其它错误 */}
      {!loading && otherErr && (
        <div className="rounded-2xl bg-red-500/10 border border-red-500/20 p-4 flex items-center gap-3">
          <AlertTriangle className="h-5 w-5 text-red-400 flex-shrink-0" />
          <p className="text-red-300">{otherErr}</p>
        </div>
      )}

      {/* Content Rows */}
      {!loading && !missingKey && (
        <div className="space-y-10">
          {trending.length > 0 && <ContentRow title="今日趋势" items={trending} />}
          {popular.length > 0 && <ContentRow title="热门电影" items={popular} />}

          {/* TMDB 配置 OK 但本次没拿到任何条目（极少见） */}
          {!networkErr && trending.length === 0 && popular.length === 0 && !otherErr && (
            <div className="text-center py-12">
              <p className="text-sand-500">暂无发现内容</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function ContentRow({ title, items }: { title: string; items: DiscoverItem[] }) {
  return (
    <section className="space-y-4">
      <h2 className="font-display text-2xl font-semibold text-ink-600 pl-1">{title}</h2>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
        {items.map((item) => (
          <DiscoverCard key={item.tmdb_id} item={item} />
        ))}
      </div>
    </section>
  )
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
  return (
    <div className="group relative overflow-hidden rounded-xl border border-white/5 bg-surface-800/60 hover:border-primary-500/30 transition-all duration-300">
      <div className="aspect-[2/3] w-full bg-surface-900 relative overflow-hidden">
        {item.poster_url ? (
          <img
            src={imageURL(item.poster_url)}
            alt={item.title}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover group-hover:scale-105 transition-transform duration-500"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-sand-400 text-xs">
            无海报
          </div>
        )}
        {item.rating > 0 && (
          <div className="absolute top-1.5 right-1.5 rounded-md bg-black/70 backdrop-blur-sm px-1.5 py-0.5 text-[11px] font-semibold text-yellow-400 border border-yellow-400/30">
            ★ {item.rating.toFixed(1)}
          </div>
        )}
      </div>
      <div className="px-2.5 py-2 space-y-0.5">
        <p className="text-xs font-medium text-ink-600 truncate group-hover:text-brand-500 transition-colors">
          {item.title}
        </p>
        {item.year > 0 && (
          <p className="text-[11px] text-sand-500">{item.year}</p>
        )}
      </div>
    </div>
  )
}

function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[1, 2].map((section) => (
        <section key={section} className="space-y-4">
          <div className="h-8 w-48 rounded-lg bg-surface-800 animate-pulse" />
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
            {[1, 2, 3, 4, 5, 6, 7, 8].map((i) => (
              <div key={i} className="aspect-[2/3] rounded-xl bg-surface-800 animate-pulse" />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
