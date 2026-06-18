import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowRight, Clock, Film, Play, Sparkles } from 'lucide-react'

import { libraryAPI, mediaAPI } from '../api/library'
import { playbackAPI, type HistoryItem } from '../api/playback'
import { imageURL } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import type { Library, Media } from '../types'
import { groupSeries, seriesCardLink } from '../utils/groupSeries'

const hasArtwork = (media?: Media | null) => !!(media?.poster_url || media?.backdrop_url)
const asArray = <T,>(value: unknown): T[] => (Array.isArray(value) ? value as T[] : [])

export function HomePage() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [recent, setRecent] = useState<Media[]>([])
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      try {
        const [libs, recentItems, hist] = await Promise.all([
          libraryAPI.list().then((rows) => asArray<Library>(rows)).catch(() => [] as Library[]),
          mediaAPI.search('', 120).then((d) => asArray<Media>(d?.items)).catch(() => [] as Media[]),
          playbackAPI.recentHistory().then((rows) => asArray<HistoryItem>(rows)).catch(() => [] as HistoryItem[]),
        ])
        if (cancelled) return
        setLibraries(libs)
        setRecent(recentItems)
        setHistory(hist.filter((h) => h && !h.completed && !!h.media))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [])

  const recentCards = useMemo(() => groupSeries(recent).slice(0, 24), [recent])
  const featuredItem = useMemo(() => {
    const candidates = [
      ...(history.map((h) => h.media).filter(Boolean) as Media[]),
      ...recentCards.map((card) => card.rep),
      ...recent,
    ]
    return candidates.find(hasArtwork) ?? candidates[0] ?? null
  }, [history, recentCards, recent])
  const featuredVisual = featuredItem?.backdrop_url || featuredItem?.poster_url || ''
  const featuredPoster = featuredItem?.poster_url || featuredItem?.backdrop_url || ''
  const featuredMark = (featuredItem?.title || 'MS').trim().slice(0, 4).toUpperCase()
  const empty = !loading && libraries.length === 0 && recentCards.length === 0 && history.length === 0

  if (loading) {
    return (
      <div className="flex items-center justify-center py-48">
        <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 1.5 }} className="flex flex-col items-center gap-4">
          <div className="relative flex items-center justify-center">
            <div className="h-10 w-10 rounded-full border-2 border-gray-100 border-t-gray-900 animate-spin" />
            <Film className="absolute h-4 w-4 text-brand-500" />
          </div>
          <span className="text-sm font-semibold tracking-widest text-gray-500 uppercase">首页内容准备中…</span>
        </motion.div>
      </div>
    )
  }

  if (empty) {
    return (
      <div className="flex flex-col items-center justify-center py-32 text-center max-w-md mx-auto">
        <div className="mb-6 flex h-24 w-24 items-center justify-center rounded-3xl bg-gray-100 border border-gray-200/50 shadow-sm">
          <Film className="h-10 w-10 text-gray-500" />
        </div>
        <p className="text-xl font-bold text-gray-900">您的家庭影视站暂无内容</p>
        <p className="mt-2 text-sm text-gray-500 leading-relaxed">
          前往管理后台添加媒体目录，扫描后首页将展示本周力荐、继续观看和最近入库。
        </p>
        <Link to="/admin" className="mt-8 btn-primary">
          前往管理后台
        </Link>
      </div>
    )
  }

  return (
    <div className="space-y-12">
      {featuredItem && (
        <section className="relative overflow-hidden rounded-[2rem] bg-white border border-gray-200/90 shadow-[0_24px_80px_rgba(15,23,42,0.08)]">
          <div className="absolute inset-0 z-0">
            <div className="h-full w-full bg-[radial-gradient(circle_at_80%_20%,rgba(212,175,55,0.22),transparent_34%),linear-gradient(135deg,#fff7ed,#f8fafc_52%,#eef2ff)]" />
            {featuredVisual && (
              <img
                src={imageURL(featuredVisual)}
                alt=""
                className="absolute inset-0 h-full w-full scale-105 object-cover object-center opacity-[0.34] blur-[1px]"
                referrerPolicy="no-referrer"
                onError={(event) => { event.currentTarget.style.display = 'none' }}
              />
            )}
            <div className="absolute inset-0 bg-[linear-gradient(90deg,#ffffff_0%,rgba(255,255,255,0.96)_37%,rgba(255,255,255,0.62)_68%,rgba(255,255,255,0.2)_100%)]" />
            <div className="absolute inset-x-0 bottom-0 h-32 bg-gradient-to-t from-white to-transparent" />
          </div>

          <div className="relative z-10 grid gap-8 px-6 py-8 sm:px-8 md:grid-cols-[minmax(0,1fr)_280px] md:px-12 md:py-12 lg:grid-cols-[minmax(0,1fr)_340px] lg:px-14 lg:py-14">
            <div className="flex min-w-0 flex-col justify-center space-y-5">
              <div className="inline-flex w-fit items-center gap-2 rounded-full bg-white/82 px-3.5 py-1.5 text-xs font-bold uppercase tracking-widest text-[#a8732d] border border-[#ead6b6] shadow-sm backdrop-blur">
                <Sparkles size={12} fill="currentColor" />
                <span>本周力荐 / Featured</span>
              </div>

              <div className="space-y-3">
                <div className="inline-flex max-w-full items-center gap-2 rounded-2xl bg-gray-950 px-3 py-2 text-white shadow-lg shadow-gray-950/10">
                  <span className="h-2 w-2 rounded-full bg-[#d4af37]" />
                  <span className="truncate text-xs font-black tracking-[0.26em]">{featuredMark}</span>
                </div>
                <h1 className="font-display text-3xl sm:text-4xl md:text-5xl font-extrabold tracking-tight leading-tight text-gray-950">
                  {featuredItem.title}
                </h1>
              </div>

              <p className="max-w-2xl text-gray-600 text-sm sm:text-base leading-relaxed line-clamp-3 font-semibold">
                {featuredItem.overview || '家庭私人媒体中心收藏。支持多端播放、外部播放器、智能刮削与订阅下载。'}
              </p>

              <div className="flex flex-wrap items-center gap-3 text-xs text-gray-500 font-bold">
                {featuredItem.year > 0 && (
                  <span className="bg-white/85 px-2.5 py-1 rounded-xl text-gray-900 border border-gray-200 shadow-sm">{featuredItem.year} 年</span>
                )}
                {featuredItem.video_codec && (
                  <span className="rounded-lg bg-[#fff8e7] px-2 py-1 text-[#9a6a1e] border border-[#ead6b6] uppercase font-bold text-[10px]">
                    {featuredItem.video_codec}
                  </span>
                )}
                {featuredItem.container && (
                  <span className="rounded-lg bg-white/80 px-2 py-1 text-gray-700 border border-gray-200 uppercase font-mono text-[10px]">
                    {featuredItem.container}
                  </span>
                )}
              </div>

              <div className="flex flex-wrap items-center gap-4 pt-2">
                <Link to={`/media/${featuredItem.id}`} className="inline-flex items-center justify-center gap-2 rounded-xl bg-[#111827] px-6 py-3.5 text-sm font-bold text-white shadow-lg shadow-gray-900/15 hover:bg-[#1f2937] hover:-translate-y-0.5 transition-all">
                  <Play size={16} fill="currentColor" />
                  <span>立即播放</span>
                </Link>
                <Link to="/discover" className="btn-outline bg-white/80 px-5 py-3.5 text-sm font-bold text-gray-700 border border-gray-200 hover:border-gray-300">
                  <span>发现更多精彩</span>
                  <ArrowRight size={16} />
                </Link>
              </div>
            </div>

            <div className="relative order-first mx-auto flex w-full max-w-[220px] items-center md:order-none md:max-w-[260px] lg:max-w-[310px]">
              <div className="absolute -right-6 top-5 h-32 w-32 rounded-full bg-[#d4af37]/20 blur-3xl" />
              <div className="relative aspect-[2/3] w-full overflow-hidden rounded-[1.7rem] border border-white/70 bg-white p-2 shadow-[0_32px_80px_rgba(15,23,42,0.20)]">
                <div className="flex h-full w-full flex-col items-center justify-center rounded-[1.25rem] bg-[linear-gradient(135deg,#f9fafb,#fff7ed)] text-center">
                  <Film className="mb-4 h-12 w-12 text-[#c9954a]" />
                  <span className="px-6 font-display text-3xl font-black tracking-tight text-gray-950">{featuredItem.title}</span>
                </div>
                {featuredPoster && (
                  <img
                    src={imageURL(featuredPoster)}
                    alt={featuredItem.title}
                    className="absolute inset-2 h-[calc(100%-1rem)] w-[calc(100%-1rem)] rounded-[1.25rem] object-cover"
                    referrerPolicy="no-referrer"
                    onError={(event) => { event.currentTarget.style.display = 'none' }}
                  />
                )}
              </div>
            </div>
          </div>
        </section>
      )}

      {history.length > 0 && (
        <section className="space-y-5">
          <div className="flex items-center gap-2.5">
            <span className="p-1.5 rounded-xl bg-gray-100 border border-gray-200/50 text-gray-900">
              <Clock size={16} />
            </span>
            <h2 className="font-display text-xl font-extrabold tracking-tight text-gray-900">继续观看</h2>
            <span className="text-xs font-bold text-gray-500 bg-gray-100 px-2.5 py-0.5 rounded-full border border-gray-200/40">{history.length} 个记录</span>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4">
            {history.slice(0, 8).map((h) => {
              const media = h.media!
              const progress = h.duration_ms > 0 ? h.position_ms / h.duration_ms : 0
              return <ContinueCard key={h.id} media={media} progress={progress} />
            })}
          </div>
        </section>
      )}

      {recentCards.length > 0 && (
        <section className="space-y-5">
          <div className="flex items-center justify-between border-b border-gray-200/80 pb-3">
            <div className="flex items-center gap-2.5">
              <span className="p-1.5 rounded-xl bg-gray-100 text-gray-900 border border-gray-200/50">
                <Clock size={18} />
              </span>
              <div>
                <h2 className="font-display text-xl font-extrabold tracking-tight text-gray-900">最近入库</h2>
                <p className="text-xs text-gray-500">按整部电影、剧集、番剧和综艺合集展示新增内容。</p>
              </div>
            </div>
            <Link to="/poster-wall" className="group inline-flex items-center gap-1 text-xs font-bold text-gray-600 hover:text-brand-600 transition-colors">
              <span>海报墙</span>
              <ArrowRight size={14} className="transition-transform group-hover:translate-x-0.5" />
            </Link>
          </div>

          <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
            {recentCards.map((card) => (
              <MediaCard
                key={card.key}
                media={card.rep}
                count={card.count}
                linkTo={seriesCardLink(card)}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function ContinueCard({ media, progress }: { media: Media; progress: number }) {
  return (
    <Link to={`/media/${media.id}`} className="group flex items-center gap-4 rounded-2xl bg-white p-3.5 border border-gray-200/80 shadow-[0_1px_3px_rgba(0,0,0,0.01)] transition-all duration-300 hover:shadow-md hover:border-brand-500/30">
      <div className="relative h-18 w-12 shrink-0 overflow-hidden rounded-xl bg-gray-50 border border-gray-100">
        {media.poster_url ? (
          <img
            src={imageURL(media.poster_url)}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-gray-500 bg-gray-50">
            <Film size={16} />
          </div>
        )}
        <div className="absolute inset-0 flex items-center justify-center bg-black/30 opacity-0 transition-opacity group-hover:opacity-100">
          <Play size={14} fill="white" className="text-white" />
        </div>
      </div>
      <div className="min-w-0 flex-1 space-y-1.5">
        <p className="truncate text-sm font-bold text-gray-900 group-hover:text-brand-500 transition-colors">
          {media.title}
        </p>
        <div className="space-y-1">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-gray-100">
            <motion.div
              initial={{ width: 0 }}
              animate={{ width: `${Math.round(progress * 100)}%` }}
              transition={{ duration: 0.5, delay: 0.1 }}
              className="h-full rounded-full bg-gradient-to-r from-brand-400 to-brand-500"
            />
          </div>
          <p className="text-[10px] text-gray-500 font-bold tracking-wide uppercase">
            已观看到 {Math.round(progress * 100)}%
          </p>
        </div>
      </div>
    </Link>
  )
}
