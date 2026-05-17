import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Globe, Plus, Trash2, Wifi, RefreshCw, X, Edit3, CheckCircle, XCircle, HelpCircle } from 'lucide-react'

import { sitesAPI } from '../api/sites'
import type { Site } from '../types'

// ── 站点类型映射 ──
const SITE_TYPE_LABELS: Record<string, string> = {
  nexusphp: 'NexusPHP',
  gazelle: 'Gazelle',
  unit3d: 'UNIT3D',
  mteam: 'M-Team',
  discuz: 'Discuz',
  custom_rss: '自定义 RSS',
}

const SITE_TYPE_ABBR: Record<string, string> = {
  nexusphp: 'NP',
  gazelle: 'GZ',
  unit3d: 'U3',
  mteam: 'MT',
  discuz: 'DZ',
  custom_rss: 'RS',
}

const SITE_TYPE_COLORS: Record<string, string> = {
  nexusphp: 'bg-blue-500/15 text-blue-400',
  gazelle: 'bg-purple-500/15 text-purple-400',
  unit3d: 'bg-orange-500/15 text-orange-400',
  mteam: 'bg-green-500/15 text-green-400',
  discuz: 'bg-yellow-500/15 text-yellow-400',
  custom_rss: 'bg-slate-500/15 text-slate-400',
}

const AUTH_TYPE_LABELS: Record<string, string> = {
  cookie: 'Cookie',
  api_key: 'API Key',
  auth_header: 'Auth Header',
}

// ── 默认表单 ──
const defaultForm = () => ({
  name: '',
  url: '',
  type: 'nexusphp',
  auth_type: 'cookie',
  cookie: '',
  api_key: '',
  auth_header: '',
  enabled: true,
  is_default: false,
  extra: '',
  // 高级设置
  user_agent: '',
  rss_url: '',
  timeout: 15,
  priority: 50,
  use_proxy: false,
  rate_limit: false,
  browser_emulation: false,
  downloader: '',
})

