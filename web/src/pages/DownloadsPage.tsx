import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { ArrowDown, ArrowUp, Download, Film, HardDrive, Rss, ShieldCheck, Trash2 } from 'lucide-react'

import { imageURL } from '../api/client'
import { downloadsAPI } from '../api/downloads'
import { useAuthStore } from '../stores/auth'
import type { DownloadTask, QBitTorrent } from '../types'

type DownloadCardItem = {
  id?: string
  hash?: string
  title: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  save_path?: string
  status?: string
  state?: string
  progress: number
  dlspeed?: number
  upspeed?: number
  num_seeds?: number
  num_leechs?: number
  size?: number
  downloaded?: number
  created_at?: string
}

function fmtBytes(n?: number): string {
  if (!n || n <= 0) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${u[i]}`
}

function fmtSpeed(n?: number): string {
  return `${fmtBytes(n)}/s`
}

function pct(progress?: number): number {
  if (!Number.isFinite(progress)) return 0
  return Math.min(100, Math.max(0, Math.round((progress ?? 0) * 1000) / 10))
}

function stateLabel(item: DownloadCardItem): string {
  const state = (item.state || item.status || 'queued').toLowerCase()
  if (state.includes('down') || state.includes('meta')) return '下载中'
  if (state.includes('up') || state.includes('seed')) return '做种中'
  if (state.includes('pause')) return '已暂停'
  if (state.includes('error')) return '出错'
  if (state.includes('complete') || pct(item.progress) >= 100) return '已完成'
  if (state.includes('queue')) return '排队中'
  return item.state || item.status || '等待中'
}

function statusTone(item: DownloadCardItem): string {
  const state = stateLabel(item)
  if (state === '已完成' || state === '做种中') return 'bg-emerald-50 text-emerald-600'
  if (state === '出错') return 'bg-red-50 text-red-500'
  if (state === '已暂停') return 'bg-amber-50 text-amber-600'
  return 'bg-primary-400/10 text-brand-500'
}

function toLiveCard(t: QBitTorrent): DownloadCardItem {
  return {
    hash: t.hash,
    title: t.title || t.name || '下载任务',
    poster_url: t.poster_url,
    backdrop_url: t.backdrop_url,
    overview: t.overview,
    save_path: t.save_path,
    state: t.state,
    progress: t.progress,
    dlspeed: t.dlspeed,
    upspeed: t.upspeed,
    num_seeds: t.num_seeds,
    num_leechs: t.num_leechs,
    size: t.size,
    downloaded: t.downloaded,
  }
}

function toTaskCard(t: DownloadTask): DownloadCardItem {
  return {
    id: t.id,
    title: t.title || '下载任务',
    poster_url: t.poster_url,
    backdrop_url: t.backdrop_url,
    overview: t.overview,
    save_path: t.save_path,
    status: t.status,
    state: t.state,
    progress: t.progress,
    dlspeed: t.dlspeed,
    upspeed: t.upspeed,
    num_seeds: t.num_seeds,
    num_leechs: t.num_leechs,
    size: t.size,
    downloaded: t.downloaded,
    created_at: t.created_at,
  }
}

function DownloadCard({
  item,
  removable,
  onRemove,
}: {
  item: DownloadCardItem
  removable?: boolean
  onRemove?: () => Promise<void>
}) {
  const progress = pct(item.progress)
  const visual = item.poster_url || item.backdrop_url
  const downloaded = item.downloaded || (item.size ? Math.round(item.size * (item.progress || 0)) : 0)

  return (
    <article className="group overflow-hidden rounded-3xl border border-white/70 bg-white shadow-sm transition hover:-translate-y-1 hover:shadow-xl">
      <div className="relative flex gap-4 p-4">
        <div className="relative h-40 w-28 flex-shrink-0 overflow-hidden rounded-2xl bg-gradient-to-br from-primary-400/15 via-white to-surface-200 shadow-inner">
          {visual ? (
            <img
              src={imageURL(visual)}
              alt={item.title}
              className="h-full w-full object-cover"
              referrerPolicy="no-referrer"
            />
          ) : (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 px-3 text-center text-xs font-semibold text-brand-500">
              <Film size={24} />
              <span className="line-clamp-3">{item.title}</span>
            </div>
          )}
          <span className={`absolute left-2 top-2 rounded-full px-2 py-0.5 text-[10px] font-semibold ${statusTone(item)}`}>
            {stateLabel(item)}
          </span>
        </div>

        <div className="min-w-0 flex-1 space-y-3">
          <div>
            <h2 className="line-clamp-2 font-display text-lg font-semibold leading-snug text-ink-600" title={item.title}>
              {item.title}
            </h2>
            <p className="mt-1 line-clamp-2 text-xs leading-5 text-ink-50">
              {item.overview || item.save_path || '已隐藏原始种子 URL，避免泄露私有 Token。'}
            </p>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between text-xs font-semibold text-ink-100">
              <span>进度 {progress.toFixed(1)}%</span>
              <span>{fmtBytes(downloaded)} / {fmtBytes(item.size)}</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-gray-100">
              <div
                className="h-full rounded-full bg-gradient-to-r from-primary-400 to-brand-500 transition-all"
                style={{ width: `${progress}%` }}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-2 text-xs text-ink-100">
            <div className="rounded-2xl bg-gray-50 px-3 py-2">
              <ArrowDown size={13} className="mr-1 inline text-brand-500" />
              {fmtSpeed(item.dlspeed)}
            </div>
            <div className="rounded-2xl bg-gray-50 px-3 py-2">
              <ArrowUp size={13} className="mr-1 inline text-brand-500" />
              {fmtSpeed(item.upspeed)}
            </div>
            <div className="rounded-2xl bg-gray-50 px-3 py-2">
              <Rss size={13} className="mr-1 inline text-brand-500" />
              {item.num_seeds ?? 0} / {item.num_leechs ?? 0}
            </div>
            <div className="rounded-2xl bg-gray-50 px-3 py-2">
              <HardDrive size={13} className="mr-1 inline text-brand-500" />
              {fmtBytes(item.size)}
            </div>
          </div>

          <div className="flex items-center justify-between gap-2 text-xs text-ink-50">
            <span className="truncate" title={item.save_path || ''}>
              {item.save_path || '默认下载目录'}
            </span>
            {item.created_at && <span>{new Date(item.created_at).toLocaleString()}</span>}
          </div>
        </div>
      </div>

      {removable && onRemove && (
        <div className="flex justify-end border-t border-gray-100 bg-gray-50/70 px-4 py-3">
          <button
            className="rounded-xl border border-red-400/40 bg-white px-3 py-1.5 text-xs font-semibold text-red-400 hover:bg-red-400/10"
            onClick={() => void onRemove()}
          >
            <Trash2 size={13} className="mr-1 inline" />
            删除任务
          </button>
        </div>
      )}
    </article>
  )
}

export function DownloadsPage() {
  const role = useAuthStore((s) => s.user?.role)
  const [tasks, setTasks] = useState<DownloadTask[]>([])
  const [torrents, setTorrents] = useState<QBitTorrent[] | null>(null)
  const [url, setURL] = useState('')
  const [savePath, setSavePath] = useState('')

  const refresh = () =>
    downloadsAPI.list().then((d) => {
      setTasks(d.tasks)
      setTorrents(d.torrents)
    })

  useEffect(() => {
    void refresh().catch(() => undefined)
    const id = window.setInterval(() => void refresh().catch(() => undefined), 5_000)
    return () => window.clearInterval(id)
  }, [])

  const onAdd = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await downloadsAPI.add(url, savePath)
      toast.success('已加入下载队列')
      setURL('')
      setSavePath('')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '提交失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h1 className="font-display text-3xl font-bold text-ink-600">下载管理</h1>
        <p className="flex items-center gap-2 text-sm text-ink-50">
          <ShieldCheck size={16} className="text-brand-500" />
          页面仅展示安全标题、海报和进度信息，不再暴露种子原始 URL 或私有 Token。
        </p>
      </header>

      <form onSubmit={onAdd} className="glass-panel grid gap-3 md:grid-cols-[1fr_1fr_auto]">
        <input
          required
          className="input-base md:col-span-2"
          placeholder="磁力链接 / .torrent URL（提交后不会在页面公开显示）"
          value={url}
          onChange={(e) => setURL(e.target.value)}
        />
        <input
          className="input-base"
          placeholder="保存路径 (可选)"
          value={savePath}
          onChange={(e) => setSavePath(e.target.value)}
        />
        <button type="submit" className="neon-button md:col-span-3">
          <Download size={16} /> 添加
        </button>
      </form>

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-display text-xl font-semibold text-ink-600">当前下载</h2>
          <span className="text-xs text-ink-50">每 5 秒自动刷新</span>
        </div>
        {torrents === null && (
          <div className="glass-panel text-sand-500">
            尚未连接到下载器 — 请到{' '}
            <a href="/download-clients" className="text-brand-500 hover:underline">
              下载器
            </a>{' '}
            页面添加并测试连接。
          </div>
        )}
        {torrents && torrents.length === 0 && (
          <div className="glass-panel text-sand-500">暂无运行中任务。</div>
        )}
        {torrents && torrents.length > 0 && (
          <div className="grid gap-5 lg:grid-cols-2 2xl:grid-cols-3">
            {torrents.map((torrent) => (
              <DownloadCard
                key={torrent.hash}
                item={toLiveCard(torrent)}
                removable={role === 'admin'}
                onRemove={async () => {
                  if (!confirm(`删除「${torrent.title || torrent.name}」?`)) return
                  await downloadsAPI.remove(torrent.hash, false)
                  toast.success('已删除任务')
                  await refresh()
                }}
              />
            ))}
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="font-display text-xl font-semibold text-ink-600">下载历史</h2>
        {tasks.length === 0 ? (
          <div className="glass-panel text-sand-500">暂无历史下载。</div>
        ) : (
          <div className="grid gap-5 lg:grid-cols-2 2xl:grid-cols-3">
            {tasks.map((task) => (
              <DownloadCard key={task.id} item={toTaskCard(task)} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
