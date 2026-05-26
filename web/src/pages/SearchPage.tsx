import { ChangeEvent, FormEvent, useCallback, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Sparkles } from 'lucide-react'

import { aiAPI, type SearchIntent } from '../api/ai'
import { mediaAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

export function SearchPage() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [aiOn, setAiOn] = useState(false)
  const [aiAvailable, setAiAvailable] = useState(false)
  const [intent, setIntent] = useState<SearchIntent | null>(null)
  const [hasSearched, setHasSearched] = useState(false)

  useEffect(() => {
    aiAPI
      .status()
      .then((s) => setAiAvailable(s.enabled))
      .catch(() => setAiAvailable(false))
  }, [])

  const doQuickSearch = useCallback((query: string) => {
    if (!query.trim()) {
      setItems([])
      setHasSearched(false)
      setLoading(false)
      return
    }
    setHasSearched(true)
    setError('')
    mediaAPI
      .search(query, 60)
      .then((d) => {
        setItems(d.items ?? [])
        setIntent(null)
      })
      .catch((err) => {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '搜索失败'
        setError(msg)
        toast.error(msg)
      })
      .finally(() => setLoading(false))
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
    setLoading(true)
    setError('')
    setHasSearched(true)
    try {
      const data = await aiAPI.smartSearch(q)
      setItems(data.items ?? [])
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

  const showEmpty = !loading && !error && hasSearched && items.length === 0
  const showIdle = !loading && !error && !hasSearched

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="font-display text-3xl font-bold text-ink-600">搜索</h1>
        <button
          disabled={!aiAvailable}
          className={
            'neon-button !px-3 !py-1 !text-xs ' +
            (aiOn ? '!border-accent-400 !bg-accent-400/20 !text-accent-400' : '')
          }
          onClick={() => setAiOn((on) => !on)}
          title={aiAvailable ? '启用 AI 智能搜索' : '请先在 ai.* 中配置 API Key'}
        >
          <Sparkles size={12} /> {aiOn ? 'AI 已开启' : 'AI 智能搜索'}
        </button>
      </header>

      {aiOn ? (
        <form onSubmit={onAISubmit} className="flex gap-2">
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

      {items.length > 0 && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </div>
      )}
    </div>
  )
}
