import { useEffect, useState } from 'react'
import { Activity, Cpu, Database, Film, HardDrive, Users } from 'lucide-react'

import { statsAPI } from '../api/stats'
import { MediaCard } from '../components/MediaCard'
import type { StatsSnapshot } from '../types'

// fmtBytes is a tiny helper shared by the dashboard cards.
function fmtBytes(n: number): string {
  if (!n || n <= 0) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(2)} ${u[i]}`
}

function fmtHours(seconds: number): string {
  if (!seconds || seconds <= 0) return '—'
  const h = Math.floor(seconds / 3600)
  return `${h.toLocaleString()} h`
}

// StatsPage renders the operator dashboard. Refreshes every 10 s.
export function StatsPage() {
  const [snap, setSnap] = useState<StatsSnapshot | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const tick = () =>
      statsAPI.snapshot().then((s) => {
        if (!cancelled) setSnap(s)
      })
    tick().finally(() => setLoading(false))
    const id = window.setInterval(tick, 10_000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  if (loading) return <p className="text-slate-500">加载中…</p>
  if (!snap) return <p className="text-slate-500">无法获取统计数据</p>

  const memPct =
    snap.hardware.memory_total > 0
      ? (snap.hardware.memory_used / snap.hardware.memory_total) * 100
      : 0
  const diskPct =
    snap.hardware.disk_total > 0
      ? (snap.hardware.disk_used / snap.hardware.disk_total) * 100
      : 0

  return (
    <div className="space-y-8">
      <header>
        <h1 className="font-display text-3xl font-bold text-white">运行状态</h1>
        <p className="text-sm text-slate-400">
          快照时间:{new Date(snap.generated_at).toLocaleString()}
        </p>
      </header>

      <section className="grid gap-4 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
        <Tile icon={<Database size={20} />} label="媒体库" value={snap.libraries.toLocaleString()} />
        <Tile icon={<Film size={20} />} label="媒体总数" value={snap.media_count.toLocaleString()} />
        <Tile icon={<Users size={20} />} label="用户" value={snap.users_count.toLocaleString()} />
        <Tile icon={<HardDrive size={20} />} label="入库容量" value={fmtBytes(snap.total_size_bytes)} />
        <Tile icon={<Activity size={20} />} label="累计时长" value={fmtHours(snap.total_seconds)} />
        <Tile icon={<Cpu size={20} />} label="CPU 占用" value={`${snap.hardware.cpu_percent.toFixed(1)}%`} />
        <Tile icon={<Cpu size={20} />} label="内存占用" value={`${memPct.toFixed(1)}%`} />
        <Tile icon={<HardDrive size={20} />} label="数据盘占用" value={`${diskPct.toFixed(1)}%`} />
      </section>

      <section className="space-y-3">
        <h2 className="font-display text-xl font-semibold text-white">系统</h2>
        <div className="glass-panel grid gap-2 text-sm">
          <Row label="Go 运行时" value={snap.hardware.go_version} />
          <Row label="Goroutines" value={snap.hardware.goroutines.toLocaleString()} />
          <Row
            label="内存"
            value={`${fmtBytes(snap.hardware.memory_used)} / ${fmtBytes(snap.hardware.memory_total)}`}
          />
          <Row
            label="数据盘"
            value={`${fmtBytes(snap.hardware.disk_used)} / ${fmtBytes(snap.hardware.disk_total)}`}
          />
        </div>
      </section>

      {snap.recently_added.length > 0 && (
        <section className="space-y-3">
          <h2 className="font-display text-xl font-semibold text-white">最近入库</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {snap.recently_added.map((m) => (
              <MediaCard key={m.id} media={m} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function Tile({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: string
}) {
  return (
    <div className="glass-panel flex items-center gap-3 !p-4">
      <div className="rounded-lg border border-primary-400/40 bg-primary-400/10 p-2 text-primary-400">
        {icon}
      </div>
      <div>
        <p className="text-xs uppercase tracking-wider text-slate-500">{label}</p>
        <p className="font-display text-lg font-semibold text-white">{value}</p>
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between border-b border-white/5 pb-1 text-sm last:border-0">
      <span className="text-slate-400">{label}</span>
      <span className="font-mono text-white">{value}</span>
    </div>
  )
}
