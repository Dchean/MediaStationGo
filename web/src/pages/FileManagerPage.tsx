import { useCallback, useEffect, useMemo, useState } from 'react'
import toast from 'react-hot-toast'
import {
  ChevronUp,
  Copy,
  FileVideo,
  Folder,
  FolderOpen,
  GitBranch,
  HardDrive,
  Home,
  Move,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'

import { filesAPI, type FileEntry, type FileListing } from '../api/files'
import { adminAPI } from '../api/admin'
import { libraryAPI } from '../api/library'
import { schedulerAPI } from '../api/scheduler'
import { toolsAPI } from '../api/tools'
import { confirmAction } from '../components/ConfirmDialog'
import type { Library, Setting } from '../types'

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

function formatScanSummary(scans: Array<{ name: string; added: number; updated: number; visited: number; error?: string }>): string {
  if (scans.length === 0) return ' · 未扫描：没有匹配的媒体库'
  const ok = scans.filter((scan) => !scan.error)
  const added = ok.reduce((sum, scan) => sum + (scan.added ?? 0), 0)
  const updated = ok.reduce((sum, scan) => sum + (scan.updated ?? 0), 0)
  const visited = ok.reduce((sum, scan) => sum + (scan.visited ?? 0), 0)
  return ` · 扫描 ${ok.length}/${scans.length} 个库 · 新入库 ${added} · 更新 ${updated} · 访问 ${visited}`
}

function formatScrapeSummary(scrapes: Array<{ name: string; matched: number; skipped?: boolean; reason?: string; error?: string }>): string {
  if (scrapes.length === 0) return ''
  const ok = scrapes.filter((scrape) => !scrape.error && !scrape.skipped)
  const skipped = scrapes.filter((scrape) => scrape.skipped).length
  const matched = ok.reduce((sum, scrape) => sum + (scrape.matched ?? 0), 0)
  if (ok.length === 0 && skipped > 0) return ` · 刮削跳过 ${skipped} 个库`
  return ` · 刮削 ${ok.length}/${scrapes.length} 个库 · 匹配 ${matched}${skipped ? ` · 跳过 ${skipped}` : ''}`
}

type AutoOrganizeConfig = {
  enabled: string
  afterDownload: string
  scrapeAfter: string
  downloadSmartClassify: string
  smartClassify: string
  sourceDir: string
  targetDir: string
  transferMode: string
  intervalSeconds: string
  keepSeeding: string
  movieFormat: string
  tvFormat: string
  animeFormat: string
  scrapeAutoOnScan: string
  scrapeProviders: string
  scrapeLanguage: string
  scrapeDelayMinMs: string
  scrapeDelayMaxMs: string
}

const AUTO_ORGANIZE_DEFAULTS: AutoOrganizeConfig = {
  enabled: 'false',
  afterDownload: 'false',
  scrapeAfter: 'true',
  downloadSmartClassify: 'true',
  smartClassify: 'true',
  sourceDir: '',
  targetDir: '',
  transferMode: 'hardlink',
  intervalSeconds: '300',
  keepSeeding: 'true',
  movieFormat: '{title} ({year})/{title} ({year})',
  tvFormat: '{title} ({year})/Season {season:02}/{title} S{season:02}E{episode:02}',
  animeFormat: '{title}/Season {season:02}/{title} S{season:02}E{episode:02}',
  scrapeAutoOnScan: 'false',
  scrapeProviders: 'tmdb,douban,bangumi,thetvdb,fanart',
  scrapeLanguage: 'zh-CN',
  scrapeDelayMinMs: '250',
  scrapeDelayMaxMs: '500',
}

const AUTO_ORGANIZE_KEYS: Record<keyof AutoOrganizeConfig, string> = {
  enabled: 'organize.auto',
  afterDownload: 'organizer.auto_after_download',
  scrapeAfter: 'organize.scrape_after',
  downloadSmartClassify: 'downloads.smart_classify',
  smartClassify: 'organizer.smart_classify',
  sourceDir: 'organize.source_dir',
  targetDir: 'organize.target_dir',
  transferMode: 'organize.transfer_mode',
  intervalSeconds: 'organize.interval_seconds',
  keepSeeding: 'organize.keep_seeding',
  movieFormat: 'organize.movie_format',
  tvFormat: 'organize.tv_format',
  animeFormat: 'organize.anime_format',
  scrapeAutoOnScan: 'scrape.auto_on_scan',
  scrapeProviders: 'scrape.providers',
  scrapeLanguage: 'scrape.language',
  scrapeDelayMinMs: 'scrape.delay_min_ms',
  scrapeDelayMaxMs: 'scrape.delay_max_ms',
}

type AutoOrganizeTab = 'basic' | 'naming' | 'scrape'

function settingIndex(rows: Setting[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) out[row.key] = row.value
  return out
}

function mergeAutoOrganizeSettings(rows: Setting[]): AutoOrganizeConfig {
  const idx = settingIndex(rows)
  return {
    enabled: idx[AUTO_ORGANIZE_KEYS.enabled] ?? AUTO_ORGANIZE_DEFAULTS.enabled,
    afterDownload: idx[AUTO_ORGANIZE_KEYS.afterDownload] ?? AUTO_ORGANIZE_DEFAULTS.afterDownload,
    scrapeAfter: idx[AUTO_ORGANIZE_KEYS.scrapeAfter] ?? AUTO_ORGANIZE_DEFAULTS.scrapeAfter,
    downloadSmartClassify: idx[AUTO_ORGANIZE_KEYS.downloadSmartClassify] ?? AUTO_ORGANIZE_DEFAULTS.downloadSmartClassify,
    smartClassify: idx[AUTO_ORGANIZE_KEYS.smartClassify] ?? AUTO_ORGANIZE_DEFAULTS.smartClassify,
    sourceDir: idx[AUTO_ORGANIZE_KEYS.sourceDir] ?? AUTO_ORGANIZE_DEFAULTS.sourceDir,
    targetDir: idx[AUTO_ORGANIZE_KEYS.targetDir] ?? AUTO_ORGANIZE_DEFAULTS.targetDir,
    transferMode: idx[AUTO_ORGANIZE_KEYS.transferMode] ?? AUTO_ORGANIZE_DEFAULTS.transferMode,
    intervalSeconds: idx[AUTO_ORGANIZE_KEYS.intervalSeconds] ?? AUTO_ORGANIZE_DEFAULTS.intervalSeconds,
    keepSeeding: idx[AUTO_ORGANIZE_KEYS.keepSeeding] ?? AUTO_ORGANIZE_DEFAULTS.keepSeeding,
    movieFormat: idx[AUTO_ORGANIZE_KEYS.movieFormat] ?? AUTO_ORGANIZE_DEFAULTS.movieFormat,
    tvFormat: idx[AUTO_ORGANIZE_KEYS.tvFormat] ?? AUTO_ORGANIZE_DEFAULTS.tvFormat,
    animeFormat: idx[AUTO_ORGANIZE_KEYS.animeFormat] ?? AUTO_ORGANIZE_DEFAULTS.animeFormat,
    scrapeAutoOnScan: idx[AUTO_ORGANIZE_KEYS.scrapeAutoOnScan] ?? AUTO_ORGANIZE_DEFAULTS.scrapeAutoOnScan,
    scrapeProviders: idx[AUTO_ORGANIZE_KEYS.scrapeProviders] ?? AUTO_ORGANIZE_DEFAULTS.scrapeProviders,
    scrapeLanguage: idx[AUTO_ORGANIZE_KEYS.scrapeLanguage] ?? AUTO_ORGANIZE_DEFAULTS.scrapeLanguage,
    scrapeDelayMinMs: idx[AUTO_ORGANIZE_KEYS.scrapeDelayMinMs] ?? AUTO_ORGANIZE_DEFAULTS.scrapeDelayMinMs,
    scrapeDelayMaxMs: idx[AUTO_ORGANIZE_KEYS.scrapeDelayMaxMs] ?? AUTO_ORGANIZE_DEFAULTS.scrapeDelayMaxMs,
  }
}

function settingOn(value: string): boolean {
  return ['1', 'true', 'yes', 'on', 'enabled', '启用', '开启'].includes(value.trim().toLowerCase())
}

function isCloudLibraryPath(value: string): boolean {
  return value.trim().toLowerCase().startsWith('cloud://')
}

// FileManagerPage provides a focused local storage view:
// browse allowed roots, optionally recurse, and perform safe local operations.
export function FileManagerPage() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [path, setPath] = useState('')
  const [data, setData] = useState<FileListing | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [recursive, setRecursive] = useState(false)
  const [selected, setSelected] = useState<FileEntry | null>(null)
  const [selectedPaths, setSelectedPaths] = useState<string[]>([])
  const [folderName, setFolderName] = useState('')
  const [renameTo, setRenameTo] = useState('')
  const [destPath, setDestPath] = useState('')
  const [transferMode, setTransferMode] = useState('copy')
  const [busy, setBusy] = useState('')
  const [organizeLibraryID, setOrganizeLibraryID] = useState('')
  const [organizeDestPath, setOrganizeDestPath] = useState('')
  const [organizeTransferMode, setOrganizeTransferMode] = useState('hardlink')
  const [organizeMediaType, setOrganizeMediaType] = useState('auto')
  const [scanAfter, setScanAfter] = useState(true)
  const [scrapeAfter, setScrapeAfter] = useState(true)
  const [organizeBusy, setOrganizeBusy] = useState('')
  const [previewItems, setPreviewItems] = useState<Array<{
    source: string
    target?: string
    action: string
    reason?: string
  }>>([])
  const [autoConfig, setAutoConfig] = useState<AutoOrganizeConfig>(AUTO_ORGANIZE_DEFAULTS)
  const [autoDirty, setAutoDirty] = useState(false)
  const [autoSaving, setAutoSaving] = useState(false)
  const [autoRunning, setAutoRunning] = useState(false)
  const [autoLoading, setAutoLoading] = useState(true)
  const [autoTab, setAutoTab] = useState<AutoOrganizeTab>('basic')

  const currentDir = useMemo(() => {
    if (data?.path) return data.path
    return path
  }, [data?.path, path])
  const localLibraries = useMemo(
    () => libraries.filter((library) => !isCloudLibraryPath(library.path)),
    [libraries],
  )
  const autoMoveKeepsSeeding = autoConfig.transferMode === 'move' && settingOn(autoConfig.keepSeeding)
  const manualMoveKeepsSeeding = organizeTransferMode === 'move' && settingOn(autoConfig.keepSeeding)

  const refresh = useCallback(() => {
    setLoading(true)
    setError('')
    filesAPI
      .list(path, recursive ? 5000 : 1000, recursive)
      .then(setData)
      .catch((err: unknown) => {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '加载失败'
        setError(msg)
      })
      .finally(() => setLoading(false))
  }, [path, recursive])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    libraryAPI.list({ includeHidden: true }).then(setLibraries).catch(() => undefined)
  }, [])

  const refreshAutoConfig = useCallback(() => {
    setAutoLoading(true)
    adminAPI
      .listSettings()
      .then((rows) => {
        const nextConfig = mergeAutoOrganizeSettings(rows)
        setAutoConfig(nextConfig)
        setScrapeAfter(settingOn(nextConfig.scrapeAfter))
        setAutoDirty(false)
      })
      .catch(() => undefined)
      .finally(() => setAutoLoading(false))
  }, [])

  useEffect(() => {
    refreshAutoConfig()
  }, [refreshAutoConfig])

  useEffect(() => {
    setSelected(null)
    setSelectedPaths([])
    setRenameTo('')
  }, [path])

  useEffect(() => {
    if (!organizeLibraryID) return
    const lib = localLibraries.find((item) => item.id === organizeLibraryID)
    if (!lib) {
      setOrganizeLibraryID('')
      return
    }
    setOrganizeDestPath(lib.path)
    setOrganizeMediaType(lib.type || 'auto')
  }, [localLibraries, organizeLibraryID])

  const changeAutoConfig = (key: keyof AutoOrganizeConfig, value: string) => {
    setAutoConfig((current) => ({ ...current, [key]: value }))
    if (key === 'scrapeAfter') setScrapeAfter(settingOn(value))
    setAutoDirty(true)
  }

  const saveAutoConfig = async (): Promise<boolean> => {
    setAutoSaving(true)
    try {
      for (const key of Object.keys(AUTO_ORGANIZE_KEYS) as Array<keyof AutoOrganizeConfig>) {
        await adminAPI.updateSetting(AUTO_ORGANIZE_KEYS[key], autoConfig[key] ?? '')
      }
      setAutoDirty(false)
      toast.success('整理入库设置已保存')
      return true
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '保存整理入库设置失败')
      return false
    } finally {
      setAutoSaving(false)
    }
  }

  const runAutoOrganizeNow = async () => {
    if (autoDirty) {
      const saved = await saveAutoConfig()
      if (!saved) return
    }
    setAutoRunning(true)
    try {
      await schedulerAPI.run('organize_source')
      toast.success('已触发自动整理任务，请稍后刷新媒体库查看入库结果')
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '触发自动整理失败')
    } finally {
      setAutoRunning(false)
    }
  }

  const enter = (e: FileEntry) => {
    if (e.is_dir) setPath(e.path)
  }

  const choose = (e: FileEntry) => {
    setSelected(e)
    setRenameTo(e.name)
    if (e.is_dir) setDestPath(e.path)
  }

  const toggleSelectedPath = (entry: FileEntry, checked: boolean) => {
    setSelectedPaths((current) => {
      if (checked) {
        return current.includes(entry.path) ? current : [...current, entry.path]
      }
      return current.filter((item) => item !== entry.path)
    })
  }

  const createFolder = async () => {
    if (!currentDir || !folderName.trim()) return
    setBusy('mkdir')
    try {
      const res = await filesAPI.createFolder(currentDir, folderName.trim())
      toast.success(`已创建目录：${res.path}`)
      setFolderName('')
      refresh()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '创建失败')
    } finally {
      setBusy('')
    }
  }

  const renameSelected = async () => {
    if (!selected || !renameTo.trim()) return
    setBusy('rename')
    try {
      const res = await filesAPI.rename(selected.path, renameTo.trim())
      toast.success(`已重命名：${res.path}`)
      setSelected(null)
      refresh()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '重命名失败')
    } finally {
      setBusy('')
    }
  }

  const deleteSelected = async () => {
    if (!selected) return
    const ok = await confirmAction({
      title: '确认删除文件',
      message: `将删除：${selected.path}。此操作不可恢复，请确认不是媒体库根目录。`,
      confirmText: '确认删除',
      danger: true,
    })
    if (!ok) return
    setBusy('delete')
    try {
      await filesAPI.remove(selected.path)
      toast.success('已删除')
      setSelected(null)
      refresh()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '删除失败')
    } finally {
      setBusy('')
    }
  }

  const transferSelected = async () => {
    if (!selected || selected.is_dir || !destPath.trim()) return
    setBusy('transfer')
    try {
      const res = await filesAPI.transfer(selected.path, destPath.trim(), transferMode)
      toast.success(`已完成转移：${res.path}`)
      if (transferMode === 'move') setSelected(null)
      refresh()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '转移失败')
    } finally {
      setBusy('')
    }
  }

  const organizeSources = selectedPaths.length > 0 ? selectedPaths : [selected?.path || currentDir].filter(Boolean)
  const organizeSource = organizeSources.length === 1 ? organizeSources[0] : `${organizeSources.length} 个已选项目`
  const organizeReady = organizeSources.length > 0 && Boolean(organizeDestPath.trim())

  const runManualOrganize = async (dryRun: boolean) => {
    if (!organizeReady) {
      toast.error('请选择来源文件/文件夹，并设置目标媒体库或目的路径')
      return
    }
    if (!dryRun) {
      const ok = await confirmAction({
        title: '确认整理入库',
        message: `来源：${organizeSources.join('\n')}\n目标：${organizeDestPath}\n方式：${organizeTransferMode}${scanAfter ? '\n整理完成后会扫描入库。' : ''}${scanAfter && scrapeAfter ? '\n扫描后会自动刮削。' : ''}`,
        confirmText: '开始整理',
      })
      if (!ok) return
    }
    setOrganizeBusy(dryRun ? 'preview' : 'run')
    try {
      const results = []
      for (const sourcePath of organizeSources) {
        results.push(await toolsAPI.organizeDirectory({
          source_path: sourcePath,
          dest_path: organizeDestPath.trim(),
          transfer_mode: organizeTransferMode,
          media_type: organizeMediaType === 'auto' ? undefined : organizeMediaType,
          scan_after: !dryRun && scanAfter,
          scrape_after: !dryRun && scanAfter && scrapeAfter,
          library_id: !dryRun && scanAfter && organizeLibraryID ? organizeLibraryID : undefined,
          dry_run: dryRun,
        }))
      }
      const preview = results.flatMap((result) => result.items ?? [])
      setPreviewItems(preview)
      const organized = results.reduce((sum, result) => sum + (result.organized ?? 0), 0)
      const replaced = results.reduce((sum, result) => sum + (result.replaced ?? 0), 0)
      const skipped = results.reduce((sum, result) => sum + (result.skipped ?? 0), 0)
      const errors = results.flatMap((result) => result.errors ?? [])
      const scans = results.flatMap((result) => result.scans ?? [])
      const scrapes = results.flatMap((result) => result.scrapes ?? [])
      const total = organized + replaced + skipped + errors.length
      if (total === 0) {
        toast(`未发现可整理视频：${organizeSource}`, {
          icon: '!',
          duration: 6000,
        })
        return
      }
      if (dryRun) {
        toast.success(`预览完成：新增 ${organized} · 替换 ${replaced} · 跳过 ${skipped}`)
        return
      }
      const scanText = scanAfter ? formatScanSummary(scans) : ''
      const scrapeText = scanAfter && scrapeAfter ? formatScrapeSummary(scrapes) : ''
      toast.success(`整理完成：新增 ${organized} · 替换 ${replaced} · 跳过 ${skipped}${scanText}${scrapeText}`)
      setSelectedPaths([])
      refresh()
    } catch (err: unknown) {
      toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '整理失败')
    } finally {
      setOrganizeBusy('')
    }
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="font-display text-3xl font-bold text-ink-600">文件管理</h1>
        <p className="text-sm text-ink-50">
          在下载目录中选择文件夹或视频，直接设置目标并整理入库。
        </p>
      </header>

      <div className="flex flex-wrap items-center gap-2">
        <button className="neon-button !px-3 !py-1 !text-xs" onClick={() => setPath('')} title="返回根列表">
          <Home size={14} /> 根
        </button>
        {data?.parent && (
          <button className="neon-button !px-3 !py-1 !text-xs" onClick={() => setPath(data.parent ?? '')}>
            <ChevronUp size={14} /> 上一级
          </button>
        )}
        <button className="neon-button !px-3 !py-1 !text-xs" onClick={refresh}>
          <RefreshCw size={14} /> 刷新
        </button>
        <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
          <input type="checkbox" checked={recursive} onChange={(e) => setRecursive(e.target.checked)} />
          递归扫描
        </label>
        {data?.path && (
          <span className="rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 font-mono text-xs text-ink-100">
            {data.path}
          </span>
        )}
      </div>

      <section className="glass-panel space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="font-display text-lg font-semibold text-ink-600">自动整理设置</h2>
            <p className="text-xs text-sand-500">
              设置后可自动递归扫描下载/待整理目录，整理到媒体库目录；也可以在这里立即执行一次。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className="neon-button !border-primary-400/30 !bg-white !text-brand-500"
              disabled={autoLoading || autoSaving}
              onClick={refreshAutoConfig}
            >
              <RefreshCw size={14} /> 重新读取
            </button>
            <button
              type="button"
              className="neon-button !border-primary-400/30 !bg-white !text-brand-500"
              disabled={autoLoading || autoSaving || !autoDirty}
              onClick={() => void saveAutoConfig()}
            >
              {autoSaving ? '保存中…' : '保存设置'}
            </button>
            <button
              type="button"
              className="neon-button"
              disabled={autoLoading || autoSaving || autoRunning}
              onClick={runAutoOrganizeNow}
            >
              {autoRunning ? '执行中…' : '立即整理一次'}
            </button>
          </div>
        </div>

        <div className="flex flex-wrap gap-2 rounded-2xl border border-gray-200 bg-gray-50 p-1">
          {[
            ['basic', '基础设置'],
            ['naming', '命名规则'],
            ['scrape', '刮削联动'],
          ].map(([key, label]) => (
            <button
              key={key}
              type="button"
              className={
                autoTab === key
                  ? 'rounded-xl bg-white px-3 py-1.5 text-xs font-semibold text-brand-500 shadow-sm'
                  : 'rounded-xl px-3 py-1.5 text-xs text-ink-100 hover:bg-white/70'
              }
              onClick={() => setAutoTab(key as AutoOrganizeTab)}
            >
              {label}
            </button>
          ))}
          <span className="ml-auto self-center px-2 text-xs text-sand-500">
            {autoDirty ? '有未保存设置' : '设置已同步'} · 定时任务名：organize_source
          </span>
        </div>

        {autoTab === 'basic' && (
          <>
            <div className="grid gap-3 lg:grid-cols-[1fr_1fr_150px_140px]">
              <label className="space-y-1">
                <span className="text-xs text-ink-50">整理源目录（待整理 / 下载目录）</span>
                <div className="flex gap-2">
                  <input
                    className="input-base w-full"
                    placeholder="例如 F:\\downloads 或 /downloads"
                    value={autoConfig.sourceDir}
                    onChange={(event) => changeAutoConfig('sourceDir', event.target.value)}
                  />
                  <button
                    type="button"
                    className="rounded-xl border border-gray-200 px-3 text-xs text-ink-100 hover:border-primary-400/40"
                    disabled={!currentDir}
                    onClick={() => changeAutoConfig('sourceDir', currentDir)}
                  >
                    当前
                  </button>
                </div>
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">整理目的地目录（媒体库根目录）</span>
                <div className="flex gap-2">
                  <input
                    className="input-base w-full"
                    placeholder="例如 F:\\media 或 /media"
                    value={autoConfig.targetDir}
                    onChange={(event) => changeAutoConfig('targetDir', event.target.value)}
                  />
                  <button
                    type="button"
                    className="rounded-xl border border-gray-200 px-3 text-xs text-ink-100 hover:border-primary-400/40"
                    disabled={!currentDir}
                    onClick={() => changeAutoConfig('targetDir', currentDir)}
                  >
                    当前
                  </button>
                </div>
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">默认整理方式</span>
                <select
                  className="input-base w-full"
                  value={autoConfig.transferMode}
                  onChange={(event) => changeAutoConfig('transferMode', event.target.value)}
                >
                  <option value="hardlink">硬链接</option>
                  <option value="move">移动（关闭保种才会移动）</option>
                  <option value="copy">复制</option>
                  <option value="symlink">软链接</option>
                </select>
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">检查间隔（秒）</span>
                <input
                  type="number"
                  min={60}
                  className="input-base w-full"
                  value={autoConfig.intervalSeconds}
                  onChange={(event) => changeAutoConfig('intervalSeconds', event.target.value)}
                />
              </label>
            </div>

            {autoMoveKeepsSeeding && (
              <div className="rounded-xl border border-orange-300 bg-orange-50 px-3 py-2 text-xs text-orange-700">
                当前同时选择了“移动”和“保种”。为避免 qB 做种源文件被删除，后端会实际使用硬链接；Docker / NAS
                多挂载或不同子卷下可能报 invalid cross-device link。需要真正移动时请关闭“保种”，需要保种但硬链接失败时请选择“复制”。
              </div>
            )}

            <div className="flex flex-wrap items-center gap-3">
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.enabled)}
                  onChange={(event) => changeAutoConfig('enabled', event.target.checked ? 'true' : 'false')}
                />
                整理源目录定时自动整理
              </label>
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.afterDownload)}
                  onChange={(event) => changeAutoConfig('afterDownload', event.target.checked ? 'true' : 'false')}
                />
                qB 下载完成后自动整理
              </label>
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.downloadSmartClassify)}
                  onChange={(event) => changeAutoConfig('downloadSmartClassify', event.target.checked ? 'true' : 'false')}
                />
                下载器智能分类
              </label>
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.smartClassify)}
                  onChange={(event) => changeAutoConfig('smartClassify', event.target.checked ? 'true' : 'false')}
                />
                智能分类到子库
              </label>
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.keepSeeding)}
                  onChange={(event) => changeAutoConfig('keepSeeding', event.target.checked ? 'true' : 'false')}
                />
                保种
              </label>
            </div>
          </>
        )}

        {autoTab === 'naming' && (
          <div className="grid gap-3">
            <label className="space-y-1">
              <span className="text-xs text-ink-50">电影命名格式</span>
              <input
                className="input-base w-full font-mono text-xs"
                value={autoConfig.movieFormat}
                onChange={(event) => changeAutoConfig('movieFormat', event.target.value)}
              />
            </label>
            <label className="space-y-1">
              <span className="text-xs text-ink-50">剧集命名格式</span>
              <input
                className="input-base w-full font-mono text-xs"
                value={autoConfig.tvFormat}
                onChange={(event) => changeAutoConfig('tvFormat', event.target.value)}
              />
            </label>
            <label className="space-y-1">
              <span className="text-xs text-ink-50">动漫命名格式</span>
              <input
                className="input-base w-full font-mono text-xs"
                value={autoConfig.animeFormat}
                onChange={(event) => changeAutoConfig('animeFormat', event.target.value)}
              />
            </label>
            <p className="text-xs text-sand-500">
              可用占位符：{'{title}'} {'{year}'} {'{season}'} {'{season:02}'} {'{episode}'} {'{episode:02}'} {'{category}'}。扩展名会自动补齐。
            </p>
          </div>
        )}

        {autoTab === 'scrape' && (
          <>
            <div className="flex flex-wrap items-center gap-3">
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.scrapeAfter)}
                  onChange={(event) => changeAutoConfig('scrapeAfter', event.target.checked ? 'true' : 'false')}
                />
                整理后自动刮削
              </label>
              <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
                <input
                  type="checkbox"
                  checked={settingOn(autoConfig.scrapeAutoOnScan)}
                  onChange={(event) => changeAutoConfig('scrapeAutoOnScan', event.target.checked ? 'true' : 'false')}
                />
                扫描后自动刮削
              </label>
            </div>
            <div className="grid gap-3 lg:grid-cols-[1fr_160px_160px_160px]">
              <label className="space-y-1">
                <span className="text-xs text-ink-50">刮削源优先级</span>
                <input
                  className="input-base w-full"
                  placeholder="tmdb,douban,bangumi,thetvdb,fanart"
                  value={autoConfig.scrapeProviders}
                  onChange={(event) => changeAutoConfig('scrapeProviders', event.target.value)}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">首选语言</span>
                <input
                  className="input-base w-full"
                  value={autoConfig.scrapeLanguage}
                  onChange={(event) => changeAutoConfig('scrapeLanguage', event.target.value)}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">最小间隔 ms</span>
                <input
                  type="number"
                  min={0}
                  className="input-base w-full"
                  value={autoConfig.scrapeDelayMinMs}
                  onChange={(event) => changeAutoConfig('scrapeDelayMinMs', event.target.value)}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-ink-50">最大间隔 ms</span>
                <input
                  type="number"
                  min={0}
                  className="input-base w-full"
                  value={autoConfig.scrapeDelayMaxMs}
                  onChange={(event) => changeAutoConfig('scrapeDelayMaxMs', event.target.value)}
                />
              </label>
            </div>
          </>
        )}
      </section>

      {data?.path && (
        <section className="glass-panel space-y-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 className="font-display text-lg font-semibold text-ink-600">手动整理入库</h2>
              <p className="text-xs text-sand-500">来源优先使用选中项；未选中时使用当前目录。</p>
            </div>
            <div className="max-w-xl truncate rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 font-mono text-xs text-ink-100" title={organizeSource}>
              来源：{organizeSource || '未选择'}
            </div>
          </div>
          {selectedPaths.length > 0 && (
            <div className="flex flex-wrap items-center gap-2 rounded-xl border border-primary-400/20 bg-primary-400/5 px-3 py-2 text-xs text-ink-100">
              <span>已选择 {selectedPaths.length} 个项目用于整理。</span>
              <button type="button" className="font-semibold text-brand-500 hover:text-brand-700" onClick={() => setSelectedPaths([])}>
                清空选择
              </button>
            </div>
          )}

          <div className="grid gap-3 lg:grid-cols-[1.2fr_1fr_150px_150px]">
            <label className="space-y-1">
              <span className="text-xs text-ink-50">目标媒体库 / 存储</span>
              <select
                className="input-base w-full"
                value={organizeLibraryID}
                onChange={(event) => setOrganizeLibraryID(event.target.value)}
              >
                <option value="">手动填写目的路径</option>
                {localLibraries.map((library) => (
                  <option key={library.id} value={library.id}>
                    {library.name}（{library.type}）— {library.path}
                  </option>
                ))}
              </select>
              <span className="text-[11px] text-sand-500">
                手动整理只写入本地可写媒体库；网盘请到“外部存储”中挂载、扫描或转存。
              </span>
            </label>
            <label className="space-y-1">
              <span className="text-xs text-ink-50">目的路径</span>
              <input
                className="input-base w-full"
                placeholder="例如 F:\\media\\电影 或 /media/电影"
                value={organizeDestPath}
                onChange={(event) => setOrganizeDestPath(event.target.value)}
              />
            </label>
            <label className="space-y-1">
              <span className="text-xs text-ink-50">类型</span>
              <select className="input-base w-full" value={organizeMediaType} onChange={(event) => setOrganizeMediaType(event.target.value)}>
                <option value="auto">自动识别</option>
                <option value="movie">电影</option>
                <option value="tv">剧集</option>
                <option value="anime">动漫</option>
                <option value="variety">综艺</option>
                <option value="adult">成人</option>
              </select>
            </label>
            <label className="space-y-1">
              <span className="text-xs text-ink-50">整理方式</span>
              <select className="input-base w-full" value={organizeTransferMode} onChange={(event) => setOrganizeTransferMode(event.target.value)}>
                <option value="hardlink">硬链接</option>
                <option value="move">移动（关闭保种才会移动）</option>
                <option value="copy">复制</option>
                <option value="symlink">软链接</option>
              </select>
            </label>
          </div>

          {manualMoveKeepsSeeding && (
            <div className="rounded-xl border border-orange-300 bg-orange-50 px-3 py-2 text-xs text-orange-700">
              “保种”已开启，选择“移动”时后端会改用硬链接以保留下载源。要执行真正移动，请先在上方自动整理设置里关闭“保种”并保存。
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
              <input type="checkbox" checked={scanAfter} onChange={(event) => setScanAfter(event.target.checked)} />
              整理后扫描入库
            </label>
            <label className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-ink-100">
              <input
                type="checkbox"
                checked={scanAfter && scrapeAfter}
                disabled={!scanAfter}
                onChange={(event) => setScrapeAfter(event.target.checked)}
              />
              整理后自动刮削
            </label>
            <button
              type="button"
              className="neon-button !border-primary-400/30 !bg-white !text-brand-500"
              disabled={!organizeReady || organizeBusy !== ''}
              onClick={() => runManualOrganize(true)}
            >
              {organizeBusy === 'preview' ? '预览中…' : '预览整理'}
            </button>
            <button
              type="button"
              className="neon-button"
              disabled={!organizeReady || organizeBusy !== ''}
              onClick={() => runManualOrganize(false)}
            >
              {organizeBusy === 'run' ? '整理中…' : '开始整理入库'}
            </button>
          </div>

          {previewItems.length > 0 && (
            <div className="max-h-72 overflow-auto rounded-xl border border-gray-200 bg-white/70">
              <table className="w-full text-left text-xs">
                <thead className="sticky top-0 bg-white text-sand-500">
                  <tr>
                    <th className="px-3 py-2">动作</th>
                    <th>来源</th>
                    <th>目标</th>
                    <th>原因</th>
                  </tr>
                </thead>
                <tbody>
                  {previewItems.map((item, index) => (
                    <tr key={`${item.source}-${index}`} className="border-t border-gray-200 align-top">
                      <td className="px-3 py-2 font-semibold text-brand-500">{item.action}</td>
                      <td className="max-w-xs truncate py-2 font-mono text-ink-100" title={item.source}>{item.source}</td>
                      <td className="max-w-xs truncate py-2 font-mono text-ink-100" title={item.target}>{item.target || '—'}</td>
                      <td className="py-2 text-sand-500">{item.reason || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {data?.path && (
        <details className="glass-panel" open={Boolean(selected)}>
          <summary className="cursor-pointer list-none font-display text-lg font-semibold text-ink-600">
            文件操作
            <span className="ml-2 text-xs font-normal text-sand-500">
              新建目录 / 重命名 / 删除 / 转移
            </span>
          </summary>
          <div className="mt-4 grid gap-4 lg:grid-cols-[1fr_1.2fr]">
            <div className="space-y-2">
              <h2 className="text-sm font-semibold text-ink-600">新建目录</h2>
              <div className="flex gap-2">
                <input className="input-base flex-1" placeholder="新目录名称" value={folderName} onChange={(e) => setFolderName(e.target.value)} />
                <button className="neon-button" disabled={busy === 'mkdir' || !folderName.trim()} onClick={createFolder}>
                  <Plus size={16} /> 创建
                </button>
              </div>
            </div>

            <div className="space-y-2">
              <h2 className="text-sm font-semibold text-ink-600">选中项</h2>
              {selected ? (
                <div className="space-y-2">
                  <p className="truncate font-mono text-xs text-ink-50" title={selected.path}>{selected.path}</p>
                  <div className="flex flex-wrap gap-2">
                    <input className="input-base min-w-[220px] flex-1" value={renameTo} onChange={(e) => setRenameTo(e.target.value)} />
                    <button className="neon-button" disabled={busy === 'rename' || !renameTo.trim()} onClick={renameSelected}>
                      <Pencil size={16} /> 重命名
                    </button>
                    <button className="rounded-xl border border-red-400/40 px-3 py-2 text-sm text-red-500 hover:bg-red-50" disabled={busy === 'delete'} onClick={deleteSelected}>
                      <Trash2 size={16} className="inline" /> 删除
                    </button>
                  </div>
                  {!selected.is_dir && (
                    <div className="grid gap-2 md:grid-cols-[1fr_140px_auto]">
                      <input className="input-base" placeholder="目标目录路径" value={destPath} onChange={(e) => setDestPath(e.target.value)} />
                      <select className="input-base" value={transferMode} onChange={(e) => setTransferMode(e.target.value)}>
                        <option value="copy">复制</option>
                        <option value="move">移动</option>
                        <option value="hardlink">硬链接</option>
                        <option value="symlink">软链接</option>
                      </select>
                      <button className="neon-button" disabled={busy === 'transfer' || !destPath.trim()} onClick={transferSelected}>
                        {transferMode === 'move' ? <Move size={16} /> : transferMode === 'copy' ? <Copy size={16} /> : <GitBranch size={16} />}
                        转移
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-ink-50">先在下方列表点击“操作”选择文件或目录。</p>
              )}
            </div>
          </div>
        </details>
      )}

      {loading && <p className="text-sand-500">加载中…</p>}
      {error && <div className="glass-panel !border-red-400/40 text-red-400">{error}</div>}

      {!loading && data && !data.path && data.roots && (
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {data.roots.map((r) => (
            <button key={r.path} onClick={() => setPath(r.path)} className="glass-panel flex items-center gap-3 text-left transition hover:border-primary-400/40">
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
        <div className="glass-panel overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="w-10 py-2">
                  <input
                    type="checkbox"
                    aria-label="选择当前目录全部项目"
                    checked={data.entries.length > 0 && data.entries.every((entry) => selectedPaths.includes(entry.path))}
                    onChange={(event) => {
                      const entries = data.entries ?? []
                      if (event.target.checked) {
                        setSelectedPaths(entries.map((entry) => entry.path))
                      } else {
                        setSelectedPaths([])
                      }
                    }}
                  />
                </th>
                <th className="py-2">名称</th>
                <th>大小</th>
                <th>修改时间</th>
                <th className="text-right">选择</th>
              </tr>
            </thead>
            <tbody>
              {data.entries.map((entry) => (
                <tr key={entry.path} className={'border-t border-gray-200 transition hover:bg-gray-50 ' + (selected?.path === entry.path || selectedPaths.includes(entry.path) ? 'bg-primary-400/5' : '')}>
                  <td className="py-2">
                    <input
                      type="checkbox"
                      aria-label={`选择 ${entry.name}`}
                      checked={selectedPaths.includes(entry.path)}
                      onChange={(event) => toggleSelectedPath(entry, event.target.checked)}
                    />
                  </td>
                  <td className="py-2 text-ink-600">
                    <button className="flex max-w-xl items-center gap-2 text-left" onClick={() => (entry.is_dir ? enter(entry) : choose(entry))} title={entry.path}>
                      {entry.is_dir ? <Folder size={16} className="text-brand-500" /> : <FileVideo size={16} className="text-ink-50" />}
                      <span className="truncate">{recursive ? entry.path.replace((data.path || '') + '\\', '').replace((data.path || '') + '/', '') : entry.name}</span>
                    </button>
                  </td>
                  <td className="text-ink-100">{entry.is_dir ? '—' : fmtBytes(entry.size)}</td>
                  <td className="text-sand-500">{new Date(entry.modified * 1000).toLocaleString()}</td>
                  <td className="text-right">
                    <button className="rounded-lg border border-gray-200 px-2 py-1 text-xs text-ink-100 hover:border-primary-400/40" onClick={() => choose(entry)}>
          <HardDrive size={12} className="mr-1 inline" /> 操作
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!loading && data?.entries && data.entries.length === 0 && <p className="text-ink-50">空目录。</p>}
    </div>
  )
}
