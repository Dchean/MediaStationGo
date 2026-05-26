import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Eye, KeyRound, Save, Trash2 } from 'lucide-react'

import { apiConfigsAPI, type APIConfig } from '../api/api_configs'

// APIConfigsPage manages third-party API keys (TMDb / Bangumi / TheTVDB /
// Fanart / OpenAI / Douban). Plaintext keys are never returned by the
// backend — only a "abc1****wxyz" mask. The actual secret is encrypted
// in SQLite with AES-GCM keyed off the JWT secret.
export function APIConfigsPage() {
  const [items, setItems] = useState<APIConfig[]>([])
  const [loading, setLoading] = useState(true)

  const refresh = () =>
    apiConfigsAPI
      .list()
      .then(setItems)
      .finally(() => setLoading(false))

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <KeyRound className="h-6 w-6 text-brand-500" />
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">外部 API 配置</h1>
          <p className="text-sm text-ink-50">
            管理 TMDb / Bangumi / TheTVDB / Fanart / OpenAI / Douban 的密钥。
            后端使用 AES-GCM 加密存储,数据库泄漏时密钥仍然安全。
          </p>
        </div>
      </header>

      {loading && <p className="text-sand-500">加载中…</p>}

      <div className="space-y-3">
        {items.map((item) => (
          <ProviderCard key={item.id} item={item} onUpdated={refresh} />
        ))}
      </div>
    </div>
  )
}

function ProviderCard({ item, onUpdated }: { item: APIConfig; onUpdated: () => void }) {
  const [apiKey, setAPIKey] = useState('')
  const [baseURL, setBaseURL] = useState(item.base_url ?? '')
  const [enabled, setEnabled] = useState(item.enabled)
  const [saving, setSaving] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const patch: Record<string, unknown> = { base_url: baseURL, enabled }
      if (apiKey.trim()) patch.api_key = apiKey.trim()
      await apiConfigsAPI.update(item.provider, patch)
      toast.success(`${item.provider} 已保存`)
      setAPIKey('')
      onUpdated()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const testKey = async () => {
    // No /test endpoint yet — render a hint instead.
    toast(`已配置 ${item.has_key ? '✓' : '✗'} 密钥(在线测试请用对应功能页面)`)
  }

  const removeKey = async () => {
    if (!confirm(`确定清除 ${item.provider} 的 API Key?`)) return
    await apiConfigsAPI.remove(item.provider)
    toast.success('已清除')
    onUpdated()
  }

  return (
    <form onSubmit={submit} className="glass-panel grid gap-3 md:grid-cols-[1fr_2fr]">
      <div>
        <p className="font-display text-lg font-semibold text-ink-600">{item.provider}</p>
        {item.description && (
          <p className="text-xs text-ink-50">{item.description}</p>
        )}
        <p className="mt-2 text-xs text-sand-500">
          状态: {item.has_key ? <span className="text-emerald-400">已配置</span> : <span className="text-sand-500">未配置</span>}
          {item.has_key && (
            <span className="ml-2 font-mono text-brand-500">{item.masked_key}</span>
          )}
        </p>
      </div>
      <div className="space-y-2">
        <label className="block text-xs text-ink-50">
          API Key (留空保留原值)
          <input
            className="input-base mt-1"
            type="password"
            placeholder={item.has_key ? '••••••••••••' : '尚未配置'}
            value={apiKey}
            onChange={(e) => setAPIKey(e.target.value)}
          />
        </label>
        <label className="block text-xs text-ink-50">
          Base URL (可选)
          <input
            className="input-base mt-1"
            placeholder="https://api.themoviedb.org/3"
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
          />
        </label>
        <label className="inline-flex items-center gap-2 text-xs text-ink-50">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          启用
        </label>
        <div className="flex gap-2">
          <button type="submit" disabled={saving} className="neon-button !text-xs">
            <Save size={12} /> 保存
          </button>
          <button
            type="button"
            onClick={testKey}
            className="neon-button !text-xs !border-sand-400/40 !text-ink-100"
          >
            <Eye size={12} /> 状态
          </button>
          {item.has_key && (
            <button
              type="button"
              onClick={removeKey}
              className="neon-button !text-xs !border-red-400/40 !text-red-400"
            >
              <Trash2 size={12} /> 清除
            </button>
          )}
        </div>
      </div>
    </form>
  )
}
