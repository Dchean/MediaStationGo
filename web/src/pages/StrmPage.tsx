import { FormEvent, useEffect, useState } from 'react'
import { Link as LinkIcon, Loader2, Plus, Save, Search, Trash2, Wand2 } from 'lucide-react'
import toast from 'react-hot-toast'

import { adminAPI } from '../api/admin'
import { libraryAPI, mediaAPI } from '../api/library'
import { strmAPI, type GenerateSTRMResult } from '../api/strm'
import { confirmAction } from '../components/ConfirmDialog'
import type { Library, Media } from '../types'

type CloudPlaybackMode = 'strm' | 'redirect_proxy'

function currentOrigin() {
  if (typeof window === 'undefined') return ''
  return window.location.origin.replace(/\/+$/, '')
}

function isLocalPlaybackBase(raw: string) {
  try {
    const u = new URL(raw)
    const host = u.hostname.toLowerCase()
    return host === 'localhost' || host === '127.0.0.1' || host === '::1'
  } catch {
    return false
  }
}

function preferredSTRMBaseURL(saved: string) {
  const current = currentOrigin()
  const trimmed = saved.trim().replace(/\/+$/, '')
  if (!trimmed) return current
  if (current && isLocalPlaybackBase(trimmed) && trimmed !== current) return current
  return trimmed
}

