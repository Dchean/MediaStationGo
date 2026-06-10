import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Cloud, FileVideo, Folder, Loader2, QrCode, Save, Send, Trash2, Upload } from 'lucide-react'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import {
  cloudAPI,
  storageAPI,
  type CloudEntry,
  type QRSession,
  type StorageType,
} from '../api/storage_config'
import { confirmAction } from '../components/ConfirmDialog'
import type { Library } from '../types'

const CLOUD_TYPES: StorageType[] = ['cloud115', 'quark', 'clouddrive2', 'openlist']
const isCloud = (t: StorageType) => CLOUD_TYPES.includes(t)
const TYPE_LABEL: Record<string, string> = {
  alist: 'ALIST',
  openlist: 'OpenList',
  webdav: 'WEBDAV',
  s3: 'S3',
  cloud115: '115网盘',
  quark: '夸克网盘',
  clouddrive2: 'CloudDrive2',
}
const PATH_BASED_CLOUD = new Set<StorageType>(['openlist', 'clouddrive2'])

function normalizeCloudDisplayPath(value: string) {
  let text = value.trim()
  try {
    text = decodeURIComponent(text)
  } catch {
    // Keep the original value if it is not URI-encoded.
  }
  return text.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
}

function cloudLibraryProvider(path: string) {
  if (!path.toLowerCase().startsWith('cloud://')) return ''
  try {
    return new URL(path).host
  } catch {
    return ''
  }
}

function cloudLibraryLabel(path: string) {
  try {
    const parsed = new URL(path)
    const display = normalizeCloudDisplayPath(parsed.pathname)
    const scanDir = normalizeCloudDisplayPath(parsed.searchParams.get('dir') ?? '')
    return display || scanDir || '根目录'
  } catch {
    return path
  }
}

function cloudMountDisplayPath(type: StorageType, stack: { id: string; name: string }[], child?: CloudEntry) {
  if (PATH_BASED_CLOUD.has(type)) {
    return normalizeCloudDisplayPath(child?.id ?? stack[stack.length - 1]?.id ?? '')
  }
  const parts = stack.slice(1).map((item) => item.name).filter(Boolean)
  if (child?.name) parts.push(child.name)
  return parts.map(normalizeCloudDisplayPath).filter(Boolean).join('/')
}

// StorageConfigPage manages the Alist / S3 / WebDAV adapters used by
// the import / playback / STRM subsystems. Mirrors the Vue UI's
// `admin/storage/*` tabs in a tabbed React surface.
export function StorageConfigPage() {
  const [active, setActive] = useState<StorageType>('openlist')
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-400/10 text-blue-300">
          <Cloud size={20} />
        </div>
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">外部存储</h1>
          <p className="text-sm text-ink-50">
            配置 OpenList / Alist / S3 / WebDAV / CloudDrive2 / 网盘(115 / 夸克)后端，支持本地转存、网盘挂载和 302/反代播放
          </p>
        </div>
      </div>

      <div className="flex gap-2 border-b border-gray-200">
        {(['openlist', 'alist', 'webdav', 'clouddrive2', 's3', 'cloud115', 'quark'] as StorageType[]).map((t) => (
          <button
            key={t}
            onClick={() => setActive(t)}
            className={
              'border-b-2 px-4 py-2 text-sm ' +
              (active === t
                ? 'border-primary-400 text-brand-500'
                : 'border-transparent text-ink-50 hover:text-white')
            }
          >
            {TYPE_LABEL[t] ?? t}
          </button>
        ))}
      </div>

      <StorageForm key={active} type={active} />
    </div>
  )
}

