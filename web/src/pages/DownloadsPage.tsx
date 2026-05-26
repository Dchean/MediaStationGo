import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Download, Trash2 } from 'lucide-react'

import { downloadsAPI } from '../api/downloads'
import { useAuthStore } from '../stores/auth'
import type { DownloadTask, QBitTorrent } from '../types'

function fmtBytes(n: number): string {
  if (!n || n <= 0) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(2)} ${u[i]}`
}

function fmtSpeed(n: number): string {
  return `${fmtBytes(n)}/s`
}

// DownloadsPage: shows running torrents (live) + persisted task rows.
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
      <h1 className="font-display text-3xl font-bold text-ink-600">下载</h1>

      <form onSubmit={onAdd} className="glass-panel grid gap-3 md:grid-cols-[1fr_1fr_auto]">
        <input
          required
          className="input-base md:col-span-2"
          placeholder="磁力链接 / .torrent URL"
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

      <section className="glass-panel">
        <h2 className="mb-3 font-display text-lg font-semibold text-ink-600">实时状态</h2>
        {torrents === null && (
          <p className="text-sand-500">
            尚未连接到下载器 — 请到{' '}
            <a href="/download-clients" className="text-brand-500 hover:underline">
              下载器
            </a>{' '}
            页面添加并测试连接（qBittorrent / Aria2 / Transmission）。
          </p>
        )}
        {torrents && torrents.length === 0 && <p className="text-sand-500">暂无运行中任务。</p>}
        {torrents && torrents.length > 0 && (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">名称</th>
                <th>状态</th>
                <th>进度</th>
                <th>速度</th>
                <th>体积</th>
                {role === 'admin' && <th />}
              </tr>
            </thead>
            <tbody>
              {torrents.map((t) => (
                <tr key={t.hash} className="border-t border-white/5 align-top">
                  <td className="max-w-md truncate py-2 text-ink-600" title={t.name}>
                    {t.name}
                  </td>
                  <td className="text-ink-100">{t.state}</td>
                  <td className="text-ink-100">
                    <div className="flex items-center gap-2">
                      <div className="h-1 w-24 overflow-hidden rounded bg-white/10">
                        <div
                          className="h-full bg-primary-400"
                          style={{ width: `${Math.round(t.progress * 100)}%` }}
                        />
                      </div>
                      {(t.progress * 100).toFixed(1)}%
                    </div>
                  </td>
                  <td className="text-ink-100">
                    ↓ {fmtSpeed(t.dlspeed)} / ↑ {fmtSpeed(t.upspeed)}
                  </td>
                  <td className="text-ink-100">{fmtBytes(t.size)}</td>
                  {role === 'admin' && (
                    <td className="py-2 text-right">
                      <button
                        className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                        onClick={async () => {
                          if (!confirm(`删除「${t.name}」?`)) return
                          await downloadsAPI.remove(t.hash, false)
                          toast.success('已删除任务')
                          await refresh()
                        }}
                      >
                        <Trash2 size={12} />
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {tasks.length > 0 && (
        <section className="glass-panel">
          <h2 className="mb-3 font-display text-lg font-semibold text-ink-600">历史记录</h2>
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">来源</th>
                <th>URL</th>
                <th>保存路径</th>
                <th>时间</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((t) => (
                <tr key={t.id} className="border-t border-white/5">
                  <td className="py-2 text-ink-100">{t.source}</td>
                  <td className="max-w-md truncate text-ink-100" title={t.url}>
                    {t.url}
                  </td>
                  <td className="text-ink-100">{t.save_path || '—'}</td>
                  <td className="text-sand-500">
                    {new Date(t.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}
