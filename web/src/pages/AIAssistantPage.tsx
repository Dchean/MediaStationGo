import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, Search, Sparkles, Wand2 } from 'lucide-react'
import toast from 'react-hot-toast'

import { aiAPI, type SearchIntent } from '../api/ai'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

// AIAssistantPage exposes the two AI helpers backed by the Go server:
//   - smart search: parses a natural-language query into a SearchIntent +
//     a list of matching local media items.
//   - recommendations: returns a list of recommended titles based on the
//     current user's recent watch history.
//
// The Vue version had a full chat surface; the Go backend has no chat or
// operation-execute endpoints, so we render the same two capabilities as
// a focused two-panel screen.
export function AIAssistantPage() {
  const [status, setStatus] = useState<{
    enabled: boolean
    provider: string
    model: string
  } | null>(null)
  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [intent, setIntent] = useState<SearchIntent | null>(null)
  const [items, setItems] = useState<Media[]>([])

  const [recs, setRecs] = useState<string[] | null>(null)
  const [recommending, setRecommending] = useState(false)

  useEffect(() => {
    aiAPI
      .status()
      .then(setStatus)
      .catch(() => setStatus({ enabled: false, provider: '', model: '' }))
  }, [])

  const onSearch = async (e: FormEvent) => {
    e.preventDefault()
    if (!query.trim()) return
    setSearching(true)
    setIntent(null)
    setItems([])
    try {
      const r = await aiAPI.smartSearch(query.trim())
      setIntent(r.intent)
      setItems(r.items)
      if (r.items.length === 0) toast('未找到匹配项')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '搜索失败'
      toast.error(msg)
    } finally {
      setSearching(false)
    }
  }

  const onRecommend = async () => {
    setRecommending(true)
    try {
      const titles = await aiAPI.recommend()
      setRecs(titles)
      if (titles.length === 0) toast('暂无可推荐内容,请先观看一些媒体')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '获取推荐失败'
      toast.error(msg)
    } finally {
      setRecommending(false)
    }
  }

  const quickHints = [
    '2023 年的科幻电影',
    '评分高的动漫',
    '最近添加的纪录片',
    '中文剧集',
  ]

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-primary-400 to-purple-500">
            <Sparkles className="h-5 w-5 text-white" />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-white">AI 助手</h1>
            <p className="text-sm text-slate-400">
              自然语言搜索 · 基于观影历史的智能推荐
            </p>
          </div>
        </div>
        {status && (
          <div className="text-xs text-slate-400">
            <span
              className={
                'mr-2 inline-block h-2 w-2 rounded-full ' +
                (status.enabled ? 'bg-emerald-400' : 'bg-slate-500')
              }
            />
            {status.enabled
              ? `已连接 · ${status.provider}${status.model ? ' / ' + status.model : ''}`
              : '未配置 AI 服务,使用本地规则解析'}
          </div>
        )}
      </div>

      {/* Smart search */}
      <section className="glass-panel space-y-4">
        <h2 className="font-display text-lg font-semibold text-white">智能搜索</h2>
        <form onSubmit={onSearch} className="flex gap-2">
          <input
            className="input-base flex-1"
            placeholder="试试: 2023 年的高分动作片"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button type="submit" disabled={searching} className="neon-button">
            {searching ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Search size={16} />
            )}
            搜索
          </button>
        </form>

        <div className="flex flex-wrap gap-2">
          {quickHints.map((h) => (
            <button
              key={h}
              onClick={() => {
                setQuery(h)
              }}
              className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs text-slate-300 hover:border-primary-400/40 hover:text-primary-400"
            >
              {h}
            </button>
          ))}
        </div>

        {intent && (
          <div className="rounded-lg border border-white/5 bg-white/5 p-3 text-xs text-slate-300">
            <div className="mb-1 font-semibold text-slate-200">解析结果</div>
            <div className="flex flex-wrap gap-x-6 gap-y-1">
              <span>
                查询: <span className="text-primary-400">{intent.query || '—'}</span>
              </span>
              {intent.year !== undefined && intent.year > 0 && (
                <span>
                  年份: <span className="text-primary-400">{intent.year}</span>
                </span>
              )}
              {intent.genre && (
                <span>
                  类型: <span className="text-primary-400">{intent.genre}</span>
                </span>
              )}
              {intent.type && (
                <span>
                  分类: <span className="text-primary-400">{intent.type}</span>
                </span>
              )}
              {intent.sort && (
                <span>
                  排序: <span className="text-primary-400">{intent.sort}</span>
                </span>
              )}
              {intent.language && (
                <span>
                  语言: <span className="text-primary-400">{intent.language}</span>
                </span>
              )}
            </div>
          </div>
        )}

        {items.length > 0 && (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {items.map((m) => (
              <MediaCard key={m.id} media={m} />
            ))}
          </div>
        )}
      </section>

      {/* Recommendations */}
      <section className="glass-panel space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="font-display text-lg font-semibold text-white">为你推荐</h2>
          <button onClick={onRecommend} disabled={recommending} className="neon-button">
            {recommending ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Wand2 size={16} />
            )}
            生成推荐
          </button>
        </div>
        <p className="text-xs text-slate-500">
          推荐基于你的最近观看历史。点击标题在媒体库中查找。
        </p>

        {recs && recs.length > 0 && (
          <ul className="grid gap-2 sm:grid-cols-2">
            {recs.map((t, i) => (
              <li key={i}>
                <Link
                  to={`/search?q=${encodeURIComponent(t)}`}
                  className="flex items-center justify-between rounded-lg border border-white/5 bg-white/5 px-3 py-2 text-sm text-slate-200 hover:border-primary-400/40 hover:text-primary-400"
                >
                  <span className="truncate">{t}</span>
                  <Search size={14} className="shrink-0 opacity-60" />
                </Link>
              </li>
            ))}
          </ul>
        )}

        {recs && recs.length === 0 && (
          <p className="text-sm text-slate-400">
            还没有推荐结果 — 先去看几部片子,我再给你挑。
          </p>
        )}
      </section>

      {/* Decorative footer (mirrors the Vue page hint that AI runs locally). */}
      {!status?.enabled && (
        <p className="text-xs text-slate-500">
          提示: 当前未配置外部 AI Provider,系统将使用本地规则引擎解析查询。
          管理员可在 <Link to="/api-configs" className="text-primary-400">API 配置</Link>{' '}
          中接入 OpenAI / DeepSeek 等服务以获得更好效果。
        </p>
      )}
    </div>
  )
}
