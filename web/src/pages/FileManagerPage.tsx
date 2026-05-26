import { useCallback, useEffect, useState } from 'react'
import { ChevronUp, FileVideo, Folder, FolderOpen, Home } from 'lucide-react'

import { filesAPI, type FileEntry, type FileListing } from '../api/files'

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(1)} ${u[i]}`
}

// FileManagerPage browses the server's filesystem within the allowed
// roots so the operator can pick library paths visually.
export function FileManagerPage() {
  const [path, setPath] = useState('')
  const [data, setData] = useState<FileListing | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(() => {
    setLoading(true)
    setError('')
    filesAPI
      .list(path)
      .then(setData)
      .catch((err: unknown) => {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '加载失败'
        setError(msg)
      })
      .finally(() => setLoading(false))
  }, [path])

  useEffect(() => {
    refresh()
  }, [refresh])

  const enter = (e: FileEntry) => {
    if (e.is_dir) setPath(e.path)
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="font-display text-3xl font-bold text-ink-600">文件浏览器</h1>
        <p className="text-sm text-ink-50">
          只允许访问已配置的根目录(媒体库 + data + cache)。
        </p>
      </header>

      <div className="flex flex-wrap gap-2">
        <button
          className="neon-button !px-3 !py-1 !text-xs"
          onClick={() => setPath('')}
          title="返回根列表"
        >
          <Home size={14} /> 根
        </button>
        {data?.parent && (
          <button
            className="neon-button !px-3 !py-1 !text-xs"
            onClick={() => setPath(data.parent ?? '')}
          >
            <ChevronUp size={14} /> 上一级
          </button>
        )}
        {data?.path && (
          <span className="rounded border border-white/10 bg-white/5 px-2 py-1 font-mono text-xs text-ink-100">
            {data.path}
          </span>
        )}
      </div>

      {loading && <p className="text-sand-500">加载中…</p>}
      {error && <div className="glass-panel !border-red-400/40 text-red-400">{error}</div>}

      {!loading && data && !data.entries && data.roots && (
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {data.roots.map((r) => (
            <button
              key={r.path}
              onClick={() => setPath(r.path)}
              className="glass-panel flex items-center gap-3 text-left transition hover:border-primary-400/40"
            >
              <FolderOpen size={20} className="text-brand-500" />
              <div>
                <p className="font-mono text-sm text-ink-600">{r.label}</p>
                <p className="font-mono text-xs text-ink-50">{r.path}</p>
              </div>
            </button>
          ))}
        </div>
      )}

      {!loading && data?.entries && data.entries.length > 0 && (
        <div className="glass-panel">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="py-2">名称</th>
                <th>大小</th>
                <th>修改时间</th>
              </tr>
            </thead>
            <tbody>
              {data.entries.map((e) => (
                <tr
                  key={e.path}
                  className="cursor-pointer border-t border-white/5 transition hover:bg-white/5"
                  onClick={() => enter(e)}
                  title={e.path}
                >
                  <td className="flex items-center gap-2 py-2 text-ink-600">
                    {e.is_dir ? (
                      <Folder size={16} className="text-brand-500" />
                    ) : (
                      <FileVideo size={16} className="text-ink-50" />
                    )}
                    {e.name}
                  </td>
                  <td className="text-ink-100">{e.is_dir ? '—' : fmtBytes(e.size)}</td>
                  <td className="text-sand-500">
                    {new Date(e.modified * 1000).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!loading && data?.entries && data.entries.length === 0 && (
        <p className="text-ink-50">空目录。</p>
      )}
    </div>
  )
}
