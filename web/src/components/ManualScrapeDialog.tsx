import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from 'react'
import { Check, LoaderCircle, Search, Sparkles, X } from 'lucide-react'
import toast from 'react-hot-toast'

import { imageURL } from '../api/client'
import { mediaAPI, type ManualScrapeCandidate } from '../api/library'
import type { Media } from '../types'
import { EpisodeArtworkToggle } from './EpisodeArtworkToggle'

interface ManualScrapeDialogProps {
  open: boolean
  media: Media | null
  mediaIds?: string[]
  defaultQuery?: string
  mediaType?: string
  scopeLabel?: string
  episodeArtwork?: boolean
  onClose: () => void
  onApplied?: () => void
}

const providers = [
  { value: 'tmdb', label: 'TMDb' },
  { value: 'douban', label: '豆瓣' },
  { value: 'bangumi', label: 'Bangumi' },
  { value: 'thetvdb', label: 'TheTVDB' },
  { value: 'adult', label: 'Adult / 番号' },
]

type ProviderMode = 'all' | 'single' | 'multi'

export function ManualScrapeDialog({
  open,
  media,
  mediaIds,
  defaultQuery,
  mediaType,
  scopeLabel,
  episodeArtwork,
  onClose,
  onApplied,
}: ManualScrapeDialogProps) {
  const [query, setQuery] = useState('')
  const [providerMode, setProviderMode] = useState<ProviderMode>('all')
  const [selectedProviders, setSelectedProviders] = useState<string[]>([])
  const [includeEpisodeArtwork, setIncludeEpisodeArtwork] = useState(false)
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
    setProviderMode('all')
    setSelectedProviders([])
    setIncludeEpisodeArtwork(episodeArtwork ?? false)
    setItems([])
    setApplyingKey('')
  }, [defaultQuery, episodeArtwork, media?.title, open])

  if (!open || !media) return null

  const runSearch = async () => {
    const text = query.trim()
    if (!text) {
      toast.error('请输入标题或 TMDb/豆瓣/Bangumi/TheTVDB ID')
      return
    }
    setSearching(true)
    setItems([])
    const providerValues = manualSearchProvidersForMode(providerMode, selectedProviders)
    if (providerValues.length === 0) {
      toast.error('至少选择一个刮削源')
      setSearching(false)
      return
    }
    try {
      const settled = await Promise.allSettled(
        providerValues.map(async (provider) => {
          const results = await mediaAPI.manualScrapeSearch(media.id, {
            query: text,
            provider,
            media_type: mediaType,
          })
          setItems((current) => mergeManualCandidates(current, results))
          return { provider, results }
        }),
      )
      const found = settled.reduce((sum, result) => (
        result.status === 'fulfilled' ? sum + result.value.results.length : sum
      ), 0)
      const failed = settled.filter((result) => result.status === 'rejected').length
      if (found === 0) {
        toast.error(failed === providerValues.length ? '所有刮削源搜索失败' : '没有找到可用候选')
      } else if (failed > 0) {
        toast.error(`${failed} 个刮削源搜索失败，已显示其余结果`)
      }
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
      const options = { episode_images: includeEpisodeArtwork }
      if (targetIds.length > 1) {
        const result = await mediaAPI.applyManualScrapeBatch(targetIds, item, options)
        toast.success(`已应用到 ${result.applied} 个媒体`)
      } else {
        await mediaAPI.applyManualScrape(media.id, item, options)
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

        <div className="flex flex-col gap-3 border-b border-sand-200 p-5 lg:flex-row">
          <div className="flex min-w-0 flex-col gap-2 lg:max-w-sm">
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => changeProviderMode('all', setProviderMode, setSelectedProviders)}
                className={providerModeButtonClass(providerMode === 'all')}
              >
                {providerMode === 'all' && <Check size={13} />}
                全部源
              </button>
              <button
                type="button"
                onClick={() => changeProviderMode('single', setProviderMode, setSelectedProviders)}
                className={providerModeButtonClass(providerMode === 'single')}
              >
                {providerMode === 'single' && <Check size={13} />}
                单源
              </button>
              <button
                type="button"
                onClick={() => changeProviderMode('multi', setProviderMode, setSelectedProviders)}
                className={providerModeButtonClass(providerMode === 'multi')}
              >
                {providerMode === 'multi' && <Check size={13} />}
                多源
              </button>
            </div>
            {providerMode !== 'all' && (
              <div className="flex flex-wrap gap-2">
                {providers.map((item) => {
                  const active = selectedProviders.includes(item.value)
                  return (
                    <button
                      key={item.value}
                      type="button"
                      onClick={() => chooseProvider(item.value, providerMode, setSelectedProviders)}
                      className={providerButtonClass(active)}
                    >
                      {active && <Check size={13} />}
                      {item.label}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
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
          {isEpisodeArtworkTarget(media, mediaType, targetIds.length) && (
            <EpisodeArtworkToggle
              checked={includeEpisodeArtwork}
              onChange={setIncludeEpisodeArtwork}
              title="关闭后仍写入每集简介、评分和时长，只跳过每集图片"
            />
          )}
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
                        {item.nsfw ? <span className="rounded-full bg-rose-50 px-2 py-0.5 text-[11px] font-bold text-rose-600">成人</span> : null}
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

function manualSearchProvidersForMode(mode: ProviderMode, selectedProviders: string[]): string[] {
  if (mode === 'all') return providers.map((provider) => provider.value)
  if (mode === 'single') return [selectedProviders[0] || providers[0].value]
  return selectedProviders
}

function changeProviderMode(
  mode: ProviderMode,
  setProviderMode: Dispatch<SetStateAction<ProviderMode>>,
  setSelectedProviders: Dispatch<SetStateAction<string[]>>,
) {
  setProviderMode(mode)
  setSelectedProviders((current) => {
    if (mode === 'all') return []
    if (mode === 'single') return [current[0] || providers[0].value]
    return current.length > 0 ? current : providers.slice(0, 4).map((provider) => provider.value)
  })
}

function chooseProvider(
  value: string,
  mode: ProviderMode,
  setSelectedProviders: Dispatch<SetStateAction<string[]>>,
) {
  setSelectedProviders((current) => {
    if (mode === 'single') return [value]
    if (current.includes(value)) {
      return current.filter((item) => item !== value)
    }
    return [...current, value]
  })
}

function mergeManualCandidates(current: ManualScrapeCandidate[], incoming: ManualScrapeCandidate[]): ManualScrapeCandidate[] {
  if (incoming.length === 0) return current
  const byKey = new Map(current.map((item) => [candidateKey(item), item]))
  for (const item of incoming) {
    byKey.set(candidateKey(item), item)
  }
  return Array.from(byKey.values())
}

function providerModeButtonClass(active: boolean): string {
  return (
    'inline-flex h-10 items-center gap-1.5 rounded-xl border px-3 text-xs font-bold transition ' +
    (active
      ? 'border-brand-300 bg-brand-50 text-brand-700'
      : 'border-sand-200 bg-white text-sand-600 hover:border-brand-200 hover:text-brand-600')
  )
}

function providerButtonClass(active: boolean): string {
  return (
    'inline-flex h-11 items-center gap-1.5 rounded-xl border px-3 text-xs font-bold transition ' +
    (active
      ? 'border-brand-300 bg-brand-50 text-brand-700'
      : 'border-sand-200 bg-white text-sand-600 hover:border-brand-200 hover:text-brand-600')
  )
}

function isEpisodeArtworkTarget(media: Media, mediaType?: string, targetCount = 1): boolean {
  const type = (mediaType || '').toLowerCase()
  return type === 'tv' || type === 'anime' || type === 'variety' || media.season_num > 0 || media.episode_num > 0 || targetCount > 1
}

function candidateIDText(item: ManualScrapeCandidate): string {
  const parts = [
    item.tmdb_id ? `TMDb ${item.tmdb_id}` : '',
    item.original_name && item.source === 'adult' ? `番号 ${item.original_name}` : '',
    item.douban_id ? `豆瓣 ${item.douban_id}` : '',
    item.bangumi_id ? `Bangumi ${item.bangumi_id}` : '',
    item.thetvdb_id ? `TheTVDB ${item.thetvdb_id}` : '',
    item.media_type ? item.media_type : '',
  ].filter(Boolean)
  return parts.join(' · ')
}
