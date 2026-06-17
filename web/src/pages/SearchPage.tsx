import { ChangeEvent, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { CheckCircle2, Info, Rss, Sparkles } from 'lucide-react'

import { aiAPI, type ExternalMediaResult, type SearchIntent } from '../api/ai'
import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import { subscriptionsAPI } from '../api/subscriptions'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'
import { groupSeries, seriesCardLink } from '../utils/groupSeries'

const LOCAL_SEARCH_PAGE_SIZE = 2000

export function SearchPage() {
  const [searchParams] = useSearchParams()
  const urlQuery = searchParams.get('q') ?? ''
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
  const [searchTotal, setSearchTotal] = useState(0)
  const searchSeq = useRef(0)
  const localCards = useMemo(() => groupSeries(items), [items])

  useEffect(() => {
    aiAPI
      .status()
      .then((s) => setAiAvailable(s.enabled))
      .catch(() => setAiAvailable(false))
  }, [])

  useEffect(() => {
    setQ(urlQuery)
  }, [urlQuery])

  const doQuickSearch = useCallback((query: string) => {
    const seq = ++searchSeq.current
    if (!query.trim()) {
      setItems([])
      setSearchTotal(0)
      setHasSearched(false)
      setLoading(false)
      return
    }
    setHasSearched(true)
    setError('')
    const loadAll = async () => {
      let page = 1
      let collected: Media[] = []
      for (;;) {
        const d = await mediaAPI.searchPage(query, page, LOCAL_SEARCH_PAGE_SIZE)
        if (seq !== searchSeq.current) return
        const pageItems = d.items ?? []
        collected = collected.concat(pageItems)
        const total = d.total ?? collected.length
        setItems(collected)
        setSearchTotal(total)
        if (collected.length >= total || pageItems.length < LOCAL_SEARCH_PAGE_SIZE) break
        page += 1
      }
      setExternalItems([])
      setIntent(null)
    }
    loadAll()
      .catch((err) => {
        if (seq !== searchSeq.current) return
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '搜索失败'
        setError(msg)
        toast.error(msg)
      })
      .finally(() => {
        if (seq === searchSeq.current) setLoading(false)
      })
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
    ++searchSeq.current
    setLoading(true)
    setError('')
    setHasSearched(true)
    try {
      const data = await aiAPI.smartSearch(q)
      setItems(data.items ?? [])
      setSearchTotal((data.items ?? []).length)
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

  const showEmpty = !loading && !error && hasSearched && localCards.length === 0
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

      {localCards.length > 0 && (
        <>
          <div className="text-sm font-semibold text-ink-100">
            本地媒体库 · {localCards.length} 个合集 / {items.length} 个条目
            {loading && searchTotal > items.length ? ` · 正在加载全部结果 ${items.length}/${searchTotal}` : ''}
          </div>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {localCards.map((card) => (
            <MediaCard
              key={card.key}
              media={card.rep}
              count={card.count}
              linkTo={seriesCardLink(card)}
            />
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
                total_episodes: item.total_episodes,
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
  const [detail, setDetail] = useState<ExternalMediaResult | null>(null)
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
            <article
              key={key}
              role="button"
              tabIndex={0}
              onClick={() => setDetail(item)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') setDetail(item)
              }}
              className="glass-panel flex cursor-pointer gap-3 !p-3 transition hover:-translate-y-0.5 hover:shadow-lg"
            >
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
                <div className="mt-2 flex flex-wrap gap-1.5 text-[10px]">
                  <span className={'rounded-full px-2 py-0.5 font-semibold ' + (item.in_library ? 'bg-emerald-50 text-emerald-600' : 'bg-amber-50 text-amber-600')}>
                    {item.in_library ? '本地已入库' : '本地未入库'}
                  </span>
                  {isSeriesItem(item) ? (
                    <span className="rounded-full bg-gray-100 px-2 py-0.5 text-ink-100">
                      已有 {item.downloaded_episodes || 0}/{item.total_episodes || '未知'} 集
                    </span>
                  ) : null}
                </div>
                <button
                  onClick={(event) => {
                    event.stopPropagation()
                    onSubscribe(item)
                  }}
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
      {detail && (
        <ExternalDetailModal
          item={detail}
          busy={busyKey === `${detail.source}:${detail.subscribe_keyword || detail.title}`}
          onClose={() => setDetail(null)}
          onSubscribe={onSubscribe}
        />
      )}
    </section>
  )
}

function ExternalDetailModal({
  item,
  busy,
  onClose,
  onSubscribe,
}: {
  item: ExternalMediaResult
  busy: boolean
  onClose: () => void
  onSubscribe: (item: ExternalMediaResult) => Promise<void>
}) {
  const missing = item.missing_episodes ?? []
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-4 backdrop-blur-sm" onClick={onClose}>
      <div className="max-h-[88vh] w-full max-w-3xl overflow-hidden rounded-3xl bg-white shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <div className="grid gap-0 md:grid-cols-[220px,1fr]">
          <div className="min-h-72 bg-gray-100">
            {item.poster_url ? (
              <img src={imageURL(item.poster_url)} alt={item.title} className="h-full w-full object-cover" />
            ) : (
              <div className="flex h-full min-h-72 items-center justify-center text-brand-500">
                <Info size={42} />
              </div>
            )}
          </div>
          <div className="space-y-4 p-5">
            <div>
              <div className="mb-2 flex flex-wrap gap-2 text-xs">
                <span className="rounded-full bg-primary-400/10 px-2 py-0.5 font-semibold uppercase text-brand-500">{item.source}</span>
                {item.media_type ? <span className="rounded-full bg-gray-100 px-2 py-0.5 text-ink-100">{item.media_type}</span> : null}
                {item.year ? <span className="rounded-full bg-gray-100 px-2 py-0.5 text-ink-100">{item.year}</span> : null}
                {item.rating ? <span className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-600">★ {item.rating.toFixed(1)}</span> : null}
              </div>
              <h3 className="font-display text-2xl font-bold text-ink-600">{item.title}</h3>
              <p className="mt-2 text-sm leading-6 text-ink-50">{item.overview || '暂无简介。'}</p>
            </div>

            <div className="grid gap-3 sm:grid-cols-3">
              <StatusBox label="入库状态" value={item.in_library ? '已入库' : '未入库'} ok={item.in_library} />
              <StatusBox label="本地条目" value={`${item.local_media_count || 0} 个`} />
              <StatusBox label="剧集进度" value={isSeriesItem(item) ? `${item.downloaded_episodes || 0}/${item.total_episodes || '未知'} 集` : '单部影片'} />
            </div>

            {isSeriesItem(item) && (
              <div className="rounded-2xl border border-gray-100 bg-gray-50 p-3 text-sm">
                <div className="mb-2 font-semibold text-ink-600">缺失情况</div>
                {missing.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {missing.slice(0, 80).map((episode) => (
                      <span key={episode} className="rounded-full bg-white px-2 py-0.5 text-xs text-ink-100 shadow-sm">第 {episode} 集</span>
                    ))}
                    {missing.length > 80 ? <span className="text-xs text-sand-500">还有 {missing.length - 80} 集…</span> : null}
                  </div>
                ) : item.in_library && item.total_episodes ? (
                  <p className="flex items-center gap-1.5 text-emerald-600"><CheckCircle2 size={15} /> 已完整入库</p>
                ) : (
                  <p className="text-sand-500">总集数未知，订阅时会跳过本地已有单集，优先补新集。</p>
                )}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-2">
              <button onClick={onClose} className="rounded-xl border border-gray-200 px-4 py-2 text-sm text-ink-100 hover:bg-gray-50">关闭</button>
              <button
                disabled={busy}
                onClick={async () => {
                  await onSubscribe(item)
                  onClose()
                }}
                className="neon-button"
              >
                <Rss size={14} /> {busy ? '订阅中…' : item.in_library ? '补全缺失集' : '订阅全集'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function StatusBox({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <div className="rounded-2xl border border-gray-100 bg-gray-50 p-3">
      <div className="text-xs text-sand-500">{label}</div>
      <div className={'mt-1 font-semibold ' + (ok ? 'text-emerald-600' : 'text-ink-600')}>{value}</div>
    </div>
  )
}

function isSeriesItem(item: ExternalMediaResult) {
  return ['tv', 'anime', 'variety'].includes((item.media_type || '').toLowerCase())
}
