import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import {
  ArrowRight, Play, Clock, Film, Sparkles,
  Library as LibraryIcon, Tv, Music, PlayCircle, ChevronRight
} from 'lucide-react'
import { libraryAPI, mediaAPI } from '../api/library'
import { playbackAPI, type HistoryItem } from '../api/playback'
import { imageURL } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import type { Library, Media } from '../types'
import { groupSeries } from '../utils/groupSeries'

type LibraryRow = { library: Library; cards: ReturnType<typeof groupSeries> }

const TYPE_ICONS: Record<string, React.ReactNode> = {
  movie: <Film size={18} />, tv: <Tv size={18} />, anime: <PlayCircle size={18} />, music: <Music size={18} />,
}
const TYPE_LABELS: Record<string, string> = { movie: '电影', tv: '电视剧', anime: '动漫', music: '音乐' }

export function HomePage() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [rows, setRows] = useState<LibraryRow[]>([])
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      try {
        const [libs, hist] = await Promise.all([
          libraryAPI.list().catch(() => [] as Library[]),
          playbackAPI.recentHistory().catch(() => [] as HistoryItem[]),
        ])
        if (cancelled) return
        setLibraries(libs)
        setHistory(hist.filter((h) => !h.completed && !!h.media))
        const perLib = await Promise.all(libs.map(async (lib) => {
          try {
            const page = await libraryAPI.listMedia(lib.id, 1, 60)
            return { library: lib, cards: groupSeries(page.items).slice(0, 14) } as LibraryRow
          } catch { return { library: lib, cards: [] } as LibraryRow }
        }))
        if (cancelled) return
        setRows(perLib)
      } finally { if (!cancelled) setLoading(false) }
    }
    load()
    return () => { cancelled = true }
  }, [])

  const [fallback, setFallback] = useState<Media[]>([])
  useEffect(() => {
    if (loading || libraries.length > 0) return
    mediaAPI.search('', 60).then((d) => setFallback(d.items)).catch(() => undefined)
  }, [loading, libraries.length])
  const fallbackCards = useMemo(() => groupSeries(fallback).slice(0, 16), [fallback])

  // Pick a featured movie/show for the premium editorial billboard
  const featuredItem = useMemo(() => {
    if (history.length > 0 && history[0].media) {
      return history[0].media;
    }
    if (rows.length > 0 && rows[0].cards.length > 0) {
      return rows[0].cards[0].rep;
    }
    if (fallback.length > 0) {
      return fallback[0];
    }
    return null;
  }, [history, rows, fallback]);

  const empty = !loading && history.length === 0 && rows.every((r) => r.cards.length === 0) && fallbackCards.length === 0

  if (loading) {
    return (
      <div className="flex items-center justify-center py-48">
        <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 1.5 }} className="flex flex-col items-center gap-4">
          <div className="relative flex items-center justify-center">
            <div className="h-10 w-10 rounded-full border-2 border-gray-100 border-t-gray-900 animate-spin" />
            <Film className="absolute h-4 w-4 text-brand-500" />
          </div>
          <span className="text-sm font-semibold tracking-widest text-gray-400 uppercase">站点舱准备中…</span>
        </motion.div>
      </div>
    )
  }

  if (empty) {
    return (
      <div className="flex flex-col items-center justify-center py-32 text-center max-w-md mx-auto">
        <div className="mb-6 flex h-24 w-24 items-center justify-center rounded-3xl bg-gray-100 border border-gray-200/50 shadow-sm">
          <Film className="h-10 w-10 text-gray-400" />
        </div>
        <p className="text-xl font-bold text-gray-900">您的家庭影视站暂无内容</p>
        <p className="mt-2 text-sm text-gray-500 leading-relaxed">
          立即前往管理后台配置您的第一个媒体库，添加影视目录并触发后台刮削扫描任务。
        </p>
        <Link to="/admin" className="mt-8 btn-primary">
          前往管理后台
        </Link>
      </div>
    )
  }

  return (
    <div className="space-y-12">
      {/* ─── Premium Swiss-Editorial Billboard Hero ─── */}
      {featuredItem && (
        <section className="relative overflow-hidden rounded-3xl bg-white border border-gray-200/90 shadow-[0_1px_3px_rgba(0,0,0,0.01),0_1px_2px_rgba(0,0,0,0.015)]">
          {/* Background Poster Cover & Soft Faded Backdrop */}
          <div className="absolute inset-0 z-0">
            {featuredItem.poster_url ? (
              <img 
                src={imageURL(featuredItem.poster_url)} 
                alt="" 
                className="w-full h-full object-cover object-top opacity-5 scale-105 blur-sm"
              />
            ) : (
              <div className="w-full h-full bg-gray-50" />
            )}
            {/* Soft Bright Masking */}
            <div className="absolute inset-0 bg-gradient-to-r from-white via-white/95 to-transparent" />
            <div className="absolute inset-0 bg-gradient-to-t from-white via-transparent to-transparent" />
          </div>

          {/* Billboard Content */}
          <div className="relative z-10 px-8 py-14 sm:px-12 md:py-16 lg:py-20 max-w-3xl space-y-5">
            <div className="inline-flex items-center gap-2 rounded-full bg-brand-50 px-3.5 py-1.5 text-xs font-bold uppercase tracking-widest text-[#c9954a] border border-brand-100/40">
              <Sparkles size={12} fill="currentColor" />
              <span>本周力荐 / Featured</span>
            </div>

            <h1 className="font-display text-3xl sm:text-4xl md:text-5xl font-extrabold tracking-tight leading-tight text-gray-900">
              {featuredItem.title}
            </h1>

            {featuredItem.overview ? (
              <p className="text-gray-500 text-sm sm:text-base leading-relaxed line-clamp-3 font-semibold">
                {featuredItem.overview}
              </p>
            ) : (
              <p className="text-gray-400 text-sm italic">
                家庭私人媒体中心收藏。极高视听品质，支持多端原生无损解码及HLS转码播放。
              </p>
            )}

            {/* Metadata Badges */}
            <div className="flex flex-wrap items-center gap-3 text-xs text-gray-500 font-bold">
              {featuredItem.year > 0 && (
                <span className="bg-gray-100 px-2.5 py-1 rounded-md text-gray-900 border border-gray-200">{featuredItem.year} 年</span>
              )}
              {featuredItem.video_codec && (
                <span className="rounded bg-brand-50 px-2 py-1 text-brand-700 border border-brand-100 uppercase font-bold text-[10px]">
                  {featuredItem.video_codec}
                </span>
              )}
              {featuredItem.container && (
                <span className="rounded bg-gray-100 px-2 py-1 text-gray-700 border border-gray-200 uppercase font-mono text-[10px]">
                  {featuredItem.container}
                </span>
              )}
            </div>

            {/* Buttons */}
            <div className="flex flex-wrap items-center gap-4 pt-4">
              <Link to={`/media/${featuredItem.id}`} className="inline-flex items-center justify-center gap-2 rounded-xl bg-[#111827] px-6 py-3.5 text-sm font-bold text-white shadow-sm hover:bg-[#1f2937] hover:-translate-y-0.5 transition-all">
                <Play size={16} fill="currentColor" />
                <span>立即播放</span>
              </Link>
              <Link to="/discover" className="btn-outline px-5 py-3.5 text-sm font-bold text-gray-700 border border-gray-200 hover:border-gray-300">
                <span>发现更多精彩</span>
                <ArrowRight size={16} />
              </Link>
            </div>
          </div>
        </section>
      )}

      {/* ─── Continue Watching Section ─── */}
      {history.length > 0 && (
        <section className="space-y-5">
          <div className="flex items-center gap-2.5">
            <span className="p-1.5 rounded-lg bg-gray-100 border border-gray-200/50 text-gray-900">
              <Clock size={16} />
            </span>
            <h2 className="font-display text-xl font-extrabold tracking-tight text-gray-900">继续观看</h2>
            <span className="text-xs font-bold text-gray-400 bg-gray-100 px-2.5 py-0.5 rounded-full border border-gray-200/40">{history.length} 个记录</span>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4">
            {history.slice(0, 8).map((h) => {
              const m = h.media!
              const pct = h.duration_ms > 0 ? h.position_ms / h.duration_ms : 0
              return <ContinueCard key={h.id} media={m} progress={pct} />
            })}
          </div>
        </section>
      )}

      {/* ─── Media Libraries Grid Sections ─── */}
      <div className="space-y-12">
        {rows.filter((r) => r.cards.length > 0).map((row) => (
          <section key={row.library.id} className="space-y-5">
            <div className="flex items-center justify-between border-b border-gray-200/80 pb-3">
              <div className="flex items-center gap-2.5">
                <span className="p-1.5 rounded-lg bg-gray-100 text-gray-900 border border-gray-200/50">
                  {TYPE_ICONS[row.library.type] ?? <LibraryIcon size={18} />}
                </span>
                <h2 className="font-display text-xl font-extrabold tracking-tight text-gray-900">
                  {row.library.name}
                </h2>
                <span className="rounded-full bg-gray-100 border border-gray-200/80 px-2 py-0.5 text-[10px] font-bold text-gray-500 uppercase tracking-wider">
                  {TYPE_LABELS[row.library.type] ?? row.library.type}
                </span>
              </div>
              <Link to={`/library/${row.library.id}`} className="group inline-flex items-center gap-1 text-xs font-bold text-gray-600 hover:text-brand-600 transition-colors">
                <span>浏览全部</span>
                <ChevronRight size={14} className="transition-transform group-hover:translate-x-0.5" />
              </Link>
            </div>

            <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {row.cards.map((s) => (
                <MediaCard
                  key={s.rep.id}
                  media={s.rep}
                  count={s.count}
                  linkTo={s.count > 1 ? `/library/${row.library.id}` : undefined}
                />
              ))}
            </div>
          </section>
        ))}

        {/* ─── Fallback Recents Section ─── */}
        {libraries.length === 0 && fallbackCards.length > 0 && (
          <section className="space-y-5">
            <div className="flex items-center justify-between border-b border-gray-200/80 pb-3">
              <div className="flex items-center gap-2.5">
                <span className="p-1.5 rounded-lg bg-gray-100 text-gray-900 border border-gray-200/50">
                  <Clock size={18} />
                </span>
                <h2 className="font-display text-xl font-extrabold tracking-tight text-gray-900">最近添加</h2>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {fallbackCards.map((s) => (
                <MediaCard key={s.rep.id} media={s.rep} count={s.count} />
              ))}
            </div>
          </section>
        )}
      </div>
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
          <div className="flex h-full items-center justify-center text-gray-400 bg-gray-50">
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
          <p className="text-[10px] text-gray-400 font-bold tracking-wide uppercase">
            已观看到 {Math.round(progress * 100)}%
          </p>
        </div>
      </div>
    </Link>
  )
}