import { ChangeEvent, FormEvent, useCallback, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Rss, Sparkles } from 'lucide-react'

import { aiAPI, type ExternalMediaResult, type SearchIntent } from '../api/ai'
import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import { subscriptionsAPI } from '../api/subscriptions'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

export function SearchPage() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [aiOn, setAiOn] = useState(false)
  const [aiAvailable, setAiAvailable] = useState(false)
  const [intent, setIntent] = useState<SearchIntent | null>(null)
  const [hasSearched, setHasSearched] = useState(false)
  const [externalItems, setExternalItems] = useState<ExternalMediaResult[]>([])
  const [subscribing, setSubscribing] = useState('')

  useEffect(() => {
    aiAPI
      .status()
      .then((s) => setAiAvailable(s.enabled))
      .catch(() => setAiAvailable(false))
  }, [])

  const doQuickSearch = useCallback((query: string) => {
    if (!query.trim()) {
      setItems([])
      setHasSearched(false)
      setLoading(false)
      return
    }
    setHasSearched(true)
    setError('')
    mediaAPI
      .search(query, 60)
      .then((d) => {
        setItems(d.items ?? [])
        setExternalItems([])
        setIntent(null)
      })
      .catch((err) => {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '搜索失败'
        setError(msg)
        toast.error(msg)
      })
      .finally(() => setLoading(false))
  }, [])

  // Fast LIKE search-as-you-type when AI mode is OFF.
  useEffect(() => {
    if (aiOn) return
    setLoading(true)
    const t = setTimeout(() => doQuickSearch(q), 300)
    return () => clearTimeout(t)
  }, [q, aiOn, doQuickSearch])

  const onAISubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!q.trim()) return
    setLoading(true)
    setError('')
    setHasSearched(true)
    try {
      const data = await aiAPI.smartSearch(q)
      setItems(data.items ?? [])
      setExternalItems(data.external_items ?? [])
      setIntent(data.intent)
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        'AI 搜索失败'
      setError(msg)
      toast.error(msg)
    } finally {
      setLoading(false)
    }
  }

  const showEmpty = !loading && !error && hasSearched && items.length === 0
  const showIdle = !loading && !error && !hasSearched

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="font-display text-3xl font-bold text-ink-600">搜索</h1>
        <button
          className={
            'neon-button !px-3 !py-1 !text-xs ' +
            (aiOn ? '!border-accent-400 !bg-accent-400/20 !text-accent-400' : '')
          }
          onClick={() => setAiOn((on) => !on)}
          title={aiAvailable ? '启用 AI 智能搜索' : '使用本地规则 + 外部数据源搜索'}
        >
          <Sparkles size={12} /> {aiOn ? '智能搜索已开启' : '智能搜索'}
        </button>
      </header>

      {aiOn ? (
        <form onSubmit={onAISubmit} className="flex flex-wrap gap-2">
          <input
            autoFocus
            className="input-base"
            placeholder='例如:"2010 年后的科幻电影" / "最近的动漫"'
            value={q}
            onChange={(e: ChangeEvent<HTMLInputElement>) => setQ(e.target.value)}
          />
          <button type="submit" className="neon-button">
            搜索
          </button>
        </form>
      ) : (
        <input
          autoFocus
          className="input-base"
          placeholder="按标题搜索…"
          value={q}
          onChange={(e: ChangeEvent<HTMLInputElement>) => setQ(e.target.value)}
        />
      )}

      {intent && (
        <div className="glass-panel !p-3 text-xs text-ink-100">
          AI 解析:
          <span className="ml-2 font-mono text-brand-500">{JSON.stringify(intent)}</span>
        </div>
      )}

      {loading && (
        <div className="flex items-center gap-2 py-8 text-ink-50">
          <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-primary-400 border-t-transparent" />
          搜索中…
        </div>
      )}

      {error && (
        <div className="glass-panel !border-red-400/30 p-4 text-sm text-red-400">{error}</div>
      )}

      {showIdle && (
        <div className="glass-panel flex flex-col items-center gap-2 p-10 text-center">
          <p className="text-lg text-ink-100">输入关键词开始搜索</p>
          <p className="text-sm text-sand-500">
            支持电影、电视剧、动漫等媒体内容的快速搜索
          </p>
        </div>
      )}

      {showEmpty && (
        <div className="glass-panel flex flex-col items-center gap-2 p-10 text-center">
          <p className="text-lg text-ink-100">未找到匹配的媒体</p>
          <p className="text-sm text-sand-500">尝试其他关键词，或者添加媒体库后执行扫描</p>
        </div>
      )}

      {items.length > 0 && (
        <>
          <div className="text-sm font-semibold text-ink-100">本地媒体库</div>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </div>
        </>
      )}

      {externalItems.length > 0 && (
        <ExternalResults
          items={externalItems}
          busyKey={subscribing}
          onSubscribe={async (item) => {
            const keyword = item.subscribe_keyword || item.title
            const key = `${item.source}:${keyword}`
            setSubscribing(key)
            try {
              const feed = `site-search://search?keyword=${encodeURIComponent(keyword)}&source=${encodeURIComponent(item.source)}`
              const sub = await subscriptionsAPI.create({
                name: `${item.title} 自动订阅`,
                feed_url: feed,
                filter: keyword,
                media_type: item.media_type,
                source: item.source,
                poster_url: item.poster_url,
                backdrop_url: item.backdrop_url,
                overview: item.overview,
                enabled: true,
              })
              const run = await subscriptionsAPI.runNow(sub.id)
              toast.success(
                run.queued > 0
                  ? `已订阅并加入 ${run.queued} 个下载`
                  : '已订阅，暂未在 PT 站点找到可下载资源',
              )
            } catch (err) {
              const msg =
                (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
                '订阅失败'
              toast.error(msg)
            } finally {
              setSubscribing('')
            }
          }}
        />
      )}
    </div>
  )
}