// StrmPage exposes the URL-as-file admin tooling backed by the Go server:
//   - import a brand-new media row directly from a (library, title, url)
//     tuple — useful for streaming-only entries with no on-disk file.
//   - search existing media and attach / detach a STRM URL so the player
//     issues a 302 redirect to the remote source instead of opening a
//     local file.
export function StrmPage() {
  const [libraries, setLibraries] = useState<Library[]>([])

  // Auto-generate state
  const [generateLibraryID, setGenerateLibraryID] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [outputDir, setOutputDir] = useState('')
  const [cloudPlaybackMode, setCloudPlaybackMode] = useState<CloudPlaybackMode>('redirect_proxy')
  const [strmPlaybackEnabled, setStrmPlaybackEnabled] = useState(false)
  const [redirectProxyEnabled, setRedirectProxyEnabled] = useState(true)
  const [autoGenerate, setAutoGenerate] = useState(false)
  const [savingSettings, setSavingSettings] = useState(false)
  const [overwrite, setOverwrite] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [generateResult, setGenerateResult] = useState<GenerateSTRMResult | null>(null)

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
    adminAPI
      .listSettings()
      .then((rows) => {
        const settings = Object.fromEntries(rows.map((row) => [row.key, row.value]))
        setBaseURL(preferredSTRMBaseURL(settings['strm.base_url'] || settings['app.server_url'] || ''))
        setOutputDir(settings['strm.output_dir'] || '')
        const mode = settings['cloud.playback_mode']
        const nextMode =
          mode === 'strm' || mode === 'redirect_proxy'
            ? mode
            : settings['strm.enabled'] === 'true'
              ? 'strm'
              : 'redirect_proxy'
        setCloudPlaybackMode(nextMode)
        setStrmPlaybackEnabled(
          settings['cloud.playback_strm_enabled'] != null
            ? settings['cloud.playback_strm_enabled'] === 'true'
            : settings['strm.enabled'] === 'true' || nextMode === 'strm',
        )
        setRedirectProxyEnabled(settings['cloud.playback_redirect_proxy_enabled'] !== 'false')
        setAutoGenerate(settings['strm.auto_generate_enabled'] === 'true')
      })
      .catch(() => undefined)
  }, [])

  // Default the import library to the first available one once loaded.
  useEffect(() => {
    if (!libraryID && libraries[0]) setLibraryID(libraries[0].id)
    if (!generateLibraryID && libraries[0]) setGenerateLibraryID(libraries[0].id)
  }, [libraries, libraryID, generateLibraryID])

  const onGenerate = async (e: FormEvent) => {
    e.preventDefault()
    if (!generateLibraryID || !baseURL.trim()) return
    if (!/^https?:\/\//i.test(baseURL.trim())) {
      toast.error('域名必须以 http:// 或 https:// 开头')
      return
    }
    setGenerating(true)
    try {
      const result = await strmAPI.generate({
        library_id: generateLibraryID,
        base_url: baseURL.trim().replace(/\/+$/, ''),
        output_dir: outputDir.trim(),
        overwrite,
        enabled: autoGenerate,
        include_local: true,
      })
      setGenerateResult(result)
      setOutputDir(result.output_dir || outputDir)
      toast.success(`生成完成：新增 ${result.generated} · 更新 ${result.updated} · 跳过 ${result.skipped}`)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '生成失败'
      toast.error(msg)
    } finally {
      setGenerating(false)
    }
  }

  const saveSTRMSettings = async () => {
    setSavingSettings(true)
    try {
      const effectiveMode: CloudPlaybackMode =
        strmPlaybackEnabled && !redirectProxyEnabled
          ? 'strm'
          : !strmPlaybackEnabled && redirectProxyEnabled
            ? 'redirect_proxy'
            : cloudPlaybackMode
      await Promise.all([
        adminAPI.updateSetting('cloud.playback_mode', effectiveMode),
        adminAPI.updateSetting('cloud.playback_strm_enabled', String(strmPlaybackEnabled)),
        adminAPI.updateSetting('cloud.playback_redirect_proxy_enabled', String(redirectProxyEnabled)),
        adminAPI.updateSetting('strm.enabled', String(strmPlaybackEnabled)),
        adminAPI.updateSetting('strm.auto_generate_enabled', String(autoGenerate)),
      ])
      setCloudPlaybackMode(effectiveMode)
      toast.success(
        strmPlaybackEnabled || redirectProxyEnabled
          ? `播放设置已保存：优先 ${effectiveMode === 'strm' ? 'STRMURL' : '302/反代'}`
          : '播放设置已保存：云盘第三方播放已关闭',
      )
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存 STRM 开关失败'
      toast.error(msg)
    } finally {
      setSavingSettings(false)
    }
  }

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
    if (!(await confirmAction({ title: '清除 STRM URL', message: `清除「${m.title}」的 STRM URL?`, confirmText: '清除' }))) return
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

  const playbackStatus =
    strmPlaybackEnabled && redirectProxyEnabled
      ? `两者开启 · 优先${cloudPlaybackMode === 'strm' ? 'STRMURL' : '302/反代'}`
      : strmPlaybackEnabled
        ? '仅 STRMURL'
        : redirectProxyEnabled
          ? '仅 302/反代'
          : '云盘第三方播放关闭'

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

      <section className="glass-panel space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-display text-lg font-semibold text-ink-600">自动生成 STRM 文件</h2>
            <p className="text-sm text-ink-50">
              只需要填写自己的访问域名，系统会按媒体库内每个媒体批量生成可播放的 .strm 文件。
            </p>
          </div>
          <span className={`rounded-full border px-3 py-1 text-xs font-semibold ${
            strmPlaybackEnabled || redirectProxyEnabled
              ? 'border-emerald-300/40 bg-emerald-400/10 text-emerald-500'
              : 'border-red-300/40 bg-red-400/10 text-red-500'
          }`}>
            {playbackStatus}
          </span>
        </div>
        <div className="grid gap-3 rounded-2xl border border-gray-200 bg-white/70 p-4 md:grid-cols-2">
          <label className="flex items-start gap-3 text-sm text-ink-100">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 accent-primary-400"
              checked={strmPlaybackEnabled}
              onChange={(e) => setStrmPlaybackEnabled(e.target.checked)}
            />
            <span>
              <span className="block font-medium text-ink-600">启用 STRMURL 播放</span>
              <span className="text-xs text-ink-50">第三方客户端可拿到 /api/stream/媒体ID 入口，适合 STRM 管理和自动生成方案。</span>
            </span>
          </label>
          <label className="flex items-start gap-3 text-sm text-ink-100">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 accent-primary-400"
              checked={redirectProxyEnabled}
              onChange={(e) => setRedirectProxyEnabled(e.target.checked)}
            />
            <span>
              <span className="block font-medium text-ink-600">启用 302/反代播放</span>
              <span className="text-xs text-ink-50">第三方客户端可拿到 /Videos/媒体ID/stream 入口，由服务端解析后 302 或必要时反代。</span>
            </span>
          </label>
        </div>
        <div className="grid gap-3 rounded-2xl border border-gray-200 bg-white/70 p-4 md:grid-cols-[1fr_1fr_auto]">
          <label className="text-sm text-ink-100">
            <span className="mb-1 block font-medium text-ink-600">两者都开启时优先</span>
            <select
              className="input-base"
              value={cloudPlaybackMode}
              onChange={(e) => setCloudPlaybackMode(e.target.value as CloudPlaybackMode)}
              disabled={!strmPlaybackEnabled || !redirectProxyEnabled}
            >
              <option value="strm">优先 STRMURL</option>
              <option value="redirect_proxy">优先 302/反代</option>
            </select>
            <span className="mt-1 block text-xs text-ink-50">只开启一个时自动使用已开启的播放方式；两个都关闭时云盘媒体不向第三方提供播放。</span>
          </label>
          <label className="flex items-start gap-3 text-sm text-ink-100">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 accent-primary-400"
              checked={autoGenerate}
              onChange={(e) => setAutoGenerate(e.target.checked)}
            />
            <span>
              <span className="block font-medium text-ink-600">扫描后自动刷新 STRM 文件</span>
              <span className="text-xs text-ink-50">默认关闭，避免扫描大型网盘库时重复写文件。</span>
            </span>
          </label>
          <button type="button" className="neon-button self-center" disabled={savingSettings} onClick={saveSTRMSettings}>
            {savingSettings ? <Loader2 size={16} className="animate-spin" /> : <Save size={16} />}
            保存开关
          </button>
        </div>
        <form onSubmit={onGenerate} className="grid gap-3 md:grid-cols-4">
          <select
            required
            className="input-base"
            value={generateLibraryID}
            onChange={(e) => setGenerateLibraryID(e.target.value)}
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
            className="input-base md:col-span-2"
            placeholder="http://NAS-IP:18080 或 https://media.example.com"
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
          />
          <button
            type="button"
            className="rounded-2xl border border-primary-400/40 px-3 py-2 text-sm text-brand-500 transition hover:bg-primary-400/10"
            onClick={() => setBaseURL(currentOrigin())}
          >
            使用当前访问地址
          </button>
          <label className="flex items-center gap-2 rounded-2xl border border-gray-200 bg-white/70 px-3 py-2 text-sm text-ink-50">
            <input
              type="checkbox"
              checked={overwrite}
              onChange={(e) => setOverwrite(e.target.checked)}
            />
            覆盖已存在
          </label>
          <input
            className="input-base md:col-span-3"
            placeholder="输出目录可留空，默认写入 data/strm/媒体库名"
            value={outputDir}
            onChange={(e) => setOutputDir(e.target.value)}
          />
          <button type="submit" disabled={generating || !generateLibraryID || !baseURL.trim()} className="neon-button md:col-span-4">
            {generating ? <Loader2 size={16} className="animate-spin" /> : <Wand2 size={16} />}
            {generating ? '生成中…' : '批量生成 STRM'}
          </button>
        </form>
        <p className="text-xs text-sand-500">
          生成内容为 <code>域名 + /api/stream/媒体ID?token=...</code>；第三方客户端播放优先方式由上方「STRMURL / 302反代」模式决定。域名会同步保存到系统设置中的「公开访问域名 / STRM 域名」。
        </p>
        {generateResult && (
          <div className="rounded-2xl border border-gray-200 bg-gray-50 p-4 text-sm text-ink-50">
            <div className="font-semibold text-ink-600">
              输出目录：{generateResult.output_dir}
            </div>
            <div className="mt-1">
              新增 {generateResult.generated} · 更新 {generateResult.updated} · 跳过 {generateResult.skipped}
            </div>
            {generateResult.errors && generateResult.errors.length > 0 && (
              <div className="mt-2 text-red-500">
                失败 {generateResult.errors.length} 条：{generateResult.errors.slice(0, 3).join('；')}
              </div>
            )}
          </div>
        )}
      </section>

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
        <form onSubmit={doSearch} className="flex flex-wrap gap-2">
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
                  className="rounded-xl border border-gray-200 bg-gray-50 p-4 space-y-3"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate font-medium text-ink-600">{m.title}</div>
                      <div className="truncate text-xs text-ink-50">
                        {m.year > 0 && `${m.year} · `}
                        {m.container || '本地文件'}
                      </div>
                      {isStrm && (
                        <div className="mt-1 break-all rounded-lg bg-emerald-400/10 px-2 py-0.5 text-xs text-emerald-300">
                          已设置 STRM
                        </div>
                      )}
                    </div>
                    {isStrm && (
                      <button
                        onClick={() => onDetach(m)}
                        className="rounded-lg border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                      >
                        <Trash2 size={12} className="inline" /> 清除
                      </button>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
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
