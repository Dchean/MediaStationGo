import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, Rss, Search, Sparkles, Wand2 } from 'lucide-react'
import toast from 'react-hot-toast'

import { aiAPI, type ExternalMediaResult, type SearchIntent } from '../api/ai'
import { imageURL } from '../api/client'
import { subscriptionsAPI } from '../api/subscriptions'
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
  const [externalItems, setExternalItems] = useState<ExternalMediaResult[]>([])
  const [subscribing, setSubscribing] = useState('')

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
    setExternalItems([])
    try {
      const r = await aiAPI.smartSearch(query.trim())
      setIntent(r.intent)
      setItems(r.items)
      setExternalItems(r.external_items ?? [])
      if (r.items.length === 0 && (r.external_items ?? []).length === 0) toast('未找到匹配项')
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
            <Sparkles className="h-5 w-5 text-ink-600" />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-ink-600">AI 助手</h1>
            <p className="text-sm text-ink-50">
              自然语言搜索 · 基于观影历史的智能推荐
            </p>
          </div>
        </div>
        {status && (
          <div className="text-xs text-ink-50">
            <span
              className={
                'mr-2 inline-block h-2 w-2 rounded-full ' +
                (status.enabled ? 'bg-emerald-400' : 'bg-sand-500/30')
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
        <h2 className="font-display text-lg font-semibold text-ink-600">智能搜索</h2>
        <form onSubmit={onSearch} className="flex flex-wrap gap-2">
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
              className="rounded-full border border-gray-200 bg-gray-50 px-3 py-1 text-xs text-ink-100 hover:border-primary-400/40 hover:text-brand-500"
            >
              {h}
            </button>
          ))}
        </div>

        {intent && (
          <div className="rounded-xl border border-gray-200 bg-gray-50 p-3 text-xs text-ink-100">
            <div className="mb-1 font-semibold text-ink-200">解析结果</div>
            <div className="flex flex-wrap gap-x-6 gap-y-1">
              <span>
                查询: <span className="text-brand-500">{intent.query || '—'}</span>
              </span>
              {intent.year !== undefined && intent.year > 0 && (
                <span>
                  年份: <span className="text-brand-500">{intent.year}</span>
                </span>
              )}
              {intent.genre && (
                <span>
                  类型: <span className="text-brand-500">{intent.genre}</span>
                </span>
              )}
              {intent.type && (
                <span>
                  分类: <span className="text-brand-500">{intent.type}</span>
                </span>
              )}
              {intent.sort && (
                <span>
                  排序: <span className="text-brand-500">{intent.sort}</span>
                </span>
              )}
              {intent.language && (
                <span>
                  语言: <span className="text-brand-500">{intent.language}</span>
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

        {externalItems.length > 0 && (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {externalItems.map((item) => {
              const keyword = item.subscribe_keyword || item.title
              const key = `${item.source}:${keyword}`
              return (
                <article key={key} className="rounded-2xl border border-gray-200 bg-gray-50 p-3">
                  <div className="flex gap-3">
                    <div className="h-24 w-16 shrink-0 overflow-hidden rounded-xl bg-white">
                      {item.poster_url ? (
                        <img
                          src={imageURL(item.poster_url)}
                          alt={item.title}
                          className="h-full w-full object-cover"
                        />
                      ) : null}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="mb-1 flex flex-wrap gap-2 text-[10px] uppercase text-brand-500">
                        <span>{item.source}</span>
                        {item.media_type && <span>{item.media_type}</span>}
                        {item.year ? <span>{item.year}</span> : null}
                      </div>
                      <h3 className="truncate font-semibold text-ink-600">{item.title}</h3>
                      <p className="mt-1 line-clamp-2 text-xs text-ink-50">
                        {item.overview || `订阅关键词：${keyword}`}
                      </p>
                      <button
                        disabled={subscribing === key}
                        onClick={async () => {
                          setSubscribing(key)
                          try {
                            const feed = `site-search://search?keyword=${encodeURIComponent(keyword)}&source=${encodeURIComponent(item.source)}`
                            const sub = await subscriptionsAPI.create({
                              name: `${item.title} 自动订阅`,
                              feed_url: feed,
                              filter: keyword,
                              media_type: item.media_type,
                              source: item.source,
                              poster_url: item.poster_url,
                              backdrop_url: item.backdrop_url,
                              overview: item.overview,
                              enabled: true,
                            })
                            const run = await subscriptionsAPI.runNow(sub.id)
                            toast.success(
                              run.queued > 0
                                ? `已订阅并加入 ${run.queued} 个下载`
                                : '已订阅，暂未在 PT 站点找到可下载资源',
                            )
                          } catch (err: unknown) {
                            const msg =
                              (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
                              '订阅失败'
                            toast.error(msg)
                          } finally {
                            setSubscribing('')
                          }
                        }}
                        className="mt-2 rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10 disabled:opacity-50"
                      >
                        <Rss size={12} className="mr-1 inline" />
                        {subscribing === key ? '订阅中…' : '订阅并搜索 PT'}
                      </button>
                    </div>
                  </div>
                </article>
              )
            })}
          </div>
        )}
      </section>

      {/* Recommendations */}
      <section className="glass-panel space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="font-display text-lg font-semibold text-ink-600">为你推荐</h2>
          <button onClick={onRecommend} disabled={recommending} className="neon-button">
            {recommending ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Wand2 size={16} />
            )}
            生成推荐
          </button>
        </div>
        <p className="text-xs text-sand-500">
          推荐基于你的最近观看历史。点击标题在媒体库中查找。
        </p>

        {recs && recs.length > 0 && (
          <ul className="grid gap-2 sm:grid-cols-2">
            {recs.map((t, i) => (
              <li key={i}>
                <Link
                  to={`/search?q=${encodeURIComponent(t)}`}
                  className="flex items-center justify-between rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-ink-200 hover:border-primary-400/40 hover:text-brand-500"
                >
                  <span className="truncate">{t}</span>
                  <Search size={14} className="shrink-0 opacity-60" />
                </Link>
              </li>
            ))}
          </ul>
        )}

        {recs && recs.length === 0 && (
          <p className="text-sm text-ink-50">
            还没有推荐结果 — 先去看几部片子,我再给你挑。
          </p>
        )}
      </section>

      {/* Decorative footer (mirrors the Vue page hint that AI runs locally). */}
      {!status?.enabled && (
        <p className="text-xs text-sand-500">
          提示: 当前未配置外部 AI Provider,系统将使用本地规则引擎解析查询。
          管理员可在 <Link to="/api-configs" className="text-brand-500">API 配置</Link>{' '}
          中接入 OpenAI / DeepSeek 等服务以获得更好效果。
        </p>
      )}
    </div>
  )
}
