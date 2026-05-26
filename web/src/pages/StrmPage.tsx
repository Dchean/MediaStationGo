import { FormEvent, useEffect, useState } from 'react'
import { Link as LinkIcon, Plus, Search, Trash2 } from 'lucide-react'
import toast from 'react-hot-toast'

import { libraryAPI, mediaAPI } from '../api/library'
import { strmAPI } from '../api/strm'
import type { Library, Media } from '../types'

// StrmPage exposes the URL-as-file admin tooling backed by the Go server:
//   - import a brand-new media row directly from a (library, title, url)
//     tuple — useful for streaming-only entries with no on-disk file.
//   - search existing media and attach / detach a STRM URL so the player
//     issues a 302 redirect to the remote source instead of opening a
//     local file.
export function StrmPage() {
  const [libraries, setLibraries] = useState<Library[]>([])

  // Import form state
  const [libraryID, setLibraryID] = useState('')
  const [title, setTitle] = useState('')
  const [url, setURL] = useState('')
  const [importing, setImporting] = useState(false)

  // Search + attach state
  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [results, setResults] = useState<Media[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})

  useEffect(() => {
    libraryAPI.list().then(setLibraries).catch(() => undefined)
  }, [])

  // Default the import library to the first available one once loaded.
  useEffect(() => {
    if (!libraryID && libraries[0]) setLibraryID(libraries[0].id)
  }, [libraries, libraryID])

  const onImport = async (e: FormEvent) => {
    e.preventDefault()
    if (!libraryID || !title.trim() || !url.trim()) return
    if (!/^https?:\/\//i.test(url.trim())) {
      toast.error('URL 必须以 http:// 或 https:// 开头')
      return
    }
    setImporting(true)
    try {
      await strmAPI.importURL(libraryID, title.trim(), url.trim())
      toast.success(`已导入「${title.trim()}」`)
      setTitle('')
      setURL('')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '导入失败'
      toast.error(msg)
    } finally {
      setImporting(false)
    }
  }

  const doSearch = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!query.trim()) return
    setSearching(true)
    try {
      const r = await mediaAPI.search(query.trim(), 30)
      setResults(r.items ?? [])
    } catch {
      toast.error('搜索失败')
    } finally {
      setSearching(false)
    }
  }

  const onAttach = async (m: Media) => {
    const next = (drafts[m.id] ?? '').trim()
    if (!next) return
    if (!/^https?:\/\//i.test(next)) {
      toast.error('URL 必须以 http:// 或 https:// 开头')
      return
    }
    try {
      await strmAPI.set(m.id, next)
      toast.success('已设置 STRM URL')
      // Optimistic update so the user sees the new state without a re-search.
      setResults((rs) =>
        rs.map((x) => (x.id === m.id ? ({ ...x, container: 'strm' } as Media) : x)),
      )
      setDrafts((d) => ({ ...d, [m.id]: '' }))
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '设置失败'
      toast.error(msg)
    }
  }

  const onDetach = async (m: Media) => {
    if (!confirm(`清除「${m.title}」的 STRM URL?`)) return
    try {
      await strmAPI.clear(m.id)
      toast.success('已清除')
      setResults((rs) =>
        rs.map((x) =>
          x.id === m.id ? ({ ...x, container: x.container === 'strm' ? '' : x.container } as Media) : x,
        ),
      )
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '清除失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-400/10 text-amber-400">
          <LinkIcon size={20} />
        </div>
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">STRM 管理</h1>
          <p className="text-sm text-ink-50">
            将外部 HTTP / WebDAV / Alist 直链以"虚拟文件"形式纳入媒体库
          </p>
        </div>
      </div>

      {/* Import a new STRM-only entry. */}
      <section className="glass-panel space-y-4">
        <h2 className="font-display text-lg font-semibold text-ink-600">导入 STRM 条目</h2>
        <form onSubmit={onImport} className="grid gap-3 md:grid-cols-4">
          <select
            required
            className="input-base"
            value={libraryID}
            onChange={(e) => setLibraryID(e.target.value)}
          >
            <option value="" disabled>
              选择媒体库
            </option>
            {libraries.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name} ({l.type})
              </option>
            ))}
          </select>
          <input
            required
            className="input-base"
            placeholder="标题"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
          <input
            required
            className="input-base md:col-span-2"
            placeholder="https://example.com/movie.mp4"
            value={url}
            onChange={(e) => setURL(e.target.value)}
          />
          <button type="submit" disabled={importing} className="neon-button md:col-span-4">
            <Plus size={16} /> {importing ? '导入中…' : '导入'}
          </button>
        </form>
        <p className="text-xs text-sand-500">
          导入后会创建一条 container=strm 的媒体记录,播放时会 302 重定向到该 URL。
        </p>
      </section>

      {/* Attach / detach STRM URL on existing media. */}
      <section className="glass-panel space-y-4">
        <h2 className="font-display text-lg font-semibold text-ink-600">附加 STRM URL 到已有媒体</h2>
        <form onSubmit={doSearch} className="flex gap-2">
          <input
            className="input-base flex-1"
            placeholder="搜索媒体标题…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button type="submit" disabled={searching} className="neon-button">
            <Search size={16} /> {searching ? '搜索中…' : '搜索'}
          </button>
        </form>

        {results.length > 0 && (
          <div className="space-y-3">
            {results.map((m) => {
              const isStrm = m.container === 'strm'
              return (
                <div
                  key={m.id}
                  className="rounded-xl border border-white/5 bg-white/5 p-4 space-y-3"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate font-medium text-ink-600">{m.title}</div>
                      <div className="truncate text-xs text-ink-50">
                        {m.year > 0 && `${m.year} · `}
                        {m.container || '本地文件'}
                      </div>
                      {isStrm && (
                        <div className="mt-1 break-all rounded bg-emerald-400/10 px-2 py-0.5 text-xs text-emerald-300">
                          已设置 STRM
                        </div>
                      )}
                    </div>
                    {isStrm && (
                      <button
                        onClick={() => onDetach(m)}
                        className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                      >
                        <Trash2 size={12} className="inline" /> 清除
                      </button>
                    )}
                  </div>
                  <div className="flex gap-2">
                    <input
                      className="input-base flex-1"
                      placeholder="https://example.com/stream.m3u8"
                      value={drafts[m.id] ?? ''}
                      onChange={(e) =>
                        setDrafts((d) => ({ ...d, [m.id]: e.target.value }))
                      }
                    />
                    <button
                      onClick={() => onAttach(m)}
                      disabled={!(drafts[m.id] ?? '').trim()}
                      className="neon-button"
                    >
                      {isStrm ? '替换 URL' : '设置'}
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        )}

        {!searching && query && results.length === 0 && (
          <p className="text-sm text-ink-50">未找到匹配的媒体。</p>
        )}
      </section>
    </div>
  )
}
