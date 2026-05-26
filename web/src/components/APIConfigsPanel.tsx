import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Eye, KeyRound, Save, Trash2, X } from 'lucide-react'

import { apiConfigsAPI, type APIConfig } from '../api/api_configs'

// Compact inline-editable provider table for use inside AdminPage's "外部API" tab.
export function APIConfigsPanel() {
  const [items, setItems] = useState<APIConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<string | null>(null)

  const refresh = () =>
    apiConfigsAPI
      .list()
      .then(setItems)
      .finally(() => setLoading(false))

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <KeyRound className="h-5 w-5 text-brand-500" />
        <div>
          <p className="font-display text-lg font-semibold text-ink-600">外部 API 配置</p>
          <p className="text-xs text-ink-50">
            TMDb / Bangumi / TheTVDB / Fanart / OpenAI / Douban 密钥管理
            · AES-GCM 加密存储
          </p>
        </div>
      </div>

      {loading && (
        <p className="py-6 text-center text-sm text-sand-500">加载中…</p>
      )}

      {!loading && (
        <div className="glass-panel overflow-hidden">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-white/5 text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="px-4 py-3">服务</th>
                <th className="px-4 py-3">密钥</th>
                <th className="px-4 py-3">状态</th>
                <th className="px-4 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) =>
                editing === item.provider ? (
                  <EditingRow
                    key={item.id}
                    item={item}
                    onCancel={() => setEditing(null)}
                    onSaved={() => {
                      setEditing(null)
                      refresh()
                    }}
                  />
                ) : (
                  <tr
                    key={item.id}
                    className="border-t border-white/5 transition hover:bg-white/[0.02]"
                  >
                    <td className="px-4 py-3">
                      <p className="font-medium text-ink-600">{item.provider}</p>
                      {item.description && (
                        <p className="text-xs text-sand-500">{item.description}</p>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs">
                      {item.has_key ? (
                        <span className="text-brand-500">{item.masked_key}</span>
                      ) : (
                        <span className="text-sand-400">未配置</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {item.has_key ? (
                        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-400/10 px-2 py-0.5 text-xs text-emerald-400">
                          已配置
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-full bg-sand-300/40 px-2 py-0.5 text-xs text-sand-500">
                          未配置
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => setEditing(item.provider)}
                          className="rounded p-1.5 text-ink-50 transition hover:bg-white/5 hover:text-white"
                          title="编辑"
                        >
                          <Save size={14} />
                        </button>
                        <button
                          onClick={() => {
                            toast(
                              `已配置 ${item.has_key ? '✓' : '✗'} 密钥 (在线测试请用对应功能页面)`,
                            )
                          }}
                          className="rounded p-1.5 text-ink-50 transition hover:bg-white/5 hover:text-ink-200"
                          title="查看状态"
                        >
                          <Eye size={14} />
                        </button>
                        {item.has_key && (
                          <button
                            onClick={async () => {
                              if (!confirm(`确定清除 ${item.provider} 的 API Key?`)) return
                              await apiConfigsAPI.remove(item.provider)
                              toast.success('已清除')
                              refresh()
                            }}
                            className="rounded p-1.5 text-ink-50 transition hover:bg-red-400/10 hover:text-red-400"
                            title="清除密钥"
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function EditingRow({
  item,
  onCancel,
  onSaved,
}: {
  item: APIConfig
  onCancel: () => void
  onSaved: () => void
}) {
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
      onSaved()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <tr className="border-t border-white/5 bg-primary-400/5">
      <td colSpan={4} className="px-4 py-3">
        <form onSubmit={submit} className="flex flex-wrap items-end gap-3">
          <span className="text-sm font-medium text-ink-600">{item.provider}</span>
          <label className="flex-1 text-xs text-ink-50">
            API Key
            <input
              className="input-base mt-1"
              type="password"
              placeholder={item.has_key ? '•••••••••••• (留空保留原值)' : '输入密钥'}
              value={apiKey}
              onChange={(e) => setAPIKey(e.target.value)}
            />
          </label>
          <label className="flex-1 text-xs text-ink-50">
            Base URL
            <input
              className="input-base mt-1"
              placeholder="https://api.themoviedb.org/3"
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
            />
          </label>
          <label className="flex items-center gap-2 text-xs text-ink-50">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            启用
          </label>
          <button type="submit" disabled={saving} className="neon-button !px-3 !py-1.5 !text-xs">
            <Save size={12} /> 保存
          </button>
          <button
            type="button"
            onClick={onCancel}
            className="rounded border border-sand-400/30 px-2 py-1.5 text-xs text-ink-50 hover:text-white"
          >
            <X size={12} />
          </button>
        </form>
      </td>
    </tr>
  )
}
