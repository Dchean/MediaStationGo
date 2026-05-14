import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import { seriesAPI, type SeasonGroup } from '../api/series'
import type { Library, Media } from '../types'
import { MediaCard } from '../components/MediaCard'
import { useAuthStore } from '../stores/auth'

// LibraryPage renders one of two layouts depending on the library type:
//
//   - movie / music / default → flat poster grid with pagination.
//   - tv / anime              → grouped by season with an episode list.
//
// Admins also get scan + scrape buttons in the header.
export function LibraryPage() {
  const { id = '' } = useParams()
  const role = useAuthStore((s) => s.user?.role)

  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [seasons, setSeasons] = useState<SeasonGroup[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [scraping, setScraping] = useState(false)

  const isSeries = library?.type === 'tv' || library?.type === 'anime'

  useEffect(() => {
    if (!id) return
    setLoading(true)
    Promise.all([
      libraryAPI.list().then((all) => all.find((l) => l.id === id) ?? null),
    ]).then(([lib]) => setLibrary(lib))
  }, [id])

  useEffect(() => {
    if (!id || !library) return
    setLoading(true)
    if (library.type === 'tv' || library.type === 'anime') {
      seriesAPI
        .seasons(id)
        .then((s) => setSeasons(s))
        .finally(() => setLoading(false))
    } else {
      libraryAPI
        .listMedia(id)
        .then((d) => {
          setItems(d.items)
          setTotal(d.total)
        })
        .finally(() => setLoading(false))
    }
  }, [id, library])

  const handleScan = async () => {
    setScanning(true)
    try {
      const r = await libraryAPI.scan(id)
      toast.success(`扫描完成:新增 ${r.added} 项,探测 ${r.probed} 项`)
      // Force a reload by toggling library state.
      setLibrary((l) => (l ? { ...l } : l))
    } catch {
      toast.error('扫描失败')
    } finally {
      setScanning(false)
    }
  }

  const handleScrape = async () => {
    setScraping(true)
    try {
      await libraryAPI.scrape(id)
      toast.success('刮削已加入后台队列')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '刮削失败'
      toast.error(msg)
    } finally {
      setScraping(false)
    }
  }

  const heading = useMemo(
    () => (library ? `${library.name}` : '媒体库'),
    [library],
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-display text-3xl font-bold text-white">
            {heading}
            {!isSeries && <span className="text-slate-500"> ({total})</span>}
          </h1>
          {library && (
            <p className="text-sm text-slate-400">
              {library.type} · {library.path}
            </p>
          )}
        </div>
        {role === 'admin' && (
          <div className="flex gap-2">
            <button onClick={handleScan} disabled={scanning} className="neon-button">
              {scanning ? '扫描中…' : '立即扫描'}
            </button>
            <button onClick={handleScrape} disabled={scraping} className="neon-button">
              {scraping ? '刮削中…' : '刮削元数据'}
            </button>
          </div>
        )}
      </div>

      {loading && <p className="text-slate-500">加载中…</p>}

      {!isSeries && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </div>
      )}

      {isSeries &&
        seasons.map((s) => (
          <section key={s.season} className="space-y-3">
            <h2 className="font-display text-xl font-semibold text-white">
              {s.season > 0 ? `第 ${s.season} 季` : '未分季'} ({s.episodes.length})
            </h2>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
              {s.episodes.map((e) => (
                <div key={e.id} className="space-y-1">
                  <MediaCard media={e} />
                  {e.episode_num > 0 && (
                    <p className="px-1 text-xs text-slate-400">第 {e.episode_num} 集</p>
                  )}
                </div>
              ))}
            </div>
          </section>
        ))}

      {isSeries && !loading && seasons.length === 0 && (
        <p className="text-slate-400">该库尚未发现任何剧集,触发一次扫描后再来看看。</p>
      )}
    </div>
  )
}
