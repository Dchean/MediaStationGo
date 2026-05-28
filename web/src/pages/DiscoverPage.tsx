import { useEffect, useMemo, useState } from 'react'
import toast from 'react-hot-toast'
import { AlertTriangle, Download, Info, Rss, Sparkles, X } from 'lucide-react'

import { discoverAPI, type DiscoverItem, type DiscoverSection } from '../api/discover'
import { imageURL } from '../api/client'
import { subscriptionsAPI } from '../api/subscriptions'

const defaultSections = [
  'tmdb_trending_day',
  'douban_hot_movie',
  'douban_hot_tv',
  'bangumi_calendar',
]

const storageKey = 'mediastation.discover.sections'

export function DiscoverPage() {
  const [sections, setSections] = useState<DiscoverSection[]>([])
  const [selected, setSelected] = useState<string[]>(defaultSections)
  const [rows, setRows] = useState<Record<string, DiscoverItem[]>>({})
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [activeItem, setActiveItem] = useState<DiscoverItem | null>(null)

  useEffect(() => {
    discoverAPI
      .sections()
      .then((items) => {
        setSections(items)
        const saved = readSavedSections(items)
        setSelected(saved.length > 0 ? saved : defaultSections)
      })
      .catch(() => {
        setSections(defaultSectionDefs)
        setSelected(defaultSections)
      })
  }, [])

  useEffect(() => {
    if (selected.length === 0) {
      setRows({})
      setLoading(false)
      return
    }
    setLoading(true)
    setError('')
    window.localStorage.setItem(storageKey, JSON.stringify(selected))
    discoverAPI
      .feed(selected)
      .then((feed) => {
        const next: Record<string, DiscoverItem[]> = {}
        for (const key of selected) {
          next[key] = feed[key] ?? []
        }
        setRows(next)
      })
      .catch((err) => {
        setRows({})
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => setLoading(false))
  }, [selected])

  const sectionMap = useMemo(
    () => new Map(sections.map((section) => [section.key, section])),
    [sections],
  )
  const hasContent = selected.some((key) => (rows[key] ?? []).length > 0)

  const toggleSection = (key: string) => {
    setSelected((current) => {
      if (current.includes(key)) {
        return current.filter((item) => item !== key)
      }
      return [...current, key]
    })
  }

  return (
    <div className="mx-auto max-w-7xl space-y-8 px-4 py-6">
      <header className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex items-center gap-4">
          <div className="rounded-2xl border border-primary-500/20 bg-gradient-to-br from-primary-500/20 to-primary-600/10 p-3">
            <Sparkles className="h-8 w-8 text-brand-500" />
          </div>
          <div>
            <h1 className="font-display text-4xl font-bold tracking-tight text-ink-600">
              发现
            </h1>
            <p className="mt-1 text-base text-ink-50">
              多源推荐：TMDb / 豆瓣 / Bangumi，可按需组合显示
            </p>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          {sections.map((section) => {
            const active = selected.includes(section.key)
            return (
              <button
                key={section.key}
                type="button"
                onClick={() => toggleSection(section.key)}
                className={
                  'rounded-full border px-3 py-1.5 text-xs font-semibold transition ' +
                  (active
                    ? 'border-primary-400 bg-primary-400/15 text-brand-500'
                    : 'border-gray-200 bg-white text-gray-500 hover:border-primary-300 hover:text-ink-600')
                }
              >
                {section.label}
              </button>
            )
          })}
        </div>
      </header>

      {loading && <DiscoverSkeleton />}

      {!loading && error && (
        <div className="flex items-center gap-3 rounded-2xl border border-red-500/20 bg-red-500/10 p-4">
          <AlertTriangle className="h-5 w-5 flex-shrink-0 text-red-400" />
          <p className="text-red-300">{error}</p>
        </div>
      )}

      {!loading && selected.length === 0 && (
        <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center text-sand-500">
          至少选择一个推荐源，小宇宙才会开始转动。
        </div>
      )}

      {!loading && !error && selected.length > 0 && (
        <div className="space-y-10">
          {selected.map((key) => {
            const items = rows[key] ?? []
            if (items.length === 0) return null
            return (
              <ContentRow
                key={key}
                title={sectionMap.get(key)?.label ?? key}
                items={items}
                onSelect={setActiveItem}
              />
            )
          })}

          {!hasContent && (
            <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center">
              <p className="text-sand-500">
                当前选择的推荐源暂未返回内容，可切换豆瓣 / Bangumi 或检查网络代理。
              </p>
            </div>
          )}
        </div>
      )}

      {activeItem && (
        <DiscoverDetailModal
          item={activeItem}
          onClose={() => setActiveItem(null)}
        />
      )}
    </div>
  )
}

function ContentRow({
  title,
  items,
  onSelect,
}: {
  title: string
  items: DiscoverItem[]
  onSelect: (item: DiscoverItem) => void
}) {
  return (
    <section className="space-y-4">
      <h2 className="pl-1 font-display text-2xl font-semibold text-ink-600">{title}</h2>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
        {items.map((item, index) => (
          <DiscoverCard key={discoverKey(item, index)} item={item} onSelect={onSelect} />
        ))}
      </div>
    </section>
  )
}

function DiscoverCard({ item, onSelect }: { item: DiscoverItem; onSelect: (item: DiscoverItem) => void }) {
  const source = item.source || (item.bangumi_id ? 'bangumi' : item.douban_id ? 'douban' : 'tmdb')
  return (
    <button
      type="button"
      onClick={() => onSelect(item)}
      className="group relative overflow-hidden rounded-xl border border-gray-200 bg-gray-50 text-left transition-all duration-300 hover:-translate-y-1 hover:border-primary-500/30 hover:shadow-xl focus:outline-none focus:ring-2 focus:ring-primary-400/40"
    >
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-surface-900">
        {item.poster_url ? (
          <img
            src={imageURL(item.poster_url)}
            alt={item.title}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-xs text-gray-500">
            无海报
          </div>
        )}
        <div className="absolute left-1.5 top-1.5 rounded-xl border border-white/20 bg-black/65 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-white backdrop-blur-sm">
          {source}
        </div>
        {(item.rating ?? 0) > 0 && (
          <div className="absolute right-1.5 top-1.5 rounded-xl border border-yellow-400/30 bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-yellow-400 backdrop-blur-sm">
            ★ {(item.rating ?? 0).toFixed(1)}
          </div>
        )}
      </div>
      <div className="space-y-0.5 px-2.5 py-2">
        <p className="truncate text-xs font-medium text-ink-600 transition-colors group-hover:text-brand-500">
          {item.title}
        </p>
        <p className="text-[11px] text-sand-500">
          {[item.media_type, item.year && item.year > 0 ? item.year : ''].filter(Boolean).join(' · ') || '推荐'}
        </p>
        <p className="flex items-center gap-1 pt-1 text-[10px] font-semibold text-brand-500">
          <Info size={10} />
          详情 / 订阅
        </p>
      </div>
    </button>
  )
}

function DiscoverDetailModal({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  const source = item.source || (item.bangumi_id ? 'bangumi' : item.douban_id ? 'douban' : 'tmdb')
  const keyword = item.subscribe_keyword || buildSubscribeKeyword(item)
  const [form, setForm] = useState({
    keyword,
    search_mode: 'keyword',
    imdb_id: '',
    media_type: item.media_type || '',
    resolution: 'best',
    quality: '',
    effects: '',
    release_groups: '',
    exclude_words: 'cam,ts,tc,枪版',
    wash_enabled: false,
    wash_priority: 'balanced',
    save_path: '',
    media_category: '',
    priority: 50,
    run_now: true,
  })
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    const finalKeyword = form.keyword.trim() || keyword
    const feed = `site-search://search?keyword=${encodeURIComponent(finalKeyword)}&source=${encodeURIComponent(source)}`
    setBusy(true)
    try {
      const sub = await subscriptionsAPI.create({
        name: `${item.title} 自动订阅`,
        feed_url: feed,
        filter: finalKeyword,
        media_type: form.media_type || undefined,
        media_category: form.media_category || undefined,
        save_path: form.save_path || undefined,
        search_mode: form.search_mode,
        imdb_id: form.imdb_id || undefined,
        source,
        poster_url: item.poster_url || undefined,
        backdrop_url: item.backdrop_url || undefined,
        overview: item.overview || undefined,
        resolution: form.resolution === 'best' ? 'best' : form.resolution,
        quality: form.quality || undefined,
        effects: form.effects || undefined,
        release_groups: form.release_groups || undefined,
        exclude_words: form.exclude_words || undefined,
        wash_enabled: form.wash_enabled,
        wash_priority: form.wash_priority,
        priority: form.priority,
        enabled: true,
      })
      if (form.run_now) {
        const run = await subscriptionsAPI.runNow(sub.id)
        toast.success(run.queued > 0 ? `已订阅并加入 ${run.queued} 个下载` : '已订阅，暂未命中可下载资源')
      } else {
        toast.success('已创建订阅')
      }
      onClose()
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '订阅失败'
      toast.error(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4 backdrop-blur-sm">
      <div className="max-h-[92vh] w-full max-w-5xl overflow-y-auto rounded-3xl border border-white/60 bg-white p-5 shadow-2xl">
        <div className="mb-4 flex items-start justify-between gap-3">
          <div>
            <p className="text-xs font-semibold uppercase tracking-widest text-brand-500">{source}</p>
            <h2 className="font-display text-2xl font-bold text-ink-600">{item.title}</h2>
            <p className="mt-1 text-sm text-sand-500">
              {[item.media_type, item.year && item.year > 0 ? item.year : '', item.rating ? `★ ${item.rating.toFixed(1)}` : '']
                .filter(Boolean)
                .join(' · ')}
            </p>
          </div>
          <button className="rounded-full border border-gray-200 p-2 text-ink-50 hover:bg-gray-50" onClick={onClose}>
            <X size={18} />
          </button>
        </div>

        <div className="grid gap-5 lg:grid-cols-[260px_1fr]">
          <div className="space-y-3">
            <div className="overflow-hidden rounded-2xl bg-gray-100">
              {item.poster_url ? (
                <img src={imageURL(item.poster_url)} alt={item.title} className="aspect-[2/3] w-full object-cover" />
              ) : (
                <div className="flex aspect-[2/3] items-center justify-center text-sand-500">无海报</div>
              )}
            </div>
            {item.backdrop_url && (
              <img src={imageURL(item.backdrop_url)} alt="" className="h-24 w-full rounded-2xl object-cover" />
            )}
          </div>

          <div className="space-y-5">
            <section className="rounded-2xl border border-gray-200 bg-gray-50 p-4">
              <h3 className="mb-2 font-semibold text-ink-600">简介</h3>
              <p className="text-sm leading-6 text-ink-100">{item.overview || '当前数据源没有返回简介。'}</p>
            </section>

            <section className="rounded-2xl border border-primary-400/20 bg-primary-400/5 p-4">
              <h3 className="mb-3 flex items-center gap-2 font-semibold text-ink-600">
                <Rss size={16} />
                订阅下载规则
              </h3>
              <div className="grid gap-3 md:grid-cols-3">
                <label className="text-xs text-sand-500 md:col-span-2">
                  搜索关键词
                  <input className="input-base mt-1" value={form.keyword} onChange={(e) => setForm((f) => ({ ...f, keyword: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  搜索方式
                  <select className="input-base mt-1" value={form.search_mode} onChange={(e) => setForm((f) => ({ ...f, search_mode: e.target.value }))}>
                    <option value="keyword">标题关键词</option>
                    <option value="imdb">IMDB ID</option>
                  </select>
                </label>
                <label className="text-xs text-sand-500">
                  IMDB ID
                  <input className="input-base mt-1" placeholder="tt1160419" value={form.imdb_id} onChange={(e) => setForm((f) => ({ ...f, imdb_id: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  类型
                  <select className="input-base mt-1" value={form.media_type} onChange={(e) => setForm((f) => ({ ...f, media_type: e.target.value }))}>
                    <option value="">自动识别</option>
                    <option value="movie">电影</option>
                    <option value="tv">电视剧</option>
                    <option value="anime">动漫</option>
                    <option value="variety">综艺</option>
                  </select>
                </label>
                <label className="text-xs text-sand-500">
                  分辨率
                  <select className="input-base mt-1" value={form.resolution} onChange={(e) => setForm((f) => ({ ...f, resolution: e.target.value }))}>
                    <option value="best">自动择优</option>
                    <option value="2160p">2160p / 4K</option>
                    <option value="1080p">1080p</option>
                    <option value="720p">720p</option>
                  </select>
                </label>
                <label className="text-xs text-sand-500">
                  质量
                  <select className="input-base mt-1" value={form.quality} onChange={(e) => setForm((f) => ({ ...f, quality: e.target.value }))}>
                    <option value="">不限</option>
                    <option value="remux">REMUX</option>
                    <option value="bluray">BluRay</option>
                    <option value="web-dl">WEB-DL</option>
                    <option value="hdtv">HDTV</option>
                  </select>
                </label>
                <label className="text-xs text-sand-500">
                  特效 / 音轨
                  <input className="input-base mt-1" placeholder="hdr,dolby-vision,atmos" value={form.effects} onChange={(e) => setForm((f) => ({ ...f, effects: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  洗版优先级
                  <select className="input-base mt-1 disabled:opacity-50" disabled={!form.wash_enabled} value={form.wash_priority} onChange={(e) => setForm((f) => ({ ...f, wash_priority: e.target.value }))}>
                    <option value="balanced">均衡</option>
                    <option value="resolution">分辨率优先</option>
                    <option value="quality">片源质量优先</option>
                    <option value="effects">HDR/DV/Atmos 优先</option>
                    <option value="seeders">做种数优先</option>
                  </select>
                </label>
                <label className="flex items-center gap-2 rounded-xl border border-gray-200 bg-white px-3 py-2 text-xs text-ink-100">
                  <input type="checkbox" checked={form.wash_enabled} onChange={(e) => setForm((f) => ({ ...f, wash_enabled: e.target.checked }))} />
                  启用洗版择优
                </label>
                <label className="text-xs text-sand-500">
                  发布组
                  <input className="input-base mt-1" placeholder="如 FRDS,OurTV" value={form.release_groups} onChange={(e) => setForm((f) => ({ ...f, release_groups: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  排除词
                  <input className="input-base mt-1" value={form.exclude_words} onChange={(e) => setForm((f) => ({ ...f, exclude_words: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  分类覆盖
                  <input className="input-base mt-1" placeholder="综艺 / 日番 / 欧美剧" value={form.media_category} onChange={(e) => setForm((f) => ({ ...f, media_category: e.target.value }))} />
                </label>
                <label className="text-xs text-sand-500">
                  保存路径覆盖
                  <input className="input-base mt-1" value={form.save_path} onChange={(e) => setForm((f) => ({ ...f, save_path: e.target.value }))} />
                </label>
              </div>
              <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                <label className="flex items-center gap-2 text-sm text-ink-100">
                  <input type="checkbox" checked={form.run_now} onChange={(e) => setForm((f) => ({ ...f, run_now: e.target.checked }))} />
                  创建后立即搜索并下载
                </label>
                <button disabled={busy} onClick={submit} className="neon-button disabled:opacity-60">
                  <Download size={16} />
                  {busy ? '处理中…' : '创建订阅'}
                </button>
              </div>
            </section>
          </div>
        </div>
      </div>
    </div>
  )
}

function buildSubscribeKeyword(item: DiscoverItem): string {
  return [item.title, item.year && item.year > 0 ? item.year : ''].filter(Boolean).join(' ')
}

function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[1, 2, 3].map((section) => (
        <section key={section} className="space-y-4">
          <div className="h-8 w-48 animate-pulse rounded-xl bg-gray-100" />
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
            {[1, 2, 3, 4, 5, 6, 7, 8].map((item) => (
              <div key={item} className="aspect-[2/3] animate-pulse rounded-xl bg-gray-100" />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function discoverKey(item: DiscoverItem, index: number): string {
  return `${item.source || 'source'}:${item.tmdb_id || item.douban_id || item.bangumi_id || item.title}:${index}`
}

function readSavedSections(sections: DiscoverSection[]): string[] {
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const allowed = new Set(sections.map((section) => section.key))
    return parsed.filter((key) => typeof key === 'string' && allowed.has(key))
  } catch {
    return []
  }
}

const defaultSectionDefs: DiscoverSection[] = [
  { key: 'tmdb_trending_day', label: 'TMDb 今日趋势', provider: 'tmdb' },
  { key: 'tmdb_popular_movie', label: 'TMDb 热门电影', provider: 'tmdb' },
  { key: 'douban_hot_movie', label: '豆瓣热门电影', provider: 'douban' },
  { key: 'douban_hot_tv', label: '豆瓣热门剧集', provider: 'douban' },
  { key: 'bangumi_calendar', label: 'Bangumi 每日放送', provider: 'bangumi' },
]
