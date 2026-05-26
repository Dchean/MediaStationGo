import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { RotateCcw, Trash2 } from 'lucide-react'

import { recycleAPI } from '../api/recycle'
import type { Media } from '../types'

export function RecycleBinPage() {
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(true)

  const refresh = () =>
    recycleAPI
      .list()
      .then(setItems)
      .finally(() => setLoading(false))

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-ink-600">回收站</h1>
      <p className="text-sm text-ink-50">
        软删除的媒体保留在数据库中,可以恢复。彻底删除不会移除磁盘上的文件,只会从数据库清除条目。
      </p>

      {loading && <p className="text-sand-500">加载中…</p>}
      {!loading && items.length === 0 && <p className="text-ink-50">回收站为空。</p>}

      {items.length > 0 && (
        <div className="glass-panel">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">标题</th>
                <th>路径</th>
                <th className="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((m) => (
                <tr key={m.id} className="border-t border-white/5">
                  <td className="py-2 text-ink-600">{m.title}</td>
                  <td className="max-w-md truncate text-ink-50" title={m.path}>
                    {m.path}
                  </td>
                  <td className="space-x-2 py-2 text-right">
                    <button
                      className="rounded border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10"
                      onClick={async () => {
                        await recycleAPI.restore(m.id)
                        toast.success('已恢复')
                        await refresh()
                      }}
                    >
                      <RotateCcw size={12} />
                    </button>
                    <button
                      className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                      onClick={async () => {
                        if (!confirm(`彻底删除「${m.title}」? (磁盘文件保留)`)) return
                        await recycleAPI.purge(m.id)
                        toast.success('已彻底删除')
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
