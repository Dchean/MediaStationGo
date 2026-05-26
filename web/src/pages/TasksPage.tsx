import { useEffect, useState } from 'react'
import { Activity } from 'lucide-react'

import { tasksAPI, type TasksSnapshot } from '../api/tasks'

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

// TasksPage shows everything the backend is doing right now: ffmpeg
// transcodes + qBittorrent downloads. Refreshes every 3 s.
export function TasksPage() {
  const [snap, setSnap] = useState<TasksSnapshot | null>(null)

  useEffect(() => {
    let cancelled = false
    const tick = () =>
      tasksAPI.snapshot().then((s) => {
        if (!cancelled) setSnap(s)
      })
    void tick()
    const id = window.setInterval(tick, 3_000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  if (!snap) return <p className="text-sand-500">加载中…</p>

  const torrents = snap.torrents ?? []

  return (
    <div className="space-y-8">
      <header className="flex items-center gap-3">
        <Activity className="h-6 w-6 text-brand-500" />
        <h1 className="font-display text-3xl font-bold text-ink-600">实时任务</h1>
      </header>

      <section className="glass-panel">
        <h2 className="mb-3 font-display text-lg font-semibold text-ink-600">转码任务</h2>
        {snap.transcodes.length === 0 && <p className="text-sand-500">暂无运行中转码。</p>}
        {snap.transcodes.length > 0 && (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">媒体 ID</th>
                <th>编码器</th>
                <th>开始时间</th>
                <th>就绪</th>
              </tr>
            </thead>
            <tbody>
              {snap.transcodes.map((t) => (
                <tr key={t.media_id} className="border-t border-white/5">
                  <td className="py-2 font-mono text-xs text-ink-600">{t.media_id}</td>
                  <td className="text-ink-100">{t.encoder || 'libx264'}</td>
                  <td className="text-ink-100">{new Date(t.started_at).toLocaleTimeString()}</td>
                  <td>
                    {t.playlist_ok ? (
                      <span className="rounded border border-emerald-400/40 px-1.5 py-0.5 text-xs text-emerald-400">
                        ready
                      </span>
                    ) : (
                      <span className="rounded border border-yellow-400/40 px-1.5 py-0.5 text-xs text-yellow-400">
                        starting
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="glass-panel">
        <h2 className="mb-3 font-display text-lg font-semibold text-ink-600">下载任务</h2>
        {torrents.length === 0 && <p className="text-sand-500">暂无运行中下载。</p>}
        {torrents.length > 0 && (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">名称</th>
                <th>状态</th>
                <th>进度</th>
                <th>体积</th>
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
                  <td className="text-ink-100">{fmtBytes(t.size)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  )
}
