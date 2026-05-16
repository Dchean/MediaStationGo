import { FormEvent, useEffect, useState } from 'react'
import { Bell, FolderCog, Loader2, Search, Wrench } from 'lucide-react'
import toast from 'react-hot-toast'

import { libraryAPI, mediaAPI } from '../api/library'
import { toolsAPI } from '../api/tools'
import type { Library, Media } from '../types'

// ToolsPage gathers admin-only one-off operations that don't belong on a
// dedicated screen of their own:
//
//   - Organize one media item or an entire library: renames + moves files
//     into the canonical layout (`Library/Year/Title (Year)/...`).
//   - Send a test notification through every configured channel.
//
// The original Vue version surfaces these as tabs inside SettingsView; here
// we co-locate them on a single admin tools page.
export function ToolsPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-400/10 text-violet-400">
          <Wrench size={20} />
        </div>
        <div>
          <h1 className="font-display text-3xl font-bold text-white">运维工具</h1>
          <p className="text-sm text-slate-400">整理媒体文件 · 测试通知渠道</p>
        </div>
      </div>

      <OrganizePanel />
      <NotifyPanel />
    </div>
  )
}

function OrganizePanel() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [libraryID, setLibraryID] = useState('')
  const [running, setRunning] = useState(false)

  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [results, setResults] = useState<Media[]>([])
  const [busyID, setBusyID] = useState<string | null>(null)

  useEffect(() => {
    libraryAPI.list().then(setLibraries).catch(() => undefined)
  }, [])

  const onOrganizeLibrary = async (e: FormEvent) => {
    e.preventDefault()
    if (!libraryID) return
    setRunning(true)
    try {
      await toolsAPI.organizeLibrary(libraryID)
      toast.success('已触发媒体库整理')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '整理失败'
      toast.error(msg)
    } finally {
      setRunning(false)
    }
  }

  const doSearch = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!query.trim()) return
    setSearching(true)
    try {
      const r = await mediaAPI.search(query.trim(), 20)
      setResults(r.items ?? [])
    } catch {
      toast.error('搜索失败')
    } finally {
      setSearching(false)
    }
  }

  const onOrganizeOne = async (m: Media) => {
    setBusyID(m.id)
    try {
      const r = await toolsAPI.organizeMedia(m.id)
      toast.success(`已移动到 ${r.path}`)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '整理失败'
      toast.error(msg)
    } finally {
      setBusyID(null)
    }
  }

  return (
    <section className="glass-panel space-y-5">
      <div className="flex items-center gap-2">
        <FolderCog size={18} className="text-primary-400" />
        <h2 className="font-display text-lg font-semibold text-white">整理 &amp; 重命名</h2>
      </div>
      <p className="text-xs text-slate-500">
        将媒体文件按 <code className="rounded bg-white/5 px-1">媒体库/年份/标题 (年份)/标题.ext</code>{' '}
        的规范布局移动并重命名,确保已先完成刮削。
      </p>

      <form onSubmit={onOrganizeLibrary} className="flex flex-wrap gap-2">
        <select
          required
          className="input-base flex-1 min-w-[200px]"
          value={libraryID}
          onChange={(e) => setLibraryID(e.target.value)}
        >
          <option value="" disabled>
            选择要整理的媒体库
          </option>
          {libraries.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name} ({l.type})
            </option>
          ))}
        </select>
        <button type="submit" disabled={running || !libraryID} className="neon-button">
          {running ? <Loader2 size={16} className="animate-spin" /> : <FolderCog size={16} />}
          整理整个库
        </button>
      </form>

      <div className="border-t border-white/5 pt-4">
        <form onSubmit={doSearch} className="flex gap-2">
          <input
            className="input-base flex-1"
            placeholder="或搜索单个媒体进行整理…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button type="submit" disabled={searching} className="neon-button">
            <Search size={16} /> {searching ? '搜索中…' : '搜索'}
          </button>
        </form>

        {results.length > 0 && (
          <ul className="mt-4 space-y-2">
            {results.map((m) => (
              <li
                key={m.id}
                className="flex items-center justify-between gap-3 rounded-lg border border-white/5 bg-white/5 px-3 py-2"
              >
                <div className="min-w-0">
                  <div className="truncate text-sm text-white">{m.title}</div>
                  <div className="truncate text-xs text-slate-400">{m.path}</div>
                </div>
                <button
                  onClick={() => onOrganizeOne(m)}
                  disabled={busyID === m.id}
                  className="neon-button !px-3 !py-1 text-xs"
                >
                  {busyID === m.id ? (
                    <Loader2 size={12} className="animate-spin" />
                  ) : (
                    <FolderCog size={12} />
                  )}
                  整理
                </button>
              </li>
            ))}
          </ul>
        )}

        {!searching && query && results.length === 0 && (
          <p className="mt-3 text-sm text-slate-400">未找到匹配的媒体。</p>
        )}
      </div>
    </section>
  )
}

function NotifyPanel() {
  const [title, setTitle] = useState('MediaStation 测试通知')
  const [body, setBody] = useState('如果你收到这条消息,说明通知渠道工作正常。')
  const [sending, setSending] = useState(false)

  const onSend = async (e: FormEvent) => {
    e.preventDefault()
    if (!title.trim() || !body.trim()) return
    setSending(true)
    try {
      await toolsAPI.notifyTest(title.trim(), body.trim())
      toast.success('已派发到所有已启用通道')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '发送失败'
      toast.error(msg)
    } finally {
      setSending(false)
    }
  }

  return (
    <section className="glass-panel space-y-4">
      <div className="flex items-center gap-2">
        <Bell size={18} className="text-amber-300" />
        <h2 className="font-display text-lg font-semibold text-white">通知渠道测试</h2>
      </div>
      <p className="text-xs text-slate-500">
        会向所有已配置的通知渠道(Telegram / Bark / Webhook 等)发送一条测试消息。
      </p>
      <form onSubmit={onSend} className="space-y-3">
        <Field label="标题">
          <input
            required
            className="input-base"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </Field>
        <Field label="内容">
          <textarea
            required
            rows={3}
            className="input-base resize-none"
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={sending} className="neon-button">
          {sending ? <Loader2 size={16} className="animate-spin" /> : <Bell size={16} />}
          发送测试通知
        </button>
      </form>
    </section>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm text-slate-300">{label}</span>
      {children}
    </label>
  )
}
