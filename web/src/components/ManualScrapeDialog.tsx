import { useEffect, useMemo, useState } from 'react'
import { Check, LoaderCircle, Search, Sparkles, X } from 'lucide-react'
import toast from 'react-hot-toast'

import { imageURL } from '../api/client'
import { mediaAPI, type ManualScrapeCandidate } from '../api/library'
import type { Media } from '../types'

interface ManualScrapeDialogProps {
  open: boolean
  media: Media | null
  mediaIds?: string[]
  defaultQuery?: string
  mediaType?: string
  scopeLabel?: string
  onClose: () => void
  onApplied?: () => void
}

const providers = [
  { value: 'all', label: '全部源' },
  { value: 'tmdb', label: 'TMDb' },
  { value: 'douban', label: '豆瓣' },
  { value: 'bangumi', label: 'Bangumi' },
  { value: 'thetvdb', label: 'TheTVDB' },
]

export function ManualScrapeDialog({
  open,
  media,
  mediaIds,
  defaultQuery,
  mediaType,
  scopeLabel,
  onClose,
  onApplied,
}: ManualScrapeDialogProps) {
  const [query, setQuery] = useState('')
  const [provider, setProvider] = useState('all')
  const [searching, setSearching] = useState(false)
  const [applyingKey, setApplyingKey] = useState('')
  const [items, setItems] = useState<ManualScrapeCandidate[]>([])

  const targetIds = useMemo(() => {
    const ids = (mediaIds && mediaIds.length > 0 ? mediaIds : media ? [media.id] : []).filter(Boolean)
    return Array.from(new Set(ids))
  }, [media, mediaIds])

  useEffect(() => {
    if (!open) return
    setQuery(defaultQuery || media?.title || '')
    setProvider('all')
    setItems([])
    setApplyingKey('')
  }, [defaultQuery, media?.title, open])

  if (!open || !media) return null

  const runSearch = async () => {
    const text = query.trim()
    if (!text) {
      toast.error('请输入标题或 TMDb/豆瓣/Bangumi/TheTVDB ID')
      return
    }
    setSearching(true)
    try {
      const results = await mediaAPI.manualScrapeSearch(media.id, {
        query: text,
        provider,
        media_type: mediaType,
      })
      setItems(results)
      if (results.length === 0) toast.error('没有找到可用候选')
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '搜索失败'
      toast.error(msg)
    } finally {
      setSearching(false)
    }
  }

  const apply = async (item: ManualScrapeCandidate) => {
    const key = candidateKey(item)
    setApplyingKey(key)
    try {
      if (targetIds.length > 1) {
        const result = await mediaAPI.applyManualScrapeBatch(targetIds, item)
        toast.success(`已应用到 ${result.applied} 个媒体`)
      } else {
        await mediaAPI.applyManualScrape(media.id, item)
        toast.success('已应用手动匹配')
      }
      onApplied?.()
      onClose()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '应用失败'
      toast.error(msg)
    } finally {
      setApplyingKey('')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 px-4 py-8 backdrop-blur-sm">
      <div className="flex max-h-[86vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-sand-200 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-sand-200 px-5 py-4">
          <div>
            <h2 className="font-display text-xl font-bold text-ink-600">手动搜索刮削</h2>
            <p className="mt-1 text-xs text-sand-500">
              {scopeLabel || media.title} · {targetIds.length > 1 ? `将应用到 ${targetIds.length} 个媒体` : '单个媒体'}
            </p>
          </div>
          <button onClick={onClose} className="btn-ghost h-9 w-9 p-0" aria-label="关闭">
            <X size={16} />
          </button>
        </div>

        <div className="flex flex-col gap-3 border-b border-sand-200 p-5 sm:flex-row">
          <select
            value={provider}
            onChange={(event) => setProvider(event.target.value)}
            className="h-11 rounded-xl border border-sand-200 bg-white px-3 text-sm font-semibold text-ink-600 outline-none focus:border-brand-300"
          >
            {providers.map((item) => (
              <option key={item.value} value={item.value}>{item.label}</option>
            ))}
          </select>
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-sand-500" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter') runSearch() }}
              className="h-11 w-full rounded-xl border border-sand-200 bg-white pl-9 pr-3 text-sm font-semibold text-ink-600 outline-none focus:border-brand-300"
              placeholder="输入标题或 TMDb / 豆瓣 / Bangumi / TheTVDB ID"
            />
          </div>
          <button onClick={runSearch} disabled={searching} className="btn-primary h-11 px-5">
            {searching ? <LoaderCircle size={16} className="animate-spin" /> : <Sparkles size={16} />}
            搜索
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          {items.length === 0 ? (
            <div className="flex min-h-56 items-center justify-center rounded-xl border border-dashed border-sand-200 text-sm text-sand-500">
              搜索后在这里选择正确的元数据
            </div>
          ) : (
            <div className="grid gap-3">
              {items.map((item) => {
                const key = candidateKey(item)
                const applying = applyingKey === key
                return (
                  <div key={key} className="flex gap-4 rounded-xl border border-sand-200 bg-white p-3 shadow-sm">
                    <div className="h-28 w-20 shrink-0 overflow-hidden rounded-lg bg-sand-100">
                      {item.poster_url ? (
                        <img src={imageURL(item.poster_url)} alt={item.title} className="h-full w-full object-cover" referrerPolicy="no-referrer" />
                      ) : (
                        <div className="flex h-full items-center justify-center text-xs text-sand-500">无海报</div>
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="truncate font-semibold text-ink-600">{item.title}</h3>
                        <span className="rounded-full bg-brand-50 px-2 py-0.5 text-[11px] font-bold uppercase text-brand-700">{item.source}</span>
                        {item.year ? <span className="text-xs text-sand-500">{item.year}</span> : null}
                      </div>
                      <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-50">{item.overview || '暂无简介'}</p>
                      <p className="mt-2 text-[11px] font-semibold text-sand-500">{candidateIDText(item)}</p>
                    </div>
                    <button onClick={() => apply(item)} disabled={!!applyingKey} className="btn-outline h-10 shrink-0 self-center px-3 text-xs">
                      {applying ? <LoaderCircle size={14} className="animate-spin" /> : <Check size={14} />}
                      应用匹配
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function candidateKey(item: ManualScrapeCandidate): string {
  return `${item.source}:${item.tmdb_id || item.bangumi_id || item.douban_id || item.thetvdb_id || item.title}:${item.media_type || ''}`
}

function candidateIDText(item: ManualScrapeCandidate): string {
  const parts = [
    item.tmdb_id ? `TMDb ${item.tmdb_id}` : '',
    item.douban_id ? `豆瓣 ${item.douban_id}` : '',
    item.bangumi_id ? `Bangumi ${item.bangumi_id}` : '',
    item.thetvdb_id ? `TheTVDB ${item.thetvdb_id}` : '',
    item.media_type ? item.media_type : '',
  ].filter(Boolean)
  return parts.join(' · ')
}