function ExternalResults({
  items,
  busyKey,
  onSubscribe,
}: {
  items: ExternalMediaResult[]
  busyKey: string
  onSubscribe: (item: ExternalMediaResult) => Promise<void>
}) {
  return (
    <section className="space-y-3">
      <div>
        <h2 className="font-display text-xl font-semibold text-ink-600">外部数据源</h2>
        <p className="text-xs text-ink-50">
          来自 TMDb / 豆瓣 / Bangumi。电影入队最佳资源；剧集/动漫优先整季或全集包，否则按集批量入队。
        </p>
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {items.map((item) => {
          const keyword = item.subscribe_keyword || item.title
          const key = `${item.source}:${keyword}`
          return (
            <article key={key} className="glass-panel flex gap-3 !p-3">
              <div className="h-28 w-20 shrink-0 overflow-hidden rounded-xl bg-gray-100">
                {item.poster_url ? (
                  <img
                    src={imageURL(item.poster_url)}
                    alt={item.title}
                    className="h-full w-full object-cover"
                  />
                ) : null}
              </div>
              <div className="min-w-0 flex-1">
                <div className="mb-1 flex flex-wrap items-center gap-2">
                  <span className="rounded-full bg-primary-400/10 px-2 py-0.5 text-[10px] uppercase text-brand-500">
                    {item.source}
                  </span>
                  {item.media_type && <span className="text-xs text-sand-500">{item.media_type}</span>}
                  {item.year ? <span className="text-xs text-sand-500">{item.year}</span> : null}
                  {item.rating ? <span className="text-xs text-amber-500">★ {item.rating.toFixed(1)}</span> : null}
                </div>
                <h3 className="truncate font-semibold text-ink-600">{item.title}</h3>
                <p className="mt-1 line-clamp-2 text-xs text-ink-50">
                  {item.overview || `订阅关键词：${keyword}`}
                </p>
                <button
                  onClick={() => onSubscribe(item)}
                  disabled={busyKey === key}
                  className="mt-3 rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10 disabled:opacity-50"
                >
                  <Rss size={12} className="mr-1 inline" />
                  {busyKey === key ? '订阅中…' : '订阅并搜索 PT'}
                </button>
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}
