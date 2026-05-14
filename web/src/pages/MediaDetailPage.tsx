import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Heart, Play, RefreshCw, Sparkles } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { playbackAPI } from '../api/playback'
import { imageURL } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { api } from '../api/client'
import type { Media } from '../types'

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

// Detail screen for a single media item.
//
// Buttons:
//   - Play         (direct play)
//   - HLS          (force transcoded play)
//   - Favourite    (toggle, all users)
//   - Re-scrape    (admin only, triggers TMDb lookup)
//   - Re-probe     (admin only, refreshes ffprobe metadata)
export function MediaDetailPage() {
  const { id = '' } = useParams()
  const role = useAuthStore((s) => s.user?.role)
  const [media, setMedia] = useState<Media | null>(null)
  const [favourite, setFavourite] = useState(false)
  const [loading, setLoading] = useState(true)

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
    toast.success(state ? '已加入收藏' : '已取消收藏')
  }

  const rescrape = async () => {
    if (!media) return
    await api.post(`/media/${media.id}/scrape`)
    toast.success('已刮削')
    await refresh()
  }

  const reprobe = async () => {
    if (!media) return
    await api.post(`/media/${media.id}/probe`)
    toast.success('已重新探测')
    await refresh()
  }

  if (loading) return <p className="text-slate-500">加载中…</p>
  if (!media) return <p className="text-slate-500">媒体不存在</p>

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-6 md:flex-row">
        <div className="aspect-[2/3] w-48 shrink-0 overflow-hidden rounded-xl bg-surface-800">
          {media.poster_url ? (
            <img
              src={imageURL(media.poster_url)}
              alt={media.title}
              className="h-full w-full object-cover"
              referrerPolicy="no-referrer"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-slate-600">
              无海报
            </div>
          )}
        </div>
        <div className="flex-1 space-y-4">
          <h1 className="font-display text-3xl font-bold text-white">{media.title}</h1>
          {media.year > 0 && <p className="text-slate-400">{media.year}</p>}
          {media.overview && <p className="text-slate-300">{media.overview}</p>}

          <div className="flex flex-wrap gap-2 text-xs text-slate-400">
            {media.video_codec && <Badge>{media.video_codec.toUpperCase()}</Badge>}
            {media.width > 0 && (
              <Badge>
                {media.width}×{media.height}
              </Badge>
            )}
            <Badge>{fmtDuration(media.duration_sec)}</Badge>
            <Badge>{fmtSize(media.size_bytes)}</Badge>
            {media.container && <Badge>{media.container.toUpperCase()}</Badge>}
            <Badge>{media.scrape_status}</Badge>
          </div>

          <div className="flex flex-wrap gap-2">
            <Link to={`/play/${media.id}`} className="neon-button">
              <Play size={18} /> 播放
            </Link>
            <Link
              to={`/play/${media.id}?mode=hls`}
              className="neon-button !border-accent-400/40 !bg-accent-400/10 !text-accent-400 hover:!border-accent-400"
            >
              <RefreshCw size={16} /> HLS 转码播放
            </Link>
            <button
              onClick={toggleFav}
              className={
                'neon-button ' +
                (favourite
                  ? '!border-pink-400/60 !bg-pink-400/10 !text-pink-400'
                  : '')
              }
            >
              <Heart size={16} fill={favourite ? 'currentColor' : 'none'} />
              {favourite ? '已收藏' : '收藏'}
            </button>
            {role === 'admin' && (
              <>
                <button onClick={rescrape} className="neon-button">
                  <Sparkles size={16} /> 重新刮削
                </button>
                <button onClick={reprobe} className="neon-button">
                  <RefreshCw size={16} /> 重新探测
                </button>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function Badge({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded border border-white/10 bg-white/5 px-2 py-0.5">
      {children}
    </span>
  )
}
