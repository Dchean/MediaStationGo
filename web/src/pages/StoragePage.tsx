import { useEffect, useState } from 'react'
import { Database, HardDrive, PieChart } from 'lucide-react'

import { storageAPI, type StorageBreakdown } from '../api/storage'

function fmtBytes(n: number): string {
  if (!n) return '0 B'
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
  if (!seconds) return '—'
  const h = Math.floor(seconds / 3600)
  return `${h.toLocaleString()} h`
}

// StoragePage shows disk usage broken down by library and by container.
export function StoragePage() {
  const [data, setData] = useState<StorageBreakdown | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    storageAPI
      .breakdown()
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="text-sand-500">加载中…</p>
  if (!data) return <p className="text-sand-500">无法获取存储数据</p>

  const totalBytes = data.total_bytes || 1

  return (
    <div className="space-y-8">
      <header className="flex items-center gap-3">
        <HardDrive className="h-6 w-6 text-brand-500" />
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">存储</h1>
          <p className="text-sm text-ink-50">
            按媒体库和容器格式统计的磁盘占用,数据来自数据库快照(无须实时扫描磁盘)。
          </p>
        </div>
      </header>

      <section className="grid gap-4 sm:grid-cols-3">
        <Tile icon={<Database size={20} />} label="总占用" value={fmtBytes(data.total_bytes)} />
        <Tile icon={<PieChart size={20} />} label="媒体库" value={`${data.by_library.length}`} />
        <Tile icon={<HardDrive size={20} />} label="累计时长" value={fmtHours(data.total_seconds)} />
      </section>

      <section className="space-y-3">
        <h2 className="font-display text-xl font-semibold text-ink-600">按媒体库</h2>
        <div className="glass-panel">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">名称</th>
                <th>类型</th>
                <th>媒体数</th>
                <th>占用</th>
                <th>占比</th>
              </tr>
            </thead>
            <tbody>
              {data.by_library.map((l) => {
                const pct = (l.total_bytes / totalBytes) * 100
                return (
                  <tr key={l.library_id} className="border-t border-white/5">
                    <td className="py-2 text-ink-600">{l.name}</td>
                    <td className="text-ink-100">{l.type}</td>
                    <td className="text-ink-100">{l.media_count}</td>
                    <td className="text-ink-100">{fmtBytes(l.total_bytes)}</td>
                    <td>
                      <div className="flex items-center gap-2">
                        <div className="h-1 w-24 overflow-hidden rounded bg-white/10">
                          <div
                            className="h-full bg-primary-400"
                            style={{ width: `${pct.toFixed(1)}%` }}
                          />
                        </div>
                        <span className="text-xs text-ink-50">{pct.toFixed(1)}%</span>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="font-display text-xl font-semibold text-ink-600">按容器格式</h2>
        <div className="grid gap-3 sm:grid-cols-2 md:grid-cols-3">
          {data.by_container.map((c) => (
            <div
              key={c.container}
              className="glass-panel flex items-center justify-between !p-4"
            >
              <div>
                <p className="text-xs uppercase tracking-wider text-sand-500">{c.container}</p>
                <p className="font-display text-lg font-semibold text-ink-600">{c.count} 项</p>
              </div>
              <p className="text-sm text-ink-100">{fmtBytes(c.bytes)}</p>
            </div>
          ))}
        </div>
      </section>
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
      <div className="rounded-lg border border-primary-400/40 bg-primary-400/10 p-2 text-brand-500">
        {icon}
      </div>
      <div>
        <p className="text-xs uppercase tracking-wider text-sand-500">{label}</p>
        <p className="font-display text-lg font-semibold text-ink-600">{value}</p>
      </div>
    </div>
  )
}
