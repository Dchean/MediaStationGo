import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Globe, Plus, Trash2, Wifi, WifiOff } from 'lucide-react'

import { sitesAPI, type Site } from '../api/sites'

const SITE_TYPES = [
  { value: 'nexusphp', label: 'NexusPHP (大多数国内PT站)' },
  { value: 'gazelle', label: 'Gazelle (HDBits/OPS/RED)' },
  { value: 'unit3d', label: 'UNIT3D (BeyondHD/BluTopia)' },
  { value: 'mteam', label: 'M-Team (馒头)' },
  { value: 'custom_rss', label: '自定义 RSS' },
]

const AUTH_TYPES = [
  { value: 'cookie', label: 'Cookie' },
  { value: 'api_key', label: 'API Key / Passkey' },
  { value: 'authorization', label: 'Authorization Header' },
]

// SitesPage manages PT/BT tracker sites — CRUD + connection test.
export function SitesPage() {
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)

  const refresh = () =>
    sitesAPI
      .list()
      .then(setSites)
      .finally(() => setLoading(false))

  useEffect(() => {
    refresh()
  }, [])

  const testSite = async (id: string) => {
    try {
      const r = await sitesAPI.test(id)
      if (r.success) {
        toast.success(r.message)
      } else {
        toast.error(r.message)
      }
      await refresh()
    } catch {
      toast.error('连接测试失败')
    }
  }

  const deleteSite = async (site: Site) => {
    if (!confirm(`确定删除站点「${site.name}」?`)) return
    await sitesAPI.remove(site.id)
    toast.success('已删除')
    await refresh()
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Globe className="h-6 w-6 text-primary-400" />
          <div>
            <h1 className="font-display text-3xl font-bold text-white">站点管理</h1>
            <p className="text-sm text-slate-400">
              添加 PT/BT 站点后可进行跨站资源搜索和自动下载。
            </p>
          </div>
        </div>
        <button onClick={() => setShowForm(!showForm)} className="neon-button">
          <Plus size={16} /> 添加站点
        </button>
      </header>

      {showForm && <AddSiteForm onCreated={() => { setShowForm(false); refresh() }} />}

      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && sites.length === 0 && (
        <div className="glass-panel">
          <p className="text-slate-300">
            还没有配置任何站点。点击"添加站点"开始配置你的 PT/BT 资源站。
          </p>
        </div>
      )}

      <div className="space-y-3">
        {sites.map((site) => (
          <div key={site.id} className="glass-panel flex items-center gap-4 !p-4">
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <p className="font-medium text-white">{site.name}</p>
                <span className="rounded border border-white/10 bg-white/5 px-1.5 py-0.5 text-xs text-slate-400">
                  {site.site_type}
                </span>
                {site.login_status === 'ok' && (
                  <Wifi size={14} className="text-emerald-400" />
                )}
                {site.login_status === 'fail' && (
                  <WifiOff size={14} className="text-red-400" />
                )}
                {!site.enabled && (
                  <span className="rounded border border-yellow-400/40 px-1.5 py-0.5 text-xs text-yellow-400">
                    已禁用
                  </span>
                )}
              </div>
              <p className="mt-1 font-mono text-xs text-slate-500">{site.base_url}</p>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => testSite(site.id)}
                className="rounded border border-primary-400/40 px-2 py-1 text-xs text-primary-400 hover:bg-primary-400/10"
              >
                测试
              </button>
              <button
                onClick={() => deleteSite(site)}
                className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function AddSiteForm({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [siteType, setSiteType] = useState('nexusphp')
  const [authType, setAuthType] = useState('cookie')
  const [cookie, setCookie] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const [rssURL, setRSSURL] = useState('')
  const [saving, setSaving] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await sitesAPI.create({
        name,
        base_url: baseURL,
        site_type: siteType,
        auth_type: authType,
        cookie: authType === 'cookie' ? cookie : undefined,
        api_key: authType === 'api_key' ? apiKey : undefined,
        rss_url: siteType === 'custom_rss' ? rssURL : undefined,
      })
      toast.success('站点已添加')
      onCreated()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '添加失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={submit} className="glass-panel space-y-3">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">站点名称</span>
          <input
            required
            className="input-base"
            placeholder="如: 馒头 / HDSky / OPS"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">站点地址</span>
          <input
            required
            className="input-base"
            placeholder="https://example.com"
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">站点类型</span>
          <select
            className="input-base"
            value={siteType}
            onChange={(e) => setSiteType(e.target.value)}
          >
            {SITE_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">认证方式</span>
          <select
            className="input-base"
            value={authType}
            onChange={(e) => setAuthType(e.target.value)}
          >
            {AUTH_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {authType === 'cookie' && (
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">Cookie</span>
          <textarea
            className="input-base min-h-[60px]"
            placeholder="从浏览器 F12 → Network → Cookie 复制"
            value={cookie}
            onChange={(e) => setCookie(e.target.value)}
          />
        </label>
      )}
      {authType === 'api_key' && (
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">API Key / Passkey</span>
          <input
            className="input-base"
            type="password"
            placeholder="站点生成的 API Token"
            value={apiKey}
            onChange={(e) => setAPIKey(e.target.value)}
          />
        </label>
      )}
      {siteType === 'custom_rss' && (
        <label className="block">
          <span className="mb-1 block text-xs text-slate-400">RSS 地址</span>
          <input
            className="input-base"
            placeholder="https://example.com/rss.xml"
            value={rssURL}
            onChange={(e) => setRSSURL(e.target.value)}
          />
        </label>
      )}

      <button type="submit" disabled={saving} className="neon-button">
        {saving ? '添加中…' : '添加站点'}
      </button>
    </form>
  )
}
