import { FormEvent, useEffect, useState } from 'react'
import { Bell, Loader2, Pencil, Plus, Send, Trash2 } from 'lucide-react'
import toast from 'react-hot-toast'

import {
  notifyChannelsAPI,
  type NotifyChannelInput,
} from '../api/notify_channels'
import { confirmAction } from '../components/ConfirmDialog'
import type { NotifyChannel } from '../types'

// NotifyChannelsPage replaces the Vue NotifyTab. Operators can register
// multiple Telegram bots / Bark devices / WeChat keys / Webhooks and
// fire a test notification on demand.
export function NotifyChannelsPage() {
  const [channels, setChannels] = useState<NotifyChannel[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<NotifyChannel | null>(null)
  const [showForm, setShowForm] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      setChannels(await notifyChannelsAPI.list())
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const onTest = async (id: string) => {
    try {
      await notifyChannelsAPI.test(id)
      toast.success('测试消息已发送')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '发送失败'
      toast.error(msg)
    }
  }

  const onDelete = async (ch: NotifyChannel) => {
    if (!(await confirmAction({ title: '删除通知渠道', message: `确定删除「${ch.name}」?`, confirmText: '删除' }))) return
    try {
      await notifyChannelsAPI.remove(ch.id)
      toast.success('已删除')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '删除失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-400/10 text-amber-300">
            <Bell size={20} />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-ink-600">通知渠道</h1>
            <p className="text-sm text-ink-50">
              配置 Telegram / Bark / 企业微信 / Webhook 多通道推送
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
          <Plus size={16} /> 添加渠道
        </button>
      </div>

      {loading && (
        <div className="flex justify-center py-12 text-ink-50">
          <Loader2 className="animate-spin" />
        </div>
      )}

      {!loading && channels.length === 0 && (
        <div className="glass-panel py-12 text-center text-ink-50">暂无通知渠道</div>
      )}

      {!loading && channels.length > 0 && (
        <div className="space-y-3">
          {channels.map((ch) => (
            <ChannelCard
              key={ch.id}
              channel={ch}
              onTest={() => onTest(ch.id)}
              onEdit={() => {
                setEditing(ch)
                setShowForm(true)
              }}
              onDelete={() => onDelete(ch)}
            />
          ))}
        </div>
      )}

      {showForm && (
        <ChannelFormModal
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

const TYPE_LABELS: Record<NotifyChannel['type'], string> = {
  telegram: 'Telegram',
  wechat: '企业微信',
  bark: 'Bark',
  webhook: 'Webhook',
  email: 'Email',
}

function ChannelCard({
  channel,
  onTest,
  onEdit,
  onDelete,
}: {
  channel: NotifyChannel
  onTest: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const summary = channelSummary(channel)
  return (
    <div className="glass-panel flex items-center justify-between gap-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium text-ink-600">{channel.name}</span>
          <span className="rounded-lg border border-gray-200 bg-gray-50 px-2 py-0.5 text-xs text-ink-50">
            {TYPE_LABELS[channel.type] ?? channel.type}
          </span>
          {!channel.enabled && (
            <span className="rounded-lg bg-sand-500/30 px-2 py-0.5 text-xs text-ink-100">已禁用</span>
          )}
        </div>
        <div className="mt-1 truncate text-xs text-ink-50">{summary}</div>
      </div>
      <div className="flex shrink-0 gap-2">
        <button
          onClick={onTest}
          className="rounded-lg border border-gray-200 px-2 py-1 text-xs text-ink-100 hover:border-primary-400/40 hover:text-brand-500"
        >
          <Send size={12} className="inline" /> 测试
        </button>
        <button
          onClick={onEdit}
          className="rounded-lg border border-gray-200 px-2 py-1 text-xs text-ink-100 hover:border-primary-400/40 hover:text-brand-500"
        >
          <Pencil size={12} className="inline" /> 编辑
        </button>
        <button
          onClick={onDelete}
          className="rounded-lg border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
        >
          <Trash2 size={12} className="inline" /> 删除
        </button>
      </div>
    </div>
  )
}

function channelSummary(ch: NotifyChannel): string {
  const cfg = ch.config ?? {}
  switch (ch.type) {
    case 'telegram':
      return `Bot ${cfg.bot_token ? '已配置' : '未配置'} → 管理员 ${cfg.admin_user_ids ?? '-'} · 群组 ${cfg.group_chat_id ?? '-'} · 频道 ${cfg.channel_chat_id ?? '-'}`
    case 'wechat':
      return `SendKey ${String(cfg.sendkey ?? '').slice(0, 10)}…`
    case 'bark':
      return `Device ${String(cfg.device_key ?? '').slice(0, 10)}…`
    case 'webhook':
      return `${cfg.method ?? 'POST'} ${cfg.url ?? ''}`
    case 'email':
      return `SMTP ${cfg.smtp_host ?? '-'}:${cfg.smtp_port ?? '-'} → ${cfg.to ?? '-'}`
    default:
      return ''
  }
}

// ─── Form Modal ─────────────────────────────────────────────────────────────

const EMPTY_CONFIG: Record<NotifyChannel['type'], Record<string, string>> = {
  telegram: {
    bot_token: '',
    admin_user_ids: '',
    group_chat_id: '',
    channel_chat_id: '',
    api_base_url: '',
    proxy_url: '',
  },
  wechat: { sendkey: '' },
  bark: { device_key: '', server: '' },
  webhook: { url: '', method: 'POST', headers: '', body_template: '' },
  email: { smtp_host: '', smtp_port: '465', username: '', password: '', from: '', to: '', tls: 'true' },
}

function normalizeInitialConfig(type: NotifyChannel['type'], raw: Record<string, unknown>): Record<string, string> {
  const base = { ...EMPTY_CONFIG[type] }
  for (const [key, value] of Object.entries(raw ?? {})) {
    base[key] = String(value ?? '')
  }
  if (type === 'telegram' && !base.group_chat_id && !base.channel_chat_id && base.chat_id?.startsWith('-')) {
    base.group_chat_id = base.chat_id
  }
  delete base.chat_id
  return base
}

function ChannelFormModal({
  editing,
  onClose,
  onSaved,
}: {
  editing: NotifyChannel | null
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [name, setName] = useState(editing?.name ?? '')
  const [type, setType] = useState<NotifyChannel['type']>(
    editing?.type ?? 'telegram',
  )
  const [config, setConfig] = useState<Record<string, string>>(
    normalizeInitialConfig(editing?.type ?? 'telegram', editing?.config ?? {}),
  )
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [saving, setSaving] = useState(false)

  const onTypeChange = (t: NotifyChannel['type']) => {
    setType(t)
    setConfig({ ...EMPTY_CONFIG[t] })
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (type === 'telegram') {
      if (!String(config.bot_token ?? '').trim()) {
        toast.error('请填写 Telegram Bot Token')
        return
      }
      if (!String(config.admin_user_ids ?? '').trim()) {
        toast.error('请填写管理员 Telegram ID')
        return
      }
    }
    setSaving(true)
    try {
      const cleanedConfig = Object.fromEntries(
        Object.entries(config).map(([key, value]) => [key, String(value ?? '').trim()]),
      )
      delete cleanedConfig.chat_id
      const input: NotifyChannelInput = {
        name: name.trim(),
        type: type,
        config: cleanedConfig,
        enabled,
      }
      if (editing) {
        await notifyChannelsAPI.update(editing.id, input)
      } else {
        await notifyChannelsAPI.create(input)
      }
      toast.success('已保存')
      await onSaved()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const updateConfig = (k: string, v: string) =>
    setConfig((c) => ({ ...c, [k]: v }))

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="glass-panel w-full max-w-lg max-h-[90vh] overflow-y-auto">
        <h2 className="mb-4 font-display text-xl font-semibold text-ink-600">
          {editing ? '编辑通知渠道' : '添加通知渠道'}
        </h2>
        <form onSubmit={onSubmit} className="space-y-4">
          <Field label="名称">
            <input
              required
              className="input-base"
              placeholder="如: Telegram 通知"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>

          <Field label="渠道类型">
            <select
              className="input-base"
              value={type}
              onChange={(e) => onTypeChange(e.target.value as NotifyChannel['type'])}
            >
              <option value="telegram">Telegram</option>
              <option value="wechat">企业微信 / Server酱</option>
              <option value="bark">Bark (iOS)</option>
              <option value="webhook">Webhook</option>
              <option value="email">Email (SMTP)</option>
            </select>
          </Field>

          {type === 'telegram' && (
            <>
              <Field label="Bot Token">
                <input
                  required
                  className="input-base"
                  placeholder="123456:ABC-DEF…"
                  value={config.bot_token ?? ''}
                  onChange={(e) => updateConfig('bot_token', e.target.value)}
                />
              </Field>
              <Field label="管理员 Telegram ID">
                <input
                  required
                  className="input-base"
                  placeholder="多个用逗号分隔，如 123456789,987654321"
                  value={config.admin_user_ids ?? ''}
                  onChange={(e) => updateConfig('admin_user_ids', e.target.value)}
                />
              </Field>
              <Field label="绑定群组 ID">
                <input
                  className="input-base"
                  placeholder="选填，如 -1001234567890；填写后群组成员可唤醒/绑定"
                  value={config.group_chat_id ?? ''}
                  onChange={(e) => updateConfig('group_chat_id', e.target.value)}
                />
              </Field>
              <Field label="绑定频道 ID">
                <input
                  className="input-base"
                  placeholder="选填，如 -1009876543210；填写后频道成员可唤醒/绑定"
                  value={config.channel_chat_id ?? ''}
                  onChange={(e) => updateConfig('channel_chat_id', e.target.value)}
                />
              </Field>
              <Field label="Telegram API 地址 (可选)">
                <input
                  className="input-base"
                  placeholder="默认 https://api.telegram.org；反代可填 https://tg.example.com"
                  value={config.api_base_url ?? ''}
                  onChange={(e) => updateConfig('api_base_url', e.target.value)}
                />
              </Field>
              <Field label="Telegram 代理 (可选)">
                <input
                  className="input-base"
                  placeholder="如 http://172.17.0.1:7890 或 socks5://172.17.0.1:1080"
                  value={config.proxy_url ?? ''}
                  onChange={(e) => updateConfig('proxy_url', e.target.value)}
                />
              </Field>
              <div className="rounded-2xl border border-primary-400/15 bg-primary-400/5 px-4 py-3 text-xs leading-6 text-ink-50">
                群组 ID、频道 ID 均为选填，可填一个、两个都填，也可以不填。不填时只有管理员 Telegram ID 可以私聊 Bot 使用；填写后，对应群组/频道成员可唤醒 Bot、使用 <code>/start 用户名 密码</code> 绑定账号和隐藏成人目录。<code>/status</code>、<code>/search</code>、<code>/downloads</code>、<code>/stats</code> 仍仅管理员 Telegram ID 或已绑定的本地管理员可用。若测试通知超时，可填写反代 API 地址或代理地址。
              </div>
            </>
          )}

          {type === 'wechat' && (
            <Field label="SendKey">
              <input
                required
                className="input-base"
                placeholder="SCT…"
                value={config.sendkey ?? ''}
                onChange={(e) => updateConfig('sendkey', e.target.value)}
              />
            </Field>
          )}

          {type === 'bark' && (
            <>
              <Field label="设备 Key">
                <input
                  required
                  className="input-base"
                  value={config.device_key ?? ''}
                  onChange={(e) => updateConfig('device_key', e.target.value)}
                />
              </Field>
              <Field label="服务器地址 (可选)">
                <input
                  className="input-base"
                  placeholder="https://api.day.app"
                  value={config.server ?? ''}
                  onChange={(e) => updateConfig('server', e.target.value)}
                />
              </Field>
            </>
          )}

          {type === 'webhook' && (
            <>
              <Field label="URL">
                <input
                  required
                  className="input-base"
                  placeholder="https://example.com/notify"
                  value={config.url ?? ''}
                  onChange={(e) => updateConfig('url', e.target.value)}
                />
              </Field>
              <Field label="Method">
                <select
                  className="input-base"
                  value={config.method ?? 'POST'}
                  onChange={(e) => updateConfig('method', e.target.value)}
                >
                  <option value="POST">POST</option>
                  <option value="GET">GET</option>
                </select>
              </Field>
              <Field label="Headers (JSON)">
                <textarea
                  rows={2}
                  className="input-base font-mono text-xs"
                  placeholder='{"Content-Type":"application/json"}'
                  value={config.headers ?? ''}
                  onChange={(e) => updateConfig('headers', e.target.value)}
                />
              </Field>
              <Field label="Body 模板 (支持 {{title}} {{message}})">
                <textarea
                  rows={3}
                  className="input-base font-mono text-xs"
                  placeholder='{"title":"{{title}}","message":"{{message}}"}'
                  value={config.body_template ?? ''}
                  onChange={(e) => updateConfig('body_template', e.target.value)}
                />
              </Field>
            </>
          )}

          {type === 'email' && (
            <>
              <Field label="SMTP 地址">
                <input
                  required
                  className="input-base"
                  placeholder="smtp.gmail.com"
                  value={config.smtp_host ?? ''}
                  onChange={(e) => updateConfig('smtp_host', e.target.value)}
                />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="SMTP 端口">
                  <input
                    required
                    className="input-base"
                    placeholder="465"
                    value={config.smtp_port ?? ''}
                    onChange={(e) => updateConfig('smtp_port', e.target.value)}
                  />
                </Field>
                <Field label="TLS">
                  <select
                    className="input-base"
                    value={config.tls ?? 'true'}
                    onChange={(e) => updateConfig('tls', e.target.value)}
                  >
                    <option value="true">启用</option>
                    <option value="false">关闭</option>
                  </select>
                </Field>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <Field label="用户名">
                  <input
                    required
                    className="input-base"
                    value={config.username ?? ''}
                    onChange={(e) => updateConfig('username', e.target.value)}
                  />
                </Field>
                <Field label="密码">
                  <input
                    type="password"
                    required
                    className="input-base"
                    value={config.password ?? ''}
                    onChange={(e) => updateConfig('password', e.target.value)}
                  />
                </Field>
              </div>
              <Field label="发件人">
                <input
                  required
                  className="input-base"
                  placeholder="noreply@example.com"
                  value={config.from ?? ''}
                  onChange={(e) => updateConfig('from', e.target.value)}
                />
              </Field>
              <Field label="收件人 (多个用逗号分隔)">
                <input
                  required
                  className="input-base"
                  placeholder="user@example.com"
                  value={config.to ?? ''}
                  onChange={(e) => updateConfig('to', e.target.value)}
                />
              </Field>
            </>
          )}

          <label className="flex cursor-pointer items-center gap-2 text-sm text-ink-100">
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
              onClick={onClose}
              className="rounded-lg border border-gray-200 px-4 py-2 text-sm text-ink-100 hover:bg-gray-50"
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
      <span className="mb-1 block text-sm text-ink-100">{label}</span>
      {children}
    </label>
  )
}
