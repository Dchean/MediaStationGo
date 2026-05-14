import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Play, Plus, Trash2 } from 'lucide-react'

import { subscriptionsAPI } from '../api/subscriptions'
import type { Subscription } from '../types'

export function SubscriptionsPage() {
  const [items, setItems] = useState<Subscription[]>([])
  const [name, setName] = useState('')
  const [feed, setFeed] = useState('')
  const [filter, setFilter] = useState('')
  const [loading, setLoading] = useState(true)

  const refresh = () =>
    subscriptionsAPI
      .list()
      .then(setItems)
      .finally(() => setLoading(false))

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await subscriptionsAPI.create({ name, feed_url: feed, filter })
      toast.success('已创建订阅')
      setName('')
      setFeed('')
      setFilter('')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '创建失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-white">RSS 订阅</h1>
      <p className="text-sm text-slate-400">
        定期轮询 RSS 源(每 10 分钟一次),将匹配过滤器的项目自动加入下载队列。
      </p>

      <form onSubmit={onCreate} className="glass-panel grid gap-3 md:grid-cols-[1fr_1fr_1fr_auto]">
        <input
          required
          className="input-base"
          placeholder="名称(显示用)"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          required
          className="input-base"
          placeholder="RSS 地址"
          value={feed}
          onChange={(e) => setFeed(e.target.value)}
        />
        <input
          className="input-base"
          placeholder="过滤器(正则,可选)"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <button type="submit" className="neon-button">
          <Plus size={16} /> 添加
        </button>
      </form>

      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && items.length === 0 && <p className="text-slate-400">暂无订阅。</p>}

      {items.length > 0 && (
        <div className="glass-panel">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-slate-500">
              <tr>
                <th className="py-2">名称</th>
                <th>RSS</th>
                <th>过滤器</th>
                <th>最近运行</th>
                <th className="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id} className="border-t border-white/5">
                  <td className="py-2 text-white">{s.name}</td>
                  <td className="max-w-md truncate text-slate-300" title={s.feed_url}>
                    {s.feed_url}
                  </td>
                  <td className="text-slate-300">{s.filter || '—'}</td>
                  <td className="text-slate-500">
                    {s.last_run_at ? new Date(s.last_run_at).toLocaleString() : '—'}
                  </td>
                  <td className="space-x-2 py-2 text-right">
                    <button
                      className="rounded border border-primary-400/40 px-2 py-1 text-xs text-primary-400 hover:bg-primary-400/10"
                      onClick={async () => {
                        const r = await subscriptionsAPI.runNow(s.id)
                        toast.success(`已加入 ${r.queued} 项`)
                      }}
                    >
                      <Play size={12} />
                    </button>
                    <button
                      className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                      onClick={async () => {
                        if (!confirm(`删除订阅「${s.name}」?`)) return
                        await subscriptionsAPI.remove(s.id)
                        toast.success('已删除')
                        await refresh()
                      }}
                    >
                      <Trash2 size={12} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