export function SitesPage() {
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const [showModal, setShowModal] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState(defaultForm())
  const [saving, setSaving] = useState(false)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const loadSites = async () => {
    setLoading(true)
    try {
      const res = await sitesAPI.list()
      setSites(Array.isArray(res.data) ? res.data : [])
    } catch {
      toast.error('加载站点列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadSites()
  }, [])

  // ── 弹窗操作 ──
  const openCreate = () => {
    setEditingId(null)
    setForm(defaultForm())
    setAdvancedOpen(false)
    setShowModal(true)
  }

  const openEdit = async (id: string) => {
    try {
      const res = await sitesAPI.get(id)
      const s = res.data as Site
      setEditingId(id)
      setForm({
        name: s.name || '',
        url: s.url || '',
        type: s.type || 'nexusphp',
        auth_type: s.auth_type || 'cookie',
        cookie: s.cookie || '',
        api_key: s.api_key || '',
        auth_header: s.auth_header || '',
        enabled: s.enabled !== false,
        is_default: s.is_default || false,
        extra: s.extra || '',
        // 高级设置
        user_agent: s.user_agent || '',
        rss_url: s.rss_url || '',
        timeout: s.timeout ?? 15,
        priority: s.priority ?? 50,
        use_proxy: s.use_proxy || false,
        rate_limit: s.rate_limit || false,
        browser_emulation: s.browser_emulation || false,
        downloader: s.downloader || '',
      })
      setAdvancedOpen(false)
      setShowModal(true)
    } catch {
      toast.error('获取站点详情失败')
    }
  }

  const closeModal = () => {
    setShowModal(false)
    setEditingId(null)
  }

  // ── 保存（支持静默模式）──
  const silentSave = async (): Promise<boolean> => {
    if (!form.name.trim() || !form.url.trim()) return false
    const payload: Record<string, unknown> = {
      name: form.name.trim(),
      url: form.url.trim(),
      type: form.type,
      auth_type: form.auth_type,
      cookie: form.cookie || '',
      api_key: form.api_key || '',
      auth_header: form.auth_header || '',
      enabled: form.enabled,
      is_default: form.is_default,
      extra: form.extra || '',
      user_agent: form.user_agent || '',
      rss_url: form.rss_url || '',
      timeout: Number(form.timeout) || 15,
      priority: Number(form.priority) || 50,
      use_proxy: !!form.use_proxy,
      rate_limit: !!form.rate_limit,
      browser_emulation: !!form.browser_emulation,
      downloader: form.downloader || '',
    }
    try {
      if (editingId) {
        await sitesAPI.update(editingId, payload)
      } else {
        const res = await sitesAPI.create(payload)
        setEditingId((res.data as Site)?.id ?? null)
      }
      return true
    } catch {
      return false
    }
  }

  const handleSave = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    const ok = await silentSave()
    if (ok) {
      toast.success(editingId ? '站点已更新' : '站点已添加')
      closeModal()
      await loadSites()
    } else {
      const msg = '保存失败'
      toast.error(msg)
    }
    setSaving(false)
  }

  // 测试（先自动保存）
  const handleTest = async (id: string) => {
    // 如果正在编辑当前站点，先保存
    if (editingId === id) {
      setSaving(true)
      const ok = await silentSave()
      setSaving(false)
      if (!ok) {
        toast.error('保存失败，无法测试')
        return
      }
    }
    setTestingId(id)
    try {
      const res = await sitesAPI.test(id)
      const msg = res?.message || '连接测试成功'
      toast.success(msg)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { message?: string } } })?.response?.data?.message ??
        '连接测试失败'
      toast.error(msg)
    } finally {
      setTestingId(null)
    }
  }

  // ── 删除 ──
  const handleDelete = async (site: Site) => {
    if (!confirm(`确定要删除站点「${site.name}」吗？此操作不可撤销。`)) return
    try {
      await sitesAPI.remove(site.id)
      toast.success('站点已删除')
      await loadSites()
    } catch {
      toast.error('删除站点失败')
    }
  }

  // ── 站点类型切换时自动切换认证方式 ──
  const handleTypeChange = (t: string) => {
    setForm((f) => ({
      ...f,
      type: t,
      auth_type: t === 'mteam' ? 'api_key' : f.auth_type,
    }))
  }

  return (
    <div className="space-y-6">
      {/* 页头 */}
      <div className="flex items-center justify-between">
        <h1 className="font-display text-3xl font-bold text-white">站点管理</h1>
        <button onClick={openCreate} className="neon-button flex items-center gap-2">
          <Plus size={16} />
          添加站点
        </button>
      </div>

      {/* 站点卡片网格 */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {sites.map((site) => (
          <div key={site.id} className="glass-panel p-4 space-y-3 transition-all hover:border-primary-400/30">
            {/* 头部 */}
            <div className="flex items-start justify-between">
              <div className="flex items-center gap-2 min-w-0">
                <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-xs font-bold shrink-0 ${SITE_TYPE_COLORS[site.type] || 'bg-slate-500/15 text-slate-400'}`}>
                  {SITE_TYPE_ABBR[site.type] || '?'}
                </div>
                <div className="min-w-0">
                  <div className="font-medium text-white truncate">{site.name}</div>
                  <div className="text-xs text-slate-400 truncate max-w-[160px]">{site.url}</div>
                </div>
              </div>
              {/* 状态指示 */}
              <div className="flex items-center gap-1 shrink-0 ml-2">
                {site.last_check_at ? (
                  site.last_error ? (
                    <XCircle size={14} className="text-red-400" />
                  ) : (
                    <CheckCircle size={14} className="text-green-400" />
                  )
                ) : (
                  <HelpCircle size={14} className="text-slate-500" />
                )}
                {!site.enabled && <span className="text-xs text-slate-500 ml-1">已停用</span>}
              </div>
            </div>

            {/* 标签 */}
            <div className="flex flex-wrap gap-1.5">
              <span className="text-xs px-1.5 py-0.5 rounded bg-white/5 text-slate-400">
                {SITE_TYPE_LABELS[site.type] || site.type}
              </span>
              <span className="text-xs px-1.5 py-0.5 rounded bg-white/5 text-slate-400">
                {AUTH_TYPE_LABELS[site.auth_type] || site.auth_type}
              </span>
              {site.is_default && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-primary-400/15 text-primary-400">默认</span>
              )}
              {site.use_proxy && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-blue-500/15 text-blue-400">代理</span>
              )}
              {site.rate_limit && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-yellow-500/15 text-yellow-400">限流</span>
              )}
              {site.browser_emulation && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-purple-500/15 text-purple-400">浏览器</span>
              )}
            </div>

            {/* 状态与统计（只读） */}
            <div className="text-xs text-slate-500 space-y-0.5">
              <div>状态：
                <span className={
                  site.login_status === 'ok' ? 'text-green-400' :
                  site.login_status === 'failed' ? 'text-red-400' : 'text-slate-400'
                }>
                  {site.login_status || 'unknown'}
                </span>
              </div>
              {(site.upload_bytes || 0) > 0 && (
                <div>↑ {Math.round((site.upload_bytes ?? 0) / 1073741824 * 100) / 100} GB / ↓ {Math.round((site.download_bytes ?? 0) / 1073741824 * 100) / 100} GB</div>
              )}
              {site.priority !== 50 && (
                <div>优先级：{site.priority}</div>
              )}
            </div>

            {/* 操作按钮 */}
            <div className="flex items-center gap-2 pt-1">
              <button
                onClick={() => handleTest(site.id)}
                disabled={testingId === site.id}
                className="flex-1 rounded border border-white/10 px-2 py-1.5 text-xs text-slate-300 hover:bg-white/5 disabled:opacity-50 flex items-center justify-center gap-1 transition"
              >
                {testingId === site.id ? (
                  <>
                    <RefreshCw size={12} className="animate-spin" />
                    测试中...
                  </>
                ) : (
                  <>
                    <Wifi size={12} />
                    测试连接
                  </>
                )}
              </button>
              <button
                onClick={() => openEdit(site.id)}
                className="rounded border border-white/10 p-1.5 text-slate-400 hover:text-white hover:bg-white/5 transition"
                title="编辑"
              >
                <Edit3 size={14} />
              </button>
              <button
                onClick={() => handleDelete(site)}
                className="rounded border border-white/10 p-1.5 text-slate-400 hover:text-red-400 hover:bg-red-400/10 transition"
                title="删除"
              >
                <Trash2 size={14} />
              </button>
            </div>
          </div>
        ))}

        {/* 空状态 */}
        {!loading && sites.length === 0 && (
          <div className="col-span-full py-12 text-center text-slate-400">
            <Globe size={40} className="mx-auto mb-3 text-slate-600" />
            <p>暂无站点</p>
            <p className="text-sm mt-1 text-slate-500">点击「添加站点」添加 PT/BT 站点</p>
          </div>
        )}

        {/* 加载中 */}
        {loading && (
          <div className="col-span-full py-12 text-center text-slate-400">
            <RefreshCw size={24} className="mx-auto mb-3 animate-spin" />
            <p>加载中...</p>
          </div>
        )}
      </div>

      {/* ── 创建/编辑弹窗 ── */}
      {showModal && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] bg-black/60 backdrop-blur-sm" onClick={closeModal}>
          <div
            className="glass-panel w-full max-w-xl max-h-[75vh] overflow-y-auto mx-4 space-y-5"
            onClick={(e) => e.stopPropagation()}
          >
            {/* 标题栏 */}
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold text-white">
                {editingId ? '编辑站点' : '添加站点'}
              </h2>
              <button onClick={closeModal} className="text-slate-400 hover:text-white transition">
                <X size={20} />
              </button>
            </div>

            <form onSubmit={handleSave} className="space-y-4">
              {/* 名称 */}
              <div>
                <label className="block text-sm text-slate-400 mb-1.5">站点名称 *</label>
                <input
                  required
                  className="input-base w-full"
                  placeholder="例如: 馒头、观众、家园"
                  value={form.name}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                />
              </div>

              {/* 地址 */}
              <div>
                <label className="block text-sm text-slate-400 mb-1.5">站点地址 *</label>
                <input
                  required
                  className="input-base w-full"
                  placeholder="https://www.example.com/"
                  value={form.url}
                  onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
                />
                <p className="text-xs text-slate-500 mt-1">格式: https://www.example.com/</p>
              </div>

              {/* 站点类型 + 状态 */}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm text-slate-400 mb-1.5">站点类型</label>
                  <select
                    className="input-base w-full"
                    value={form.type}
                    onChange={(e) => handleTypeChange(e.target.value)}
                  >
                    <option value="nexusphp">NexusPHP（国内主流PT）</option>
                    <option value="gazelle">Gazelle（HDBits等）</option>
                    <option value="unit3d">UNIT3D（BeyondHD等）</option>
                    <option value="mteam">馒头 M-Team（专用API）</option>
                    <option value="discuz">Discuz 论坛型</option>
                    <option value="custom_rss">自定义 RSS</option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm text-slate-400 mb-1.5">状态</label>
                  <div className="flex items-center gap-3 h-10">
                    <button
                      type="button"
                      onClick={() => setForm((f) => ({ ...f, enabled: !f.enabled }))}
                      className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors cursor-pointer ${form.enabled ? 'bg-primary-500' : 'bg-white/10'}`}
                    >
                      <span
                        className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform mt-0.5 ${form.enabled ? 'translate-x-4' : 'translate-x-0.5'}`}
                      />
                    </button>
                    <span className={`text-sm ${form.enabled ? 'text-white' : 'text-slate-500'}`}>
                      {form.enabled ? '启用' : '停用'}
                    </span>
                  </div>
                </div>
              </div>

              {/* 馒头提示 */}
              {form.type === 'mteam' && (
                <div className="p-3 rounded-lg border border-green-500/30 bg-green-500/5">
                  <div className="text-sm font-medium text-green-400 mb-1">馒头站点配置指南</div>
                  <div className="text-xs text-slate-400 space-y-1">
                    <div><b>站点地址：</b><code className="text-green-300">https://api2.m-team.cc</code></div>
                    <div><b>认证方式：</b>推荐使用「API Key / Passkey」</div>
                    <div className="pl-3 text-slate-500">
                      1. 登录馒头站 → 控制台 → 实验室 → 存取令牌<br />
                      2. 点击「创建令牌」，复制生成的 Token<br />
                      3. 将 Token 填入下方「令牌」输入框
                    </div>
                  </div>
                </div>
              )}

              {/* 认证方式 */}
              <div>
                <label className="block text-sm text-slate-400 mb-2">认证方式</label>
                <div className="flex gap-2 mb-3">
                  {[
                    { value: 'cookie', label: 'Cookie' },
                    { value: 'api_key', label: 'API Key' },
                    { value: 'auth_header', label: 'Auth Header' },
                  ].map((opt) => (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => setForm((f) => ({ ...f, auth_type: opt.value }))}
                      className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition ${
                        form.auth_type === opt.value
                          ? 'bg-primary-500 text-white border-primary-500'
                          : 'border-white/10 text-slate-400 hover:border-primary-500/50'
                      }`}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>

                {form.auth_type === 'cookie' && (
                  <div>
                    <label className="block text-xs text-slate-400 mb-1">Cookie</label>
                    <textarea
                      rows={3}
                      className="input-base w-full resize-none text-xs font-mono"
                      placeholder="uid=xxx; pass=xxx; ..."
                      value={form.cookie}
                      onChange={(e) => setForm((f) => ({ ...f, cookie: e.target.value }))}
                    />
                    <p className="text-xs text-slate-500 mt-1">从浏览器开发者工具的请求头中获取 Cookie 值</p>
                  </div>
                )}

                {form.auth_type === 'api_key' && (
                  <div>
                    <label className="block text-xs text-slate-400 mb-1">令牌（API Key / Passkey）</label>
                    <input
                      type="password"
                      className="input-base w-full font-mono text-sm"
                      placeholder="输入 API Key 或 Passkey"
                      value={form.api_key}
                      onChange={(e) => setForm((f) => ({ ...f, api_key: e.target.value }))}
                    />
                    <p className="text-xs text-slate-500 mt-1">
                      {form.type === 'mteam'
                        ? '馒头：控制台 → 实验室 → 存取令牌'
                        : '站点的访问 API Key'}
                    </p>
                  </div>
                )}

                {form.auth_type === 'auth_header' && (
                  <div>
                    <label className="block text-xs text-slate-400 mb-1">请求头（Authorization）</label>
                    <input
                      className="input-base w-full font-mono text-xs"
                      placeholder="Bearer eyJhbGciOiJIUzI1NiIs..."
                      value={form.auth_header}
                      onChange={(e) => setForm((f) => ({ ...f, auth_header: e.target.value }))}
                    />
                  </div>
                )}

                {/* RSS 地址（主表单） */}
                <div className="mt-4">
                  <label className="block text-xs text-slate-400 mb-1">RSS 地址</label>
                  <input
                    className="input-base w-full text-xs font-mono"
                    placeholder="https://.../torrents/rss?..."
                    value={form.rss_url}
                    onChange={(e) => setForm((f) => ({ ...f, rss_url: e.target.value }))}
                  />
                  <p className="text-xs text-slate-500 mt-1">站点 RSS 订阅地址，用于获取最新资源</p>
                </div>
              </div>

              {/* 高级选项 */}
              <div>
                <button
                  type="button"
                  onClick={() => setAdvancedOpen(!advancedOpen)}
                  className="flex items-center gap-1 text-xs text-slate-400 hover:text-white transition"
                >
                  {advancedOpen ? '▾' : '▸'} 高级选项
                </button>
                {advancedOpen && (
                  <div className="mt-3 pl-4 space-y-3 border-l border-white/10">
                    {/* 新增高级字段 */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                      <div>
                        <label className="block text-xs text-slate-400 mb-1">User-Agent</label>
                        <input
                          className="input-base w-full text-xs"
                          placeholder="自定义 UA，留空使用默认"
                          value={form.user_agent}
                          onChange={(e) => setForm((f) => ({ ...f, user_agent: e.target.value }))}
                        />
                      </div>
                      <div>
                        <label className="block text-xs text-slate-400 mb-1">请求超时 (秒)</label>
                        <input
                          type="number"
                          className="input-base w-full"
                          min={1}
                          max={300}
                          value={form.timeout}
                          onChange={(e) => setForm((f) => ({ ...f, timeout: Number(e.target.value) }))}
                        />
                      </div>
                      <div>
                        <label className="block text-xs text-slate-400 mb-1">优先级 (数字越大越优先)</label>
                        <input
                          type="number"
                          className="input-base w-full"
                          min={1}
                          max={100}
                          value={form.priority}
                          onChange={(e) => setForm((f) => ({ ...f, priority: Number(e.target.value) }))}
                        />
                      </div>
                      <div>
                        <label className="block text-xs text-slate-400 mb-1">关联下载器</label>
                        <input
                          className="input-base w-full text-xs"
                          placeholder="下载器 ID 或名称"
                          value={form.downloader}
                          onChange={(e) => setForm((f) => ({ ...f, downloader: e.target.value }))}
                        />
                      </div>
                    </div>

                    {/* 开关类字段 */}
                    <div className="flex flex-wrap gap-4">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          className="h-4 w-4 accent-primary-400"
                          checked={form.use_proxy}
                          onChange={(e) => setForm((f) => ({ ...f, use_proxy: e.target.checked }))}
                        />
                        <span className="text-xs text-slate-300">使用代理</span>
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          className="h-4 w-4 accent-primary-400"
                          checked={form.rate_limit}
                          onChange={(e) => setForm((f) => ({ ...f, rate_limit: e.target.checked }))}
                        />
                        <span className="text-xs text-slate-300">启用限流</span>
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          className="h-4 w-4 accent-primary-400"
                          checked={form.browser_emulation}
                          onChange={(e) => setForm((f) => ({ ...f, browser_emulation: e.target.checked }))}
                        />
                        <span className="text-xs text-slate-300">浏览器模拟</span>
                      </label>
                    </div>

                    {/* Extra JSON */}
                    <div>
                      <label className="block text-xs text-slate-400 mb-1">Extra 扩展配置 (JSON)</label>
                      <textarea
                        rows={3}
                        className="input-base w-full resize-none text-xs font-mono"
                        placeholder='{"key":"value"}'
                        value={form.extra}
                        onChange={(e) => setForm((f) => ({ ...f, extra: e.target.value }))}
                      />
                    </div>
                    <div className="flex items-center gap-3">
                      <button
                        type="button"
                        onClick={() => setForm((f) => ({ ...f, is_default: !f.is_default }))}
                        className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors cursor-pointer ${form.is_default ? 'bg-primary-500' : 'bg-white/10'}`}
                      >
                        <span
                          className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform mt-0.5 ${form.is_default ? 'translate-x-4' : 'translate-x-0.5'}`}
                        />
                      </button>
                      <span className="text-sm text-slate-400">设为默认站点</span>
                    </div>
                  </div>
                )}
              </div>

              {/* 按钮 */}
              <div className="flex justify-end gap-2 pt-2">
                <button type="button" onClick={closeModal} className="rounded border border-white/10 px-4 py-2 text-sm text-slate-300 hover:bg-white/5 transition">
                  取消
                </button>
                <button
                  type="submit"
                  disabled={saving || !form.name.trim() || !form.url.trim()}
                  className="neon-button text-sm disabled:opacity-50 flex items-center gap-1.5"
                >
                  {saving ? (
                    <>
                      <RefreshCw size={14} className="animate-spin" />
                      保存中...
                    </>
                  ) : (
                    '保存'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
