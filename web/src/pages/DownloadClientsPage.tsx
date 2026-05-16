import { FormEvent, useEffect, useState } from 'react'
import { Loader2, Pencil, Plus, Send, Server, Trash2 } from 'lucide-react'
import toast from 'react-hot-toast'

import {
  downloadClientsAPI,
  type DownloadClient,
  type DownloadClientInput,
  type DownloadClientType,
} from '../api/download_clients'

// DownloadClientsPage manages multiple downloader integrations.
// Replaces the Vue UI's DownloadView "clients" tab with a typed CRUD
// surface and a per-client Test button.
export function DownloadClientsPage() {
  const [clients, setClients] = useState<DownloadClient[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<DownloadClient | null>(null)
  const [showForm, setShowForm] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      setClients(await downloadClientsAPI.list())
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const onTest = async (id: string) => {
    try {
      const r = await downloadClientsAPI.test(id)
      if (r.ok) toast.success('连接成功')
      else toast.error(r.error ?? '连接失败')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '测试失败'
      toast.error(msg)
    }
  }

  const onDelete = async (c: DownloadClient) => {
    if (!confirm(`确定删除「${c.name}」?`)) return
    try {
      await downloadClientsAPI.remove(c.id)
      toast.success('已删除')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '删除失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-cyan-400/10 text-cyan-300">
            <Server size={20} />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-white">下载器管理</h1>
            <p className="text-sm text-slate-400">
              qBittorrent / Aria2 / Transmission · 多客户端 + 连接测试
            </p>
          </div>
        </div>
        <button
          onClick={() => {
            setEditing(null)
            setShowForm(true)
          }}
          className="neon-button"
        >
          <Plus size={16} /> 添加下载器
        </button>
      </div>

      {loading && (
        <div className="flex justify-center py-12 text-slate-400">
          <Loader2 className="animate-spin" />
        </div>
      )}

      {!loading && clients.length === 0 && (
        <div className="glass-panel py-12 text-center text-slate-400">暂无下载器</div>
      )}

      {!loading && clients.length > 0 && (
        <div className="space-y-3">
          {clients.map((c) => (
            <div
              key={c.id}
              className="glass-panel flex items-center justify-between gap-3"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-white">{c.name}</span>
                  <span className="rounded border border-white/10 bg-white/5 px-2 py-0.5 text-xs text-slate-400">
                    {c.type}
                  </span>
                  {c.is_default && (
                    <span className="rounded bg-primary-400/20 px-2 py-0.5 text-xs text-primary-400">
                      默认
                    </span>
                  )}
                  {!c.enabled && (
                    <span className="rounded bg-slate-500/30 px-2 py-0.5 text-xs text-slate-300">
                      已禁用
                    </span>
                  )}
                </div>
                <div className="mt-1 truncate text-xs text-slate-400">
                  {c.url}
                  {c.username && ` · ${c.username}`}
                  {c.save_path && ` · ${c.save_path}`}
                </div>
              </div>
              <div className="flex shrink-0 gap-2">
                <button
                  onClick={() => onTest(c.id)}
                  className="rounded border border-white/10 px-2 py-1 text-xs text-slate-300 hover:border-primary-400/40 hover:text-primary-400"
                >
                  <Send size={12} className="inline" /> 测试
                </button>
                <button
                  onClick={() => {
                    setEditing(c)
                    setShowForm(true)
                  }}
                  className="rounded border border-white/10 px-2 py-1 text-xs text-slate-300 hover:border-primary-400/40 hover:text-primary-400"
                >
                  <Pencil size={12} className="inline" /> 编辑
                </button>
                <button
                  onClick={() => onDelete(c)}
                  className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                >
                  <Trash2 size={12} className="inline" /> 删除
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showForm && (
        <ClientFormModal
          editing={editing}
          onClose={() => setShowForm(false)}
          onSaved={async () => {
            setShowForm(false)
            await refresh()
          }}
        />
      )}
    </div>
  )
}

function ClientFormModal({
  editing,
  onClose,
  onSaved,
}: {
  editing: DownloadClient | null
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [form, setForm] = useState<DownloadClientInput>(() => ({
    name: editing?.name ?? '',
    type: editing?.type ?? 'qbittorrent',
    url: editing?.url ?? '',
    username: editing?.username ?? '',
    password: '',
    save_path: editing?.save_path ?? '',
    is_default: editing?.is_default ?? false,
    enabled: editing?.enabled ?? true,
  }))
  const [saving, setSaving] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      if (editing) await downloadClientsAPI.update(editing.id, form)
      else await downloadClientsAPI.create(form)
      toast.success('已保存')
      await onSaved()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const update = <K extends keyof DownloadClientInput>(k: K, v: DownloadClientInput[K]) =>
    setForm((f) => ({ ...f, [k]: v }))

  const placeholder = (
    {
      qbittorrent: 'http://127.0.0.1:8080',
      aria2: 'http://127.0.0.1:6800/jsonrpc',
      transmission: 'http://127.0.0.1:9091/transmission/rpc',
    } as Record<DownloadClientType, string>
  )[form.type]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="glass-panel w-full max-w-lg max-h-[90vh] overflow-y-auto">
        <h2 className="mb-4 font-display text-xl font-semibold text-white">
          {editing ? '编辑下载器' : '添加下载器'}
        </h2>
        <form onSubmit={onSubmit} className="space-y-4">
          <Field label="名称">
            <input
              required
              className="input-base"
              value={form.name}
              onChange={(e) => update('name', e.target.value)}
            />
          </Field>
          <Field label="类型">
            <select
              className="input-base"
              value={form.type}
              onChange={(e) => update('type', e.target.value as DownloadClientType)}
            >
              <option value="qbittorrent">qBittorrent</option>
              <option value="aria2">Aria2 (JSON-RPC)</option>
              <option value="transmission">Transmission</option>
            </select>
          </Field>
          <Field label="URL">
            <input
              required
              className="input-base"
              placeholder={placeholder}
              value={form.url}
              onChange={(e) => update('url', e.target.value)}
            />
          </Field>
          {form.type !== 'aria2' && (
            <>
              <Field label="用户名">
                <input
                  className="input-base"
                  value={form.username ?? ''}
                  onChange={(e) => update('username', e.target.value)}
                />
              </Field>
              <Field label={editing ? '密码 (留空保持不变)' : '密码'}>
                <input
                  type="password"
                  className="input-base"
                  value={form.password ?? ''}
                  onChange={(e) => update('password', e.target.value)}
                />
              </Field>
            </>
          )}
          {form.type === 'aria2' && (
            <Field label="RPC Token (作为密码字段保存)">
              <input
                type="password"
                className="input-base"
                value={form.password ?? ''}
                onChange={(e) => update('password', e.target.value)}
              />
            </Field>
          )}
          <Field label="默认保存路径">
            <input
              className="input-base"
              value={form.save_path ?? ''}
              onChange={(e) => update('save_path', e.target.value)}
            />
          </Field>
          <div className="flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-sm text-slate-300">
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary-400"
                checked={form.is_default}
                onChange={(e) => update('is_default', e.target.checked)}
              />
              设为默认
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-300">
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary-400"
                checked={form.enabled}
                onChange={(e) => update('enabled', e.target.checked)}
              />
              启用
            </label>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-white/10 px-4 py-2 text-sm text-slate-300 hover:bg-white/5"
            >
              取消
            </button>
            <button type="submit" disabled={saving} className="neon-button">
              {saving && <Loader2 size={16} className="animate-spin" />} 保存
            </button>
          </div>
        </form>
      </div>
    </div>
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
