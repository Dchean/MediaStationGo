import { FormEvent, useEffect, useState } from 'react'
import {
  ChevronDown,
  ChevronUp,
  KeySquare,
  Loader2,
  Plus,
  ShieldOff,
  Trash2,
} from 'lucide-react'
import toast from 'react-hot-toast'

import {
  licenseAPI,
  type GenerateKeyInput,
  type LicenseActivation,
  type LicenseKey,
} from '../api/license'

// LicensePage is the admin UI for issuing / revoking license keys and
// inspecting activations. Mirrors the Vue LicenseTab inside Settings.
export function LicensePage() {
  const [keys, setKeys] = useState<LicenseKey[]>([])
  const [loading, setLoading] = useState(true)
  const [showGen, setShowGen] = useState(false)
  const [openKey, setOpenKey] = useState<string | null>(null)
  const [activations, setActivations] = useState<Record<string, LicenseActivation[]>>({})

  const refresh = async () => {
    setLoading(true)
    try {
      setKeys(await licenseAPI.list())
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const toggleOpen = async (k: LicenseKey) => {
    if (openKey === k.id) {
      setOpenKey(null)
      return
    }
    setOpenKey(k.id)
    if (!activations[k.id]) {
      try {
        const acts = await licenseAPI.listActivations(k.id)
        setActivations((a) => ({ ...a, [k.id]: acts }))
      } catch {
        toast.error('加载激活记录失败')
      }
    }
  }

  const onRevoke = async (k: LicenseKey) => {
    if (!confirm(`确定吊销 ${k.key.slice(0, 14)}…?`)) return
    try {
      await licenseAPI.revoke(k.id)
      toast.success('已吊销')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '吊销失败'
      toast.error(msg)
    }
  }

  const onUnbind = async (a: LicenseActivation) => {
    if (!confirm(`解绑设备 ${a.device_name || a.device_id}?`)) return
    try {
      await licenseAPI.unbind(a.id)
      toast.success('已解绑')
      const acts = await licenseAPI.listActivations(a.key_id)
      setActivations((all) => ({ ...all, [a.key_id]: acts }))
    } catch {
      toast.error('解绑失败')
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-fuchsia-400/10 text-fuchsia-300">
            <KeySquare size={20} />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-white">许可证管理</h1>
            <p className="text-sm text-slate-400">
              生成密钥 · 绑定设备 · 心跳监控 · 吊销
            </p>
          </div>
        </div>
        <button onClick={() => setShowGen(true)} className="neon-button">
          <Plus size={16} /> 生成新密钥
        </button>
      </div>

      {loading && (
        <div className="flex justify-center py-12 text-slate-400">
          <Loader2 className="animate-spin" />
        </div>
      )}

      {!loading && keys.length === 0 && (
        <div className="glass-panel py-12 text-center text-slate-400">暂无密钥</div>
      )}

      {!loading && keys.length > 0 && (
        <div className="space-y-3">
          {keys.map((k) => (
            <div key={k.id} className="glass-panel space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <code className="rounded bg-white/5 px-2 py-0.5 text-xs text-primary-400">
                      {k.key}
                    </code>
                    <span className="rounded border border-white/10 bg-white/5 px-2 py-0.5 text-xs text-slate-400">
                      {k.plan}
                    </span>
                    {k.revoked && (
                      <span className="rounded bg-red-400/20 px-2 py-0.5 text-xs text-red-400">
                        已吊销
                      </span>
                    )}
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    {k.customer && `客户: ${k.customer} · `}
                    最多 {k.max_activations} 设备 ·
                    {k.expires_at ? ` ${new Date(k.expires_at).toLocaleDateString()} 到期` : ' 永久'}
                  </div>
                </div>
                <div className="flex shrink-0 gap-2">
                  <button
                    onClick={() => toggleOpen(k)}
                    className="rounded border border-white/10 px-2 py-1 text-xs text-slate-300 hover:border-primary-400/40 hover:text-primary-400"
                  >
                    {openKey === k.id ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
                    {' '}激活记录
                  </button>
                  {!k.revoked && (
                    <button
                      onClick={() => onRevoke(k)}
                      className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                    >
                      <ShieldOff size={12} className="inline" /> 吊销
                    </button>
                  )}
                </div>
              </div>
              {openKey === k.id && (
                <div className="space-y-2 border-t border-white/5 pt-3">
                  {(activations[k.id] ?? []).length === 0 && (
                    <p className="text-sm text-slate-500">暂无激活记录</p>
                  )}
                  {(activations[k.id] ?? []).map((a) => (
                    <div
                      key={a.id}
                      className="flex items-center justify-between rounded border border-white/5 bg-white/5 px-3 py-2 text-sm"
                    >
                      <div className="min-w-0">
                        <div className="text-white">
                          {a.device_name || a.device_id}
                          {a.unbound_at && (
                            <span className="ml-2 text-xs text-slate-500">
                              (已解绑 {new Date(a.unbound_at).toLocaleDateString()})
                            </span>
                          )}
                        </div>
                        <div className="text-xs text-slate-400">
                          {a.ip} · 心跳{' '}
                          {a.heartbeat_at
                            ? new Date(a.heartbeat_at).toLocaleString()
                            : '未上报'}
                        </div>
                      </div>
                      {!a.unbound_at && (
                        <button
                          onClick={() => onUnbind(a)}
                          className="shrink-0 rounded border border-white/10 px-2 py-1 text-xs text-slate-300 hover:border-red-400/40 hover:text-red-400"
                        >
                          <Trash2 size={12} className="inline" /> 解绑
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {showGen && (
        <GenerateModal
          onClose={() => setShowGen(false)}
          onCreated={async () => {
            setShowGen(false)
            await refresh()
          }}
        />
      )}
    </div>
  )
}

function GenerateModal({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: () => void | Promise<void>
}) {
  const [form, setForm] = useState<GenerateKeyInput>({
    customer: '',
    plan: 'basic',
    max_activations: 1,
    expires_at: '',
    notes: '',
  })
  const [saving, setSaving] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const k = await licenseAPI.generate({
        ...form,
        expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : undefined,
      })
      toast.success(`已生成: ${k.key}`)
      await onCreated()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '生成失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="glass-panel w-full max-w-md">
        <h2 className="mb-4 font-display text-xl font-semibold text-white">生成密钥</h2>
        <form onSubmit={onSubmit} className="space-y-3">
          <label className="block">
            <span className="mb-1 block text-sm text-slate-300">客户</span>
            <input
              className="input-base"
              value={form.customer ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, customer: e.target.value }))}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-sm text-slate-300">套餐</span>
            <select
              className="input-base"
              value={form.plan ?? 'basic'}
              onChange={(e) => setForm((f) => ({ ...f, plan: e.target.value }))}
            >
              <option value="basic">basic</option>
              <option value="pro">pro</option>
              <option value="enterprise">enterprise</option>
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-sm text-slate-300">最多设备数</span>
            <input
              type="number"
              min={1}
              className="input-base"
              value={form.max_activations ?? 1}
              onChange={(e) =>
                setForm((f) => ({ ...f, max_activations: Number(e.target.value) }))
              }
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-sm text-slate-300">到期日期 (空 = 永久)</span>
            <input
              type="date"
              className="input-base"
              value={form.expires_at ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, expires_at: e.target.value }))}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-sm text-slate-300">备注</span>
            <textarea
              rows={2}
              className="input-base"
              value={form.notes ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, notes: e.target.value }))}
            />
          </label>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-white/10 px-4 py-2 text-sm text-slate-300 hover:bg-white/5"
            >
              取消
            </button>
            <button type="submit" disabled={saving} className="neon-button">
              {saving && <Loader2 size={16} className="animate-spin" />} 生成
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
