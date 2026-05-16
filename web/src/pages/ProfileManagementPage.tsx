import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Loader2, Pencil, Plus, Trash2, UserCog } from 'lucide-react'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import { playProfilesAPI, type PlayProfileInput } from '../api/play_profiles'
import { useAuthStore } from '../stores/auth'
import type { Library, PlayProfile } from '../types'

// ProfileManagementPage replicates the Vue ProfileManagementView. It
// lets a user (or admin) define multiple "viewing personas" with
// different content-rating gates, library access, and player defaults.
//
// All persistence is real: data is written to /api/play-profiles which
// is backed by the Go PlayProfileService.
export function ProfileManagementPage() {
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const userID = useAuthStore((s) => s.user?.id ?? '')

  const [profiles, setProfiles] = useState<PlayProfile[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<PlayProfile | null>(null)
  const [showForm, setShowForm] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      const [p, l] = await Promise.all([
        playProfilesAPI.list(isAdmin),
        libraryAPI.list().catch(() => [] as Library[]),
      ])
      setProfiles(p)
      setLibraries(l)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [isAdmin])

  const onDelete = async (p: PlayProfile) => {
    if (!confirm(`确定删除 Profile「${p.name}」?`)) return
    try {
      await playProfilesAPI.remove(p.id)
      toast.success('已删除')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '删除失败'
      toast.error(msg)
    }
  }

  const openCreate = () => {
    setEditing(null)
    setShowForm(true)
  }

  const openEdit = (p: PlayProfile) => {
    setEditing(p)
    setShowForm(true)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-400/10 text-purple-400">
            <UserCog size={20} />
          </div>
          <div>
            <h1 className="font-display text-3xl font-bold text-white">观影 Profile</h1>
            <p className="text-sm text-slate-400">
              为不同场景(儿童 / 影院 / 成人)定义独立的内容分级和媒体库访问规则
            </p>
          </div>
        </div>
        <button onClick={openCreate} className="neon-button">
          <Plus size={16} /> 创建 Profile
        </button>
      </div>

      {loading && (
        <div className="flex justify-center py-12 text-slate-400">
          <Loader2 className="animate-spin" />
        </div>
      )}

      {!loading && profiles.length === 0 && (
        <div className="glass-panel py-12 text-center">
          <div className="mb-2 text-4xl">👤</div>
          <p className="font-medium text-white">暂无 Profile</p>
          <p className="text-sm text-slate-400">点击右上角"创建 Profile"开始</p>
        </div>
      )}

      {!loading && profiles.length > 0 && (
        <div className="grid gap-3">
          {profiles.map((p) => (
            <ProfileCard
              key={p.id}
              profile={p}
              libraries={libraries}
              onEdit={() => openEdit(p)}
              onDelete={() => onDelete(p)}
            />
          ))}
        </div>
      )}

      {showForm && (
        <ProfileFormModal
          editing={editing}
          libraries={libraries}
          defaultUserID={userID}
          isAdmin={isAdmin}
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

function ProfileCard({
  profile,
  libraries,
  onEdit,
  onDelete,
}: {
  profile: PlayProfile
  libraries: Library[]
  onEdit: () => void
  onDelete: () => void
}) {
  const libNames = useMemo(() => {
    if (!profile.allowed_library_ids?.length) return '全部'
    const idx = new Map(libraries.map((l) => [l.id, l.name]))
    return profile.allowed_library_ids.map((id) => idx.get(id) ?? id).join(', ')
  }, [profile, libraries])

  return (
    <div className="glass-panel flex items-start justify-between gap-4">
      <div className="flex min-w-0 items-start gap-3">
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-lg font-bold text-white"
          style={{
            background: `hsl(${(profile.name.charCodeAt(0) * 47) % 360}, 60%, 35%)`,
          }}
        >
          {profile.name[0]?.toUpperCase()}
        </div>
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-semibold text-white">{profile.name}</span>
            {profile.is_default && (
              <span className="rounded bg-primary-400/20 px-2 py-0.5 text-xs text-primary-400">
                默认
              </span>
            )}
            {profile.allow_adult && (
              <span className="rounded bg-red-400/20 px-2 py-0.5 text-xs text-red-400">
                成人内容
              </span>
            )}
            {profile.require_pin && (
              <span className="rounded bg-amber-400/20 px-2 py-0.5 text-xs text-amber-400">
                🔒 PIN
              </span>
            )}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-400">
            {profile.content_rating_limit && <span>分级: {profile.content_rating_limit}</span>}
            <span>媒体库: {libNames}</span>
            <span>用户: {profile.user_id.slice(0, 8)}…</span>
          </div>
          <div className="text-xs text-slate-500">
            观看时长 {Math.round(profile.total_watch_time / 3600)} 小时
            {profile.last_active_at && ` · 最近活跃 ${new Date(profile.last_active_at).toLocaleDateString()}`}
          </div>
        </div>
      </div>
      <div className="flex shrink-0 gap-2">
        <button
          onClick={onEdit}
          className="rounded border border-white/10 px-2 py-1 text-xs text-slate-300 hover:border-primary-400/40 hover:text-primary-400"
        >
          <Pencil size={12} className="inline" /> 编辑
        </button>
        <button
          onClick={onDelete}
          className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
        >
          <Trash2 size={12} className="inline" /> 删除
        </button>
      </div>
    </div>
  )
}

function ProfileFormModal({
  editing,
  libraries,
  defaultUserID,
  isAdmin,
  onClose,
  onSaved,
}: {
  editing: PlayProfile | null
  libraries: Library[]
  defaultUserID: string
  isAdmin: boolean
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [form, setForm] = useState<PlayProfileInput>(() => ({
    user_id: editing?.user_id ?? defaultUserID,
    name: editing?.name ?? '',
    is_default: editing?.is_default ?? false,
    content_rating_limit: editing?.content_rating_limit ?? '',
    allow_adult: editing?.allow_adult ?? false,
    require_pin: editing?.require_pin ?? false,
    pin: '',
    preferred_subtitle_lang: editing?.preferred_subtitle_lang ?? '',
    preferred_audio_lang: editing?.preferred_audio_lang ?? '',
    autoplay_next: editing?.autoplay_next ?? true,
    skip_intro: editing?.skip_intro ?? false,
    allowed_library_ids: editing?.allowed_library_ids ?? [],
  }))
  const [saving, setSaving] = useState(false)

  const update = (patch: Partial<PlayProfileInput>) => setForm((f) => ({ ...f, ...patch }))

  const toggleLib = (id: string) => {
    const next = form.allowed_library_ids.includes(id)
      ? form.allowed_library_ids.filter((x) => x !== id)
      : [...form.allowed_library_ids, id]
    update({ allowed_library_ids: next })
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      if (editing) {
        await playProfilesAPI.update(editing.id, form)
      } else {
        await playProfilesAPI.create(form)
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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="glass-panel w-full max-w-lg max-h-[90vh] overflow-y-auto">
        <h2 className="mb-4 font-display text-xl font-semibold text-white">
          {editing ? '编辑 Profile' : '创建 Profile'}
        </h2>
        <form onSubmit={onSubmit} className="space-y-4">
          {!editing && isAdmin && (
            <Field label="用户 ID">
              <input
                className="input-base"
                value={form.user_id ?? ''}
                onChange={(e) => update({ user_id: e.target.value })}
              />
            </Field>
          )}
          <Field label="名称">
            <input
              required
              className="input-base"
              maxLength={50}
              placeholder="如:儿童模式、影院模式"
              value={form.name}
              onChange={(e) => update({ name: e.target.value })}
            />
          </Field>

          <Toggle
            label="设为默认 Profile"
            hint="登录后默认使用"
            checked={form.is_default}
            onChange={(v) => update({ is_default: v })}
          />

          <Field label="内容分级限制">
            <select
              className="input-base"
              value={form.content_rating_limit ?? ''}
              onChange={(e) => update({ content_rating_limit: e.target.value })}
            >
              <option value="">不限制</option>
              <option value="G">G</option>
              <option value="PG">PG</option>
              <option value="PG-13">PG-13</option>
              <option value="R">R</option>
              <option value="NC-17">NC-17</option>
            </select>
          </Field>

          <Toggle
            label="允许成人内容"
            hint="开启后可访问 NSFW 媒体"
            checked={form.allow_adult}
            onChange={(v) => update({ allow_adult: v })}
          />

          <Toggle
            label="切换时需要 PIN"
            hint="切换到此 Profile 需输入 PIN 码"
            checked={form.require_pin}
            onChange={(v) => update({ require_pin: v })}
          />
          {form.require_pin && (
            <Field label="PIN 码 (4-8 位)">
              <input
                type="password"
                className="input-base"
                maxLength={8}
                placeholder={editing ? '留空保持不变' : '设置 PIN'}
                value={form.pin ?? ''}
                onChange={(e) => update({ pin: e.target.value })}
              />
            </Field>
          )}

          <div className="grid grid-cols-2 gap-3">
            <Field label="首选字幕">
              <select
                className="input-base"
                value={form.preferred_subtitle_lang ?? ''}
                onChange={(e) => update({ preferred_subtitle_lang: e.target.value })}
              >
                <option value="">跟随系统</option>
                <option value="zh">中文</option>
                <option value="zh-CN">简体中文</option>
                <option value="en">English</option>
                <option value="ja">日语</option>
              </select>
            </Field>
            <Field label="首选音轨">
              <select
                className="input-base"
                value={form.preferred_audio_lang ?? ''}
                onChange={(e) => update({ preferred_audio_lang: e.target.value })}
              >
                <option value="">跟随系统</option>
                <option value="zh">中文</option>
                <option value="ja">日语</option>
                <option value="en">English</option>
              </select>
            </Field>
          </div>

          <Toggle
            label="自动播放下一集"
            checked={form.autoplay_next}
            onChange={(v) => update({ autoplay_next: v })}
          />
          <Toggle
            label="自动跳过片头"
            checked={form.skip_intro}
            onChange={(v) => update({ skip_intro: v })}
          />

          <Field label="允许访问的媒体库 (空 = 全部)">
            <div className="flex flex-wrap gap-2">
              {libraries.map((l) => (
                <button
                  key={l.id}
                  type="button"
                  onClick={() => toggleLib(l.id)}
                  className={
                    'rounded-full border px-3 py-1 text-xs ' +
                    (form.allowed_library_ids.includes(l.id)
                      ? 'border-primary-400/60 bg-primary-400/10 text-primary-400'
                      : 'border-white/10 text-slate-400 hover:text-white')
                  }
                >
                  {l.name}
                </button>
              ))}
              {libraries.length === 0 && (
                <span className="text-xs text-slate-500">暂无媒体库</span>
              )}
            </div>
          </Field>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-white/10 px-4 py-2 text-sm text-slate-300 hover:bg-white/5"
            >
              取消
            </button>
            <button type="submit" disabled={saving} className="neon-button">
              {saving && <Loader2 size={16} className="animate-spin" />}
              保存
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

function Toggle({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint?: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-3 rounded-lg border border-white/5 bg-white/5 px-3 py-2">
      <div>
        <div className="text-sm text-white">{label}</div>
        {hint && <div className="text-xs text-slate-400">{hint}</div>}
      </div>
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="h-4 w-4 accent-primary-400"
      />
    </label>
  )
}
