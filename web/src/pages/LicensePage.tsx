import { FormEvent, useCallback, useEffect, useState } from 'react'
import {
  CheckCircle2,
  KeySquare,
  Link2,
  Loader2,
  RefreshCw,
  XCircle,
} from 'lucide-react'
import toast from 'react-hot-toast'

import { licenseAPI, type LicenseStatus } from '../api/license'

// ── Helpers ──

function fmtDate(iso: string | null | undefined): string {
  if (!iso) return '永久'
  return new Date(iso).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function fmtDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('zh-CN')
}

// ── Page ──

export function LicensePage() {
  const [status, setStatus] = useState<LicenseStatus | null>(null)
  const [loadingStatus, setLoadingStatus] = useState(true)
  const [bindKey, setBindKey] = useState('')
  const [binding, setBinding] = useState(false)

  const refreshStatus = useCallback(async () => {
    setLoadingStatus(true)
    try {
      const s = await licenseAPI.status()
      setStatus(s)
    } catch {
      setStatus({ active: false })
    } finally {
      setLoadingStatus(false)
    }
  }, [])

  useEffect(() => {
    refreshStatus()
  }, [refreshStatus])

  const onBind = async (e: FormEvent) => {
    e.preventDefault()
    const key = bindKey.trim()
    if (!key) {
      toast.error('请输入许可证密钥')
      return
    }
    setBinding(true)
    try {
      const activation = await licenseAPI.bind(key)
      toast.success('许可证绑定成功!')
      setBindKey('')
      // Optimistically update status
      setStatus({
        active: true,
        activation,
        message: '已激活',
      })
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '绑定失败，请检查密钥是否正确'
      toast.error(msg)
    } finally {
      setBinding(false)
    }
  }

  const onHeartbeat = async () => {
    try {
      await licenseAPI.heartbeat()
      toast.success('心跳上报成功')
    } catch {
      toast.error('心跳上报失败')
    }
  }

  // ── Derive display values ──
  const active = status?.active === true
  const activation = status?.activation
  const isExpired =
    activation?.expires_at != null && new Date(activation.expires_at).getTime() < Date.now()

  return (
    <div className="mx-auto max-w-2xl space-y-8">
      {/* ── Header ── */}
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-fuchsia-400/10 text-fuchsia-300">
          <KeySquare size={20} />
        </div>
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">许可证</h1>
          <p className="text-sm text-ink-50">绑定授权密钥以解锁全部功能</p>
        </div>
      </div>

      {/* ── Bind Form ── */}
      <div className="glass-panel space-y-4">
        <div className="flex items-center gap-2">
          <Link2 size={18} className="text-brand-500" />
          <h2 className="font-display text-lg font-semibold text-ink-600">绑定许可证</h2>
        </div>
        <form onSubmit={onBind} className="flex gap-3">
          <input
            className="input-base flex-1 font-mono text-sm tracking-wider"
            placeholder="MS-XXXX-XXXX-XXXX"
            value={bindKey}
            onChange={(e) => setBindKey(e.target.value)}
            disabled={binding}
          />
          <button type="submit" disabled={binding} className="neon-button shrink-0">
            {binding ? <Loader2 size={16} className="animate-spin" /> : <Link2 size={16} />}
            绑定
          </button>
        </form>
        <p className="text-xs text-sand-500">
          输入从授权服务器获取的许可证密钥，激活后即可使用所有高级功能。
        </p>
      </div>

      {/* ── License Status ── */}
      {loadingStatus ? (
        <div className="flex justify-center py-8 text-ink-50">
          <Loader2 className="animate-spin" />
        </div>
      ) : (
        <div className="glass-panel space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {active && !isExpired ? (
                <CheckCircle2 size={18} className="text-emerald-400" />
              ) : (
                <XCircle size={18} className="text-sand-500" />
              )}
              <h2 className="font-display text-lg font-semibold text-ink-600">当前状态</h2>
            </div>
            <div className="flex gap-2">
              <button
                onClick={refreshStatus}
                className="rounded border border-white/10 p-2 text-ink-50 hover:bg-white/5 hover:text-white"
                title="刷新状态"
              >
                <RefreshCw size={14} />
              </button>
              {active && (
                <button
                  onClick={onHeartbeat}
                  className="rounded border border-primary-400/30 px-3 py-1.5 text-xs text-brand-500 hover:bg-primary-400/10"
                >
                  心跳上报
                </button>
              )}
            </div>
          </div>

          {!active && (
            <div className="space-y-3 rounded-lg border border-white/5 bg-white/[0.02] p-4 text-center">
              <p className="text-ink-50">
                尚未绑定许可证。请在上方输入密钥完成激活。
              </p>
            </div>
          )}

          {active && activation && (
            <div className="grid grid-cols-2 gap-3">
              <StatusBadge label="密钥" value={activation.key ?? activation.key_id} mono />
              <StatusBadge
                label="套餐"
                value={activation.plan ?? 'standard'}
                className={
                  activation.plan === 'enterprise'
                    ? 'text-amber-300'
                    : activation.plan === 'pro'
                      ? 'text-brand-500'
                      : 'text-ink-100'
                }
              />
              <StatusBadge label="设备名称" value={activation.device_name || 'Web Client'} />
              <StatusBadge label="设备数" value={`${activation.max_activations ?? 1} 台`} />
              <StatusBadge label="绑定时间" value={fmtDate(activation.created_at)} />
              <StatusBadge
                label="到期时间"
                value={fmtDate(activation.expires_at)}
                className={isExpired ? 'text-red-400' : undefined}
              />
              <StatusBadge label="最近心跳" value={fmtDateTime(activation.heartbeat_at)} />
              <StatusBadge label="客户端 IP" value={activation.ip ?? '—'} />
            </div>
          )}

          {isExpired && (
            <div className="rounded-lg border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
              此许可证已过期，部分功能可能受限。请获取新的许可证密钥。
            </div>
          )}
        </div>
      )}

      {/* ── Pro tip ── */}
      {!active && !loadingStatus && (
        <div className="rounded-xl border border-white/5 bg-white/[0.02] p-5 text-center">
          <p className="text-sm text-sand-500">
            需要获取许可证？请联系管理员获取 MediaStationGo 授权密钥。
          </p>
          <p className="mt-1 text-xs text-sand-400">
            授权服务器地址可在系统设置中配置
          </p>
        </div>
      )}
    </div>
  )
}

// ── Reusable status row ──

function StatusBadge({
  label,
  value,
  mono,
  className,
}: {
  label: string
  value: string
  mono?: boolean
  className?: string
}) {
  return (
    <div className="rounded-lg border border-white/5 bg-white/[0.02] p-3">
      <p className="mb-0.5 text-xs text-sand-500">{label}</p>
      <p className={`text-sm text-ink-600 ${mono ? 'font-mono' : ''} ${className ?? ''}`}>
        {value}
      </p>
    </div>
  )
}
