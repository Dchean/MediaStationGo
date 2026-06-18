import { motion } from 'framer-motion'
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { FileText, Heart, Play, RefreshCw, Sparkles, Trash2, Calendar, Database, Search, Pencil } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { playbackAPI } from '../api/playback'
import { recycleAPI } from '../api/recycle'
import { imageURL } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { api } from '../api/client'
import type { Media } from '../types'
import { confirmAction } from '../components/ConfirmDialog'
import { ExternalPlayerButton } from '../components/ExternalPlayerButton'
import { ManualScrapeDialog } from '../components/ManualScrapeDialog'
import { MetadataEditDialog } from '../components/MetadataEditDialog'

function fmtDuration(sec: number): string {
  if (!sec || sec <= 0) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

function fmtSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(2)} ${units[i]}`
}

function parseCSV(s?: string): string[] {
  if (!s) return []
  return s.split(',').map(x => x.trim()).filter(Boolean)
}

export function MediaDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const role = useAuthStore((s) => s.user?.role)
  const [media, setMedia] = useState<Media | null>(null)
  const [favourite, setFavourite] = useState(false)
  const [loading, setLoading] = useState(true)
  const [manualScrapeOpen, setManualScrapeOpen] = useState(false)
  const [metadataEditOpen, setMetadataEditOpen] = useState(false)

  const refresh = async () => {
    if (!id) return
    setLoading(true)
    try {
      const m = await mediaAPI.get(id)
      setMedia(m)
      const favs = await playbackAPI.listFavourites().catch(() => [])
      setFavourite(favs.some((f) => f.id === m.id))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [id])

  const toggleFav = async () => {
    if (!media) return
    const state = await playbackAPI.toggleFavourite(media.id)
    setFavourite(state)
    toast.success(state ? '已加入我的收藏' : '已取消收藏')
  }

  const rescrape = async () => {
    if (!media) return
    await api.post(`/media/${media.id}/scrape`)
    toast.success('已触发重新刮削')
    await refresh()
  }

  const reprobe = async () => {
    if (!media) return
    try {
      const r = await api.post(`/media/${media.id}/probe`)
      if (r.data?.code === 0) {
        toast.success('重新探测成功')
      } else {
        toast.error(r.data?.error || '探测失败')
      }
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '探测失败，请检查 ffprobe 是否已安装'
      toast.error(msg)
    }
  }

  const exportNFO = async () => {
    if (!media) return
    try {
      const r = await recycleAPI.exportNFO(media.id)
      toast.success(`NFO 已成功写入 ${r.path}`)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '导出失败'
      toast.error(msg)
    }
  }

  const softDelete = async () => {
    if (!media) return
    if (!(await confirmAction({ title: '移入回收站', message: `将「${media.title}」移至回收站? (磁盘文件保留)`, confirmText: '移入回收站' }))) return
    await recycleAPI.softDelete(media.id)
    toast.success('已移至回收站')
    navigate(-1)
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-48">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-gray-100 border-t-gray-900" />
      </div>
    )
  }
  if (!media) {
    return (
      <div className="text-center py-24 bg-white rounded-2xl border border-gray-200">
        <p className="text-gray-500">媒体资源已被移除或不存在</p>
      </div>
    )
  }

  return (
    <div className="relative overflow-hidden rounded-3xl bg-white border border-gray-200/90 shadow-[0_1px_3px_rgba(0,0,0,0.01),0_1px_2px_rgba(0,0,0,0.015)]">
      {/* ── Cinematic Blurred Backdrop Glow ── */}
      <div className="absolute inset-0 h-[480px] z-0 overflow-hidden">
        {(media.backdrop_url || media.poster_url) ? (
          <img
            src={imageURL(media.backdrop_url || media.poster_url || '')}
            alt=""
            className="w-full h-full object-cover opacity-[0.04] scale-110 blur-2xl"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="w-full h-full bg-gradient-to-b from-gray-50 to-transparent" />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-white via-white/95 to-transparent" />
      </div>

      {/* ── Main Details Layout Container ── */}
      <div className="relative z-10 p-6 sm:p-10 flex flex-col md:flex-row gap-8 lg:gap-12">
        {/* Poster Card */}
        <div className="w-56 shrink-0 mx-auto md:mx-0">
          <motion.div 
            whileHover={{ scale: 1.02 }}
            className="aspect-[2/3] w-full rounded-2xl overflow-hidden bg-gray-50 border border-gray-200 shadow-md relative group"
          >
            {media.poster_url ? (
              <img
                src={imageURL(media.poster_url)}
                alt={media.title}
                className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
                referrerPolicy="no-referrer"
              />
            ) : (
              <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-gray-500 bg-gray-50">
                <FileText size={40} className="stroke-[1]" />
                <span className="text-xs uppercase tracking-wider font-bold">无海报</span>
              </div>
            )}
            
            {/* Quick Play overlay button */}
            <Link 
              to={`/play/${media.id}`}
              className="absolute inset-0 bg-[#111827]/40 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center"
            >
              <div className="flex h-14 w-14 items-center justify-center rounded-full bg-brand-500 text-white shadow-xl transform scale-90 group-hover:scale-100 transition-transform">
                <Play size={24} fill="currentColor" />
              </div>
            </Link>
          </motion.div>
        </div>

        {/* Detailed Metadata Body */}
        <div className="flex-1 space-y-6">
          {/* Title and Year Header */}
          <div className="space-y-3">
            <h1 className="font-display text-3xl sm:text-4xl font-extrabold tracking-tight text-gray-900 leading-tight">
              {media.title}
            </h1>
            <div className="flex flex-wrap items-center gap-2.5 text-xs text-gray-500 font-bold tracking-wide uppercase">
              {media.year > 0 && (
                <span className="inline-flex items-center gap-1 bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
                  <Calendar size={13} className="text-brand-500" />
                  <span>{media.year} 年</span>
                </span>
              )}
              {media.width > 0 && (
                <span className="inline-flex items-center gap-1 bg-brand-50 text-brand-700 border border-brand-100/50 px-2.5 py-1 rounded-xl">
                  <span>{media.width} × {media.height}</span>
                </span>
              )}
              <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
                {fmtSize(media.size_bytes)}
              </span>
              <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
                {fmtDuration(media.duration_sec)}
              </span>
              {media.container && (
                <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700 font-mono">
                  {media.container}
                </span>
              )}
            </div>
          </div>

          {/* Description Card */}
          {media.overview && (
            <div className="rounded-2xl bg-gray-50/50 border border-gray-100 p-5 space-y-2">
              <h3 className="text-xs font-bold uppercase tracking-widest text-brand-500">剧情简介</h3>
              <p className="text-sm text-gray-600 leading-relaxed font-semibold">
                {media.overview}
              </p>
            </div>
          )}

          {/* Tag Rows (Genres, Languages, Countries) */}
          <div className="space-y-4">
            {/* Genres */}
            {parseCSV(media.genres).length > 0 && (
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-xs font-bold text-gray-500 w-16 uppercase tracking-wider">类型流派</span>
                <div className="flex flex-wrap gap-2">
                  {parseCSV(media.genres).map((g) => (
                    <span key={g} className="rounded-full bg-brand-50 text-brand-700 border border-brand-100/30 px-3 py-1 text-2xs font-bold uppercase tracking-wider">
                      {g}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Languages */}
            {parseCSV(media.languages).length > 0 && (
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-xs font-bold text-gray-500 w-16 uppercase tracking-wider">语言</span>
                <div className="flex flex-wrap gap-2">
                  {parseCSV(media.languages).map((l) => (
                    <span key={l} className="rounded-xl bg-gray-100 text-gray-600 border border-gray-200/40 px-2.5 py-1 text-2xs font-semibold">
                      {l}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Countries */}
            {parseCSV(media.countries).length > 0 && (
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-xs font-bold text-gray-500 w-16 uppercase tracking-wider">国家/地区</span>
                <div className="flex flex-wrap gap-2">
                  {parseCSV(media.countries).map((c) => (
                    <span key={c} className="rounded-xl bg-gray-100 text-gray-600 border border-gray-200/40 px-2.5 py-1 text-2xs font-semibold">
                      {c}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>

          <div className="divider border-gray-200/60" />

          {/* Action Buttons Panel */}
          <div className="flex flex-col gap-5">
            <div className="flex flex-wrap gap-3">
              {/* Primary Direct Play */}
              <Link to={`/play/${media.id}`} className="btn-primary px-6 py-3.5 shadow-sm">
                <Play size={16} fill="currentColor" />
                <span>立即播放</span>
              </Link>

              {/* Transcode Playback */}
              <Link
                to={`/play/${media.id}?mode=hls`}
                className="btn-outline border-brand-500/30 hover:border-brand-500 text-[#c9954a] hover:bg-brand-50 px-5"
              >
                <RefreshCw size={14} className="animate-spin-slow" />
                <span>HLS 兼容转码播放</span>
              </Link>

              <ExternalPlayerButton mediaId={media.id} />

              {/* Toggle Favourites */}
              <button
                onClick={toggleFav}
                className={
                  'btn-outline gap-2 ' +
                  (favourite
                    ? '!border-red-200 !bg-red-50 !text-red-600 hover:!bg-red-100/50'
                    : 'hover:border-red-200 hover:text-red-600 hover:bg-red-50/50')
                }
              >
                <Heart size={14} fill={favourite ? 'currentColor' : 'none'} />
                <span>{favourite ? '取消收藏' : '加入收藏'}</span>
              </button>
            </div>

            {/* Admin Management Toolbar */}
            {role === 'admin' && (
              <div className="rounded-2xl border border-gray-200 bg-gray-50/50 p-5 space-y-3">
                <p className="text-[10px] font-bold uppercase tracking-[0.2em] text-[#c9954a]">系统后台高级控制面板</p>
                <div className="flex flex-wrap gap-2">
                  <button onClick={rescrape} className="btn-outline py-2 px-3.5 text-xs gap-1.5 border-gray-200 hover:border-brand-500/50 hover:bg-brand-50">
                    <Sparkles size={13} className="text-[#c9954a]" />
                    <span>智能刮削 (TMDB)</span>
                  </button>
                  <button onClick={() => setManualScrapeOpen(true)} className="btn-outline py-2 px-3.5 text-xs gap-1.5 border-gray-200 hover:border-brand-500/50 hover:bg-brand-50">
                    <Search size={13} className="text-[#c9954a]" />
                    <span>手动匹配刮削</span>
                  </button>
                  <button onClick={() => setMetadataEditOpen(true)} className="btn-outline py-2 px-3.5 text-xs gap-1.5 border-gray-200 hover:border-brand-500/50 hover:bg-brand-50">
                    <Pencil size={13} className="text-gray-600" />
                    <span>编辑元数据</span>
                  </button>
                  <button onClick={reprobe} className="btn-outline py-2 px-3.5 text-xs gap-1.5 border-gray-200 hover:border-brand-500/50 hover:bg-brand-50">
                    <Database size={13} className="text-gray-600" />
                    <span>探测媒体轨 (ffprobe)</span>
                  </button>
                  <button onClick={exportNFO} className="btn-outline py-2 px-3.5 text-xs gap-1.5 border-gray-200 hover:border-brand-500/50 hover:bg-brand-50">
                    <FileText size={13} />
                    <span>写出本地 NFO 属性</span>
                  </button>
                  <button
                    onClick={softDelete}
                    className="btn-outline py-2 px-3.5 text-xs gap-1.5 !border-red-100 !text-red-500 hover:!bg-red-50 hover:!border-red-200"
                  >
                    <Trash2 size={13} />
                    <span>移入回收站</span>
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
      <ManualScrapeDialog
        open={manualScrapeOpen}
        media={media}
        defaultQuery={media.title}
        mediaType={media.season_num > 0 || media.episode_num > 0 ? 'tv' : undefined}
        scopeLabel={media.title}
        onClose={() => setManualScrapeOpen(false)}
        onApplied={refresh}
      />
      <MetadataEditDialog
        open={metadataEditOpen}
        media={media}
        onClose={() => setMetadataEditOpen(false)}
        onSaved={async (next) => {
          setMedia(next)
          await refresh()
        }}
      />
    </div>
  )
}
