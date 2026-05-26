import { useEffect, useMemo, useState } from 'react'
import { Link, useParams, useNavigate } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import toast from 'react-hot-toast'
import { ArrowLeft, Play, Film, Layers } from 'lucide-react'

import { libraryAPI } from '../api/library'
import type { Library, Media } from '../types'
import { MediaCard } from '../components/MediaCard'
import { imageURL } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { groupSeries, type SeriesCard } from '../utils/groupSeries'

export function LibraryPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const role = useAuthStore((s) => s.user?.role)

  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [scraping, setScraping] = useState(false)

  // 剧集模式：选中某个剧集后展开详情
  const [selectedSeries, setSelectedSeries] = useState<SeriesCard | null>(null)

  const isSeries = library?.type === 'tv' || library?.type === 'anime'

  // 折叠后的剧集卡片
  const seriesCards = useMemo(() => {
    if (!isSeries || items.length === 0) return []
    return groupSeries(items)
  }, [isSeries, items])

  // 选中的剧集：所有集按季分组
  const selectedEpisodes = useMemo(() => {
    if (!selectedSeries || items.length === 0) return []
    const title = selectedSeries.rep.title.toLowerCase()
    const eps = items.filter(
      (m) => (m.title ?? '').toLowerCase() === title || m.tmdb_id === selectedSeries.rep.tmdb_id,
    )
    // 按季分组，按集排序
    const seasons = new Map<number, Media[]>()
    for (const ep of eps) {
      const s = ep.season_num || 1
      if (!seasons.has(s)) seasons.set(s, [])
      seasons.get(s)!.push(ep)
    }
    for (const [, list] of seasons) {
      list.sort((a, b) => (a.episode_num || 0) - (b.episode_num || 0))
    }
    return Array.from(seasons.entries())
      .sort(([a], [b]) => a - b)
      .map(([season, episodes]) => ({ season, episodes }))
  }, [selectedSeries, items])

  useEffect(() => {
    if (!id) return
    libraryAPI.list().then((all) => {
      const lib = all.find((l) => l.id === id) ?? null
      setLibrary(lib)
    })
  }, [id])

  useEffect(() => {
    if (!id || !library) return
    setLoading(true)
    // tv/anime 拉取较多集数以支持前端分组
    const limit = library.type === 'tv' || library.type === 'anime' ? 1000 : 200
    libraryAPI
      .listMedia(id, 1, limit)
      .then((d) => {
        setItems(d.items)
        setTotal(d.total)
      })
      .finally(() => setLoading(false))
  }, [id, library])

  const handleScan = async () => {
    setScanning(true)
    try {
      const r = await libraryAPI.scan(id)
      toast.success(`扫描完成:新增 ${r.added} 项`)
      setLibrary((l) => (l ? { ...l } : l))
    } catch {
      toast.error('扫描失败')
    } finally { setScanning(false) }
  }

  const handleScrape = async () => {
    setScraping(true)
    try {
      await libraryAPI.scrape(id)
      toast.success('刮削已加入后台队列')
    } catch { toast.error('刮削失败') }
    finally { setScraping(false) }
  }

  const handleSeriesClick = (card: SeriesCard) => {
    setSelectedSeries(card)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 2 }} className="flex items-center gap-3">
          <div className="h-2 w-2 rounded-full bg-brand-500" />
          <span className="text-sm text-sand-500">加载中…</span>
        </motion.div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">
            {library?.name ?? '媒体库'}
            {!isSeries && <span className="text-sand-500"> ({total})</span>}
          </h1>
          {library && <p className="text-sm text-ink-50">{library.type} · {library.path}</p>}
        </div>
        {role === 'admin' && (
          <div className="flex gap-2">
            <button onClick={handleScan} disabled={scanning} className="btn-outline">{scanning ? '扫描中…' : '立即扫描'}</button>
            <button onClick={handleScrape} disabled={scraping} className="btn-outline">{scraping ? '刮削中…' : '刮削元数据'}</button>
          </div>
        )}
      </div>

      {/* 非剧集：直接展示海报网格 */}
      {!isSeries && items.length > 0 && (
        <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </div>
      )}

      {!isSeries && items.length === 0 && (
        <div className="flex flex-col items-center justify-center py-24 text-center">
          <Film className="h-12 w-12 text-sand-400 mb-4" />
          <p className="text-ink-50">该媒体库暂无内容，触发一次扫描后再来看看</p>
        </div>
      )}

      {/* 剧集模式：折叠卡片网格 */}
      {isSeries && seriesCards.length > 0 && !selectedSeries && (
        <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
          {seriesCards.map((s) => (
            <button key={s.rep.id} onClick={() => handleSeriesClick(s)} className="text-left">
              <MediaCard media={s.rep} count={s.count} />
            </button>
          ))}
        </div>
      )}

      {/* 剧集详情：季/集选择器 */}
      <AnimatePresence mode="wait">
        {selectedSeries && (
          <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            className="space-y-6"
          >
            {/* 返回按钮 + 标题 */}
            <div className="flex items-center gap-4">
              <button
                onClick={() => setSelectedSeries(null)}
                className="btn-ghost gap-2"
              >
                <ArrowLeft size={16} />
                返回列表
              </button>
              <h2 className="font-display text-2xl font-bold text-ink-600 truncate">
                {selectedSeries.rep.title}
              </h2>
              <span className="text-sm text-sand-500">共 {selectedSeries.count} 集</span>
            </div>

            {/* 海报 + 从第一集开始播放 */}
            <div className="flex flex-col sm:flex-row gap-6">
              <div className="w-40 shrink-0 overflow-hidden rounded-xl bg-sand-200 shadow-card">
                {selectedSeries.rep.poster_url ? (
                  <img src={imageURL(selectedSeries.rep.poster_url)} alt={selectedSeries.rep.title} className="w-full aspect-[2/3] object-cover" referrerPolicy="no-referrer" />
                ) : (
                  <div className="flex items-center justify-center aspect-[2/3] text-sand-400"><Film size={40} /></div>
                )}
              </div>
              <div className="flex-1 space-y-3">
                <p className="text-sm text-ink-50 leading-relaxed">
                  {selectedSeries.rep.overview || '暂无简介'}
                </p>

                {/* 从第一集开始 */}
                {(() => {
                  const firstEps = selectedEpisodes.flatMap((s) => s.episodes).filter((e) => e.episode_num > 0).sort((a, b) => (a.episode_num || 0) - (b.episode_num || 0))
                  const first = firstEps.length > 0 ? firstEps[0] : null
                  return first ? (
                    <Link to={`/play/${first.id}`} className="btn-primary inline-flex">
                      <Play size={16} fill="currentColor" />
                      从第一集开始播放
                    </Link>
                  ) : null
                })()}
              </div>
            </div>

            {/* 季 / 集列表 */}
            <div className="space-y-6">
              {selectedEpisodes.map(({ season, episodes }) => (
                <div key={season}>
                  <h3 className="mb-3 font-display text-lg font-semibold text-ink-600">
                    第 {season} 季 · {episodes.length} 集
                  </h3>
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
                    {episodes.map((ep) => (
                      <Link
                        key={ep.id}
                        to={`/play/${ep.id}`}
                        className="group flex items-center gap-3 rounded-xl border border-sand-200 bg-white p-3 shadow-card transition-all hover:border-brand-300 hover:shadow-card-hover"
                      >
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-brand-50 text-brand-600 font-semibold text-sm">
                          {ep.episode_num || '—'}
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-sm font-medium text-ink-600">
                            {ep.episode_num > 0 ? `第 ${ep.episode_num} 集` : ep.title}
                          </p>
                          <p className="text-xs text-sand-500">
                            {ep.duration_sec > 0
                              ? `${Math.floor(ep.duration_sec / 60)} 分钟`
                              : formatSize(ep.size_bytes)}
                          </p>
                        </div>
                        <Play size={14} className="shrink-0 text-sand-400 opacity-0 transition-opacity group-hover:opacity-100 group-hover:text-brand-500" />
                      </Link>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* 剧集为空 */}
      {isSeries && seriesCards.length === 0 && !loading && (
        <div className="flex flex-col items-center justify-center py-24 text-center">
          <Film className="h-12 w-12 text-sand-400 mb-4" />
          <p className="text-ink-50">该库尚未发现任何剧集，触发一次扫描后再来看看</p>
        </div>
      )}
    </div>
  )
}

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes, i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${units[i]}`
}