const FIELD_DEFS: Record<StorageType, { key: string; label: string; secret?: boolean; placeholder?: string }[]> = {
  alist: [
    { key: 'server', label: 'Server URL', placeholder: 'https://alist.example.com' },
    { key: 'token', label: 'Token', secret: true },
  ],
  openlist: [
    { key: 'server', label: 'OpenList Server URL(API / 转存)', placeholder: 'http://NAS-IP:5244' },
    { key: 'token', label: 'OpenList Token(API 转存可选)', secret: true },
    { key: 'url', label: 'WebDAV URL(浏览/挂载)', placeholder: 'http://NAS-IP:5244/dav/' },
    { key: 'username', label: 'WebDAV 用户名' },
    { key: 'password', label: 'WebDAV 密码', secret: true },
    { key: 'force_302', label: '强制 302 直链(true/false,默认反代)' },
  ],
  webdav: [
    { key: 'url', label: 'URL', placeholder: 'https://example.com/dav/' },
    { key: 'username', label: '用户名' },
    { key: 'password', label: '密码', secret: true },
  ],
  s3: [
    { key: 'endpoint', label: 'Endpoint', placeholder: 'https://s3.amazonaws.com' },
    { key: 'region', label: 'Region', placeholder: 'us-east-1' },
    { key: 'bucket', label: 'Bucket' },
    { key: 'access_key', label: 'Access Key', secret: true },
    { key: 'secret_key', label: 'Secret Key', secret: true },
    { key: 'force_path_style', label: 'force_path_style (true/false)' },
  ],
  cloud115: [
    { key: 'cookie', label: 'Cookie(UID / CID / SEID,或扫码登录自动填充)', secret: true, placeholder: 'UID=...; CID=...; SEID=...' },
    { key: 'force_proxy', label: '强制反代(true/false,默认 302 直链)' },
  ],
  quark: [
    { key: 'cookie', label: 'Cookie(从 pan.quark.cn 复制整段)', secret: true, placeholder: '__pus=...; __kp=...; kps=...' },
    { key: 'force_302', label: '强制 302 直链(true/false,默认反代)' },
  ],
  clouddrive2: [
    { key: 'url', label: 'CloudDrive2 WebDAV URL', placeholder: 'http://host.docker.internal:19798/dav 或 http://NAS-IP:19798/dav' },
    { key: 'username', label: '用户名' },
    { key: 'password', label: '密码 / Token', secret: true },
    { key: 'token', label: 'Authorization Token(可选)', secret: true, placeholder: 'Bearer ... 或 Basic ...' },
    { key: 'force_302', label: '强制 302 直链(true/false,默认反代)' },
  ],
}

function StorageForm({ type }: { type: StorageType }) {
  const fields = useMemo(() => FIELD_DEFS[type], [type])
  const [config, setConfig] = useState<Record<string, string>>({})
  const [enabled, setEnabled] = useState(true)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      const r = await storageAPI.get(type)
      const next: Record<string, string> = {}
      for (const f of fields) {
        const v = r.config?.[f.key]
        // List() redacts secrets to "********"; show empty so the user
        // doesn't accidentally save the placeholder.
        next[f.key] = v === '********' ? '' : v ?? ''
      }
      setConfig(next)
      setEnabled(r.enabled)
    } catch {
      const next: Record<string, string> = {}
      for (const f of fields) next[f.key] = ''
      setConfig(next)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh().catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type])

  const onSave = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await storageAPI.save(type, config, enabled)
      toast.success('已保存')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const onTest = async () => {
    setTesting(true)
    try {
      const r = await storageAPI.test(type, config)
      if (r.ok) toast.success('连接成功')
      else toast.error(r.error ?? '连接失败')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '测试失败'
      toast.error(msg)
    } finally {
      setTesting(false)
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-12 text-ink-50">
        <Loader2 className="animate-spin" />
      </div>
    )
  }

  return (
    <form onSubmit={onSave} className="glass-panel space-y-4">
      {fields.map((f) => (
        <label key={f.key} className="block">
          <span className="mb-1 block text-sm text-ink-100">
            {f.label}
            <span className="ml-2 font-mono text-[10px] text-gray-500">{f.key}</span>
          </span>
          <input
            type={f.secret ? 'password' : 'text'}
            className="input-base"
            placeholder={f.placeholder}
            value={config[f.key] ?? ''}
            onChange={(e) => setConfig((c) => ({ ...c, [f.key]: e.target.value }))}
          />
        </label>
      ))}
      {type === 'cloud115' && (
        <QRLoginPanel
          type={type}
          onCookie={(c) => setConfig((cfg) => ({ ...cfg, cookie: c }))}
        />
      )}
      <label className="flex items-center gap-2 text-sm text-ink-100">
        <input
          type="checkbox"
          className="h-4 w-4 accent-primary-400"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        启用
      </label>
      <div className="flex justify-end gap-2 pt-2">
        <button
          type="button"
          onClick={onTest}
          disabled={testing}
          className="rounded-lg border border-gray-200 px-4 py-2 text-sm text-ink-100 hover:bg-gray-50"
        >
          {testing ? <Loader2 size={14} className="inline animate-spin" /> : <Send size={14} className="inline" />}
          {' '}测试
        </button>
        <button type="submit" disabled={saving} className="neon-button">
          {saving ? <Loader2 size={16} className="animate-spin" /> : <Save size={16} />}
          保存
        </button>
      </div>
      <StorageUploadPanel type={type} />
      {isCloud(type) && <CloudBrowser type={type} />}
    </form>
  )
}

function StorageUploadPanel({ type }: { type: StorageType }) {
  const [sourcePath, setSourcePath] = useState('')
  const [destPath, setDestPath] = useState('/MediaStationGo')
  const [recursive, setRecursive] = useState(true)
  const [includeSidecars, setIncludeSidecars] = useState(true)
  const [overwrite, setOverwrite] = useState(false)
  const [busy, setBusy] = useState(false)

  const supported = type === 'alist' || type === 'openlist' || type === 'webdav' || type === 'clouddrive2'

  const submit = async () => {
    if (!supported) {
      toast.error('本地直传目前支持 OpenList / Alist / WebDAV / CloudDrive2。115/123/夸克建议通过 OpenList、CloudDrive2 或 Alist 桥接后转存。')
      return
    }
    if (!sourcePath.trim()) {
      toast.error('请填写本地源目录或文件路径')
      return
    }
    setBusy(true)
    try {
      const { result, error } = await storageAPI.uploadLocal(type, {
        source_path: sourcePath.trim(),
        dest_path: destPath.trim() || '/',
        recursive,
        include_sidecars: includeSidecars,
        overwrite,
      })
      const errText = error || (result.errors && result.errors.length > 0 ? ` · 错误 ${result.errors.length}` : '')
      toast.success(`转存完成：上传 ${result.uploaded} · 跳过 ${result.skipped} · ${fmtBytes(result.bytes)}${errText}`)
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '转存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-xl border border-gray-200 bg-gray-50/70 p-4">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <h3 className="flex items-center gap-2 font-display text-base font-semibold text-ink-600">
            <Upload size={16} /> 本地媒体转存到此存储
          </h3>
          <p className="mt-1 text-xs text-ink-50">
            复制本地媒体文件到外部存储，保留本地源文件；自动跳过远端已存在文件。
          </p>
        </div>
        {!supported && (
          <span className="rounded-full bg-amber-100 px-2 py-1 text-xs text-amber-700">
            直传待接
          </span>
        )}
      </div>
      {!supported && (
        <p className="mb-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          115 / 夸克原生上传需要私有分片上传协议。推荐把 115、123、夸克等挂载到 OpenList、CloudDrive2 或 Alist 后，在这里选择 OpenList / CloudDrive2 / Alist 转存。
        </p>
      )}
      {type === 'openlist' && (
        <p className="mb-3 rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-800">
          OpenList 默认端口常见为 5244，WebDAV 地址通常是 http://host:5244/dav/。如果未配置 HTTPS 反代，请不要填写 https://，否则会出现 “server gave HTTP response to HTTPS client”。
        </p>
      )}
      {type === 'clouddrive2' && (
        <p className="mb-3 rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-800">
          CloudDrive2 已经对接 115、123、阿里、夸克等网盘；这里通过它的 WebDAV 入口浏览、挂载和上传，播放默认走服务端反代以携带认证头。
        </p>
      )}
      <div className="grid gap-3 lg:grid-cols-2">
        <label className="block">
          <span className="mb-1 block text-sm text-ink-100">本地源目录 / 文件</span>
          <input
            className="input-base"
            placeholder="例如 /media/电影 或 F:\\media\\Movies"
            value={sourcePath}
            onChange={(event) => setSourcePath(event.target.value)}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-sm text-ink-100">网盘目标目录</span>
          <input
            className="input-base"
            placeholder="/MediaStationGo"
            value={destPath}
            onChange={(event) => setDestPath(event.target.value)}
          />
        </label>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-4 text-sm text-ink-100">
        <label className="flex items-center gap-2">
          <input type="checkbox" className="h-4 w-4 accent-primary-400" checked={recursive} onChange={(event) => setRecursive(event.target.checked)} />
          递归目录
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" className="h-4 w-4 accent-primary-400" checked={includeSidecars} onChange={(event) => setIncludeSidecars(event.target.checked)} />
          同步 NFO / 海报 / 字幕
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" className="h-4 w-4 accent-primary-400" checked={overwrite} onChange={(event) => setOverwrite(event.target.checked)} />
          覆盖远端同名文件
        </label>
        <button type="button" className="neon-button ml-auto" disabled={busy || !supported} onClick={submit}>
          {busy ? <Loader2 size={16} className="animate-spin" /> : <Upload size={16} />}
          {busy ? '转存中…' : '开始转存'}
        </button>
      </div>
    </div>
  )
}

function fmtBytes(value: number): string {
  if (!value) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let idx = 0
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${size.toFixed(size >= 10 || idx === 0 ? 0 : 1)} ${units[idx]}`
}

// QRLoginPanel drives the 115 QR-code login: start → render image → poll →
// fill the cookie field on confirmation.
function QRLoginPanel({ type, onCookie }: { type: StorageType; onCookie: (c: string) => void }) {
  const [sess, setSess] = useState<QRSession | null>(null)
  const [state, setState] = useState<string>('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!sess) return
    let alive = true
    const timer = setInterval(async () => {
      try {
        const st = await cloudAPI.qrPoll(type, sess)
        if (!alive) return
        setState(st.state)
        if (st.state === 'confirmed' && st.cookie) {
          onCookie(st.cookie)
          toast.success('扫码登录成功,Cookie 已填入,请点击保存')
          setSess(null)
        } else if (st.state === 'expired') {
          toast.error('二维码已过期,请重新获取')
          setSess(null)
        }
      } catch {
        /* keep polling */
      }
    }, 2000)
    return () => {
      alive = false
      clearInterval(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sess])

  const start = async () => {
    setBusy(true)
    try {
      const s = await cloudAPI.qrStart(type)
      setSess(s)
      setState('waiting')
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '获取二维码失败'
      toast.error(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-lg border border-gray-200 p-3">
      <button
        type="button"
        onClick={start}
        disabled={busy}
        className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-ink-100 hover:bg-gray-50"
      >
        {busy ? <Loader2 size={14} className="animate-spin" /> : <QrCode size={14} />}
        使用 115 App 扫码登录
      </button>
      {sess && (
        <div className="mt-3 flex items-center gap-3">
          <img src={sess.qr_image_url} alt="115 QR" className="h-40 w-40 rounded bg-white p-1" />
          <span className="text-sm text-ink-50">
            {state === 'scanned' ? '已扫描,请在手机上确认登录…' : '请使用 115 手机 App 扫描二维码…'}
          </span>
        </div>
      )}
    </div>
  )
}

// CloudBrowser lists 网盘 directories and imports a file as a 302-backed media.
function CloudBrowser({ type }: { type: StorageType }) {
  const [stack, setStack] = useState<{ id: string; name: string }[]>([{ id: '', name: '根目录' }])
  const [items, setItems] = useState<CloudEntry[]>([])
  const [mounts, setMounts] = useState<Library[]>([])
  const [loading, setLoading] = useState(false)
  const [mounting, setMounting] = useState(false)
  const [batchMounting, setBatchMounting] = useState(false)
  const [mountMediaType, setMountMediaType] = useState('auto')
  const [error, setError] = useState('')

  const cur = stack[stack.length - 1]
  const load = async (dir: string) => {
    setLoading(true)
    setError('')
    try {
      const r = await cloudAPI.list(type, dir)
      setItems(r.items ?? [])
      if (r.error) setError(r.error)
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '加载失败')
      setItems([])
    } finally {
      setLoading(false)
    }
  }

  const loadMounts = async () => {
    const libs = await libraryAPI.list({ includeHidden: true })
    setMounts(libs.filter((lib) => cloudLibraryProvider(lib.path) === type))
  }

  useEffect(() => {
    load(cur.id).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stack.length, type])

  useEffect(() => {
    loadMounts().catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type])

  const enter = (e: CloudEntry) => setStack((s) => [...s, { id: e.id, name: e.name }])
  const goTo = (i: number) => setStack((s) => s.slice(0, i + 1))
  const currentMountPath = () => cloudMountDisplayPath(type, stack)
  const childMountPath = (child: CloudEntry) => cloudMountDisplayPath(type, stack, child)

  const handleMountResult = (res: unknown, label: string) => {
    const out = res as { already_mounted?: boolean; skipped?: boolean; reason?: string; library?: Library; message?: string; estimate_message?: string }
    if (out.skipped) {
      toast(`已跳过「${label}」：和已挂载目录重叠`)
      return 'skipped'
    }
    if (out.already_mounted) {
      toast(`「${label}」已经挂载，后台会刷新扫描并自动入库`)
      return 'mounted'
    }
    toast.success(`已挂载「${label}」，${out.message ?? '后台会递归扫描并自动加入媒体库'}。${out.estimate_message ?? ''}`)
    return 'mounted'
  }

  const doImport = async (e: CloudEntry) => {
    const ref = type === 'cloud115' ? e.pick_code || e.id : e.id
    try {
      await cloudAPI.import(type, ref, e.name, e.size)
      toast.success(`已导入「${e.name}」,可在媒体库中 302 播放`)
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '导入失败')
    }
  }

  const mountCurrent = async () => {
    setMounting(true)
    try {
      const label = TYPE_LABEL[type] ?? type
      const name = cur.id ? `${label} · ${cur.name}` : label
      const res = await cloudAPI.mount(type, cur.id, name, mountMediaType, currentMountPath())
      handleMountResult(res, cur.name)
      await loadMounts()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '挂载失败')
    } finally {
      setMounting(false)
    }
  }

  const mountVisibleDirectories = async () => {
    const dirs = items.filter((item) => item.is_dir)
    if (dirs.length === 0) {
      toast.error('当前目录下没有可挂载的子目录')
      return
    }
    setBatchMounting(true)
    let ok = 0
    let skipped = 0
    let failed = 0
    const label = TYPE_LABEL[type] ?? type
    for (const dir of dirs) {
      try {
        const result = await cloudAPI.mount(type, dir.id, `${label} · ${dir.name}`, 'auto', childMountPath(dir))
        const state = handleMountResult(result, dir.name)
        if (state === 'skipped') skipped += 1
        else ok += 1
      } catch {
        failed += 1
      }
    }
    if (failed > 0) {
      toast.error(`已挂载 ${ok} 个目录，跳过 ${skipped} 个重叠目录，失败 ${failed} 个`)
    } else {
      toast.success(`已挂载 ${ok} 个目录，跳过 ${skipped} 个重叠目录，后台会自动生成 302/STRM 播放入口`)
    }
    await loadMounts()
    setBatchMounting(false)
  }

  const removeMount = async (lib: Library) => {
    const ok = await confirmAction({
      title: '移除网盘挂载',
      message: `仅移除「${lib.name}」在本项目中的媒体库和媒体记录，不会删除网盘文件。`,
      confirmText: '移除',
    })
    if (!ok) return
    await libraryAPI.remove(lib.id)
    toast.success('已移除挂载')
    await loadMounts()
  }

  return (
    <div className="mt-2 rounded-lg border border-gray-200 p-3" onClick={(e) => e.preventDefault()}>
      {mounts.length > 0 && (
        <div className="mb-3 rounded border border-blue-100 bg-blue-50/60 p-2">
          <div className="mb-1 text-xs font-semibold text-ink-100">已挂载目录</div>
          <div className="space-y-1">
            {mounts.map((lib) => (
              <div key={lib.id} className="flex items-center gap-2 rounded bg-white/80 px-2 py-1 text-xs">
                <span className="min-w-0 flex-1 truncate text-ink-100">
                  {lib.name} · {cloudLibraryLabel(lib.path)}
                </span>
                <button
                  type="button"
                  className="rounded border border-red-200 px-1.5 py-0.5 text-red-500 hover:bg-red-50"
                  onClick={() => removeMount(lib)}
                  title="移除挂载"
                >
                  <Trash2 size={13} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-1 text-xs text-ink-50">
          <span className="text-ink-100">网盘资源:</span>
          {stack.map((s, i) => (
            <span key={i}>
              <button type="button" className="hover:text-brand-500" onClick={() => goTo(i)}>
                {s.name}
              </button>
              {i < stack.length - 1 && <span className="mx-1">/</span>}
            </span>
          ))}
        </div>
        <p className="basis-full text-xs text-ink-50">
          挂载后不会复制网盘文件；后台会递归读取该目录里的子文件夹和媒体文件，扫描到的影片会自动加入对应媒体库。小目录通常几十秒，大目录取决于网盘接口速度。
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <select
            className="rounded border border-gray-200 bg-white px-2 py-0.5 text-xs text-ink-100"
            value={mountMediaType}
            onChange={(event) => setMountMediaType(event.target.value)}
          >
            <option value="auto">自动识别</option>
            <option value="movie">电影</option>
            <option value="tv">剧集</option>
            <option value="anime">动漫</option>
            <option value="variety">综艺</option>
            <option value="adult">成人</option>
          </select>
          <button
            type="button"
            className="rounded border border-brand-400/40 px-2 py-0.5 text-xs text-brand-500 hover:bg-brand-400/10"
            disabled={mounting || batchMounting || loading}
            onClick={mountCurrent}
          >
            {mounting ? '挂载中…' : '挂载当前目录为媒体库并递归扫描'}
          </button>
          <button
            type="button"
            className="rounded border border-blue-300 px-2 py-0.5 text-xs text-blue-600 hover:bg-blue-50"
            disabled={mounting || batchMounting || loading || items.every((item) => !item.is_dir)}
            onClick={mountVisibleDirectories}
          >
            {batchMounting ? '批量挂载中…' : '一键把当前目录下所有文件夹挂载为媒体库'}
          </button>
        </div>
      </div>
      {loading ? (
        <div className="flex justify-center py-4 text-ink-50">
          <Loader2 className="animate-spin" size={16} />
        </div>
      ) : error ? (
        <p className="py-2 text-sm text-red-400">{error}</p>
      ) : items.length === 0 ? (
        <p className="py-2 text-sm text-ink-50">该目录为空</p>
      ) : (
        <ul className="divide-y divide-gray-100">
          {items.map((e) => (
            <li key={e.id} className="flex items-center gap-2 py-1.5 text-sm">
              {e.is_dir ? <Folder size={15} className="text-amber-400" /> : <FileVideo size={15} className="text-blue-300" />}
              {e.is_dir ? (
                <button type="button" className="flex-1 text-left text-ink-100 hover:text-brand-500" onClick={() => enter(e)}>
                  {e.name}
                </button>
              ) : (
                <>
                  <span className="flex-1 truncate text-ink-100">{e.name}</span>
                  <button
                    type="button"
                    className="rounded border border-gray-200 px-2 py-0.5 text-xs text-ink-100 hover:bg-gray-50"
                    onClick={() => doImport(e)}
                  >
                    导入
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
