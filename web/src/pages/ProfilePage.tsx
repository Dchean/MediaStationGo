import { FormEvent, useState } from 'react'
import toast from 'react-hot-toast'
import { KeyRound, Save } from 'lucide-react'

import { authAPI } from '../api/auth'
import { profileAPI } from '../api/profile'
import { useAuthStore } from '../stores/auth'

export function ProfilePage() {
  const user = useAuthStore((s) => s.user)
  const setUser = useAuthStore((s) => s.setUser)

  const [email, setEmail] = useState(user?.email ?? '')
  const [avatar, setAvatar] = useState(user?.avatar_url ?? '')
  const [oldPwd, setOldPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')

  const onProfile = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const u = await profileAPI.update({ email, avatar_url: avatar })
      setUser(u)
      toast.success('资料已更新')
    } catch {
      toast.error('保存失败')
    }
  }

  const onPwd = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await authAPI.changePassword(oldPwd, newPwd)
      toast.success('密码已更新')
      setOldPwd('')
      setNewPwd('')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '密码更新失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-ink-600">个人资料</h1>

      <form onSubmit={onProfile} className="glass-panel space-y-4">
        <h2 className="font-display text-lg font-semibold text-ink-600">基本信息</h2>
        <Field label="用户名">
          <input className="input-base" value={user?.username ?? ''} disabled />
        </Field>
        <Field label="角色">
          <input className="input-base" value={user?.role ?? ''} disabled />
        </Field>
        <Field label="电子邮箱">
          <input
            type="email"
            className="input-base"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>
        <Field label="头像 URL">
          <input
            className="input-base"
            value={avatar}
            onChange={(e) => setAvatar(e.target.value)}
          />
        </Field>
        <button type="submit" className="neon-button">
          <Save size={16} /> 保存
        </button>
      </form>

      <form onSubmit={onPwd} className="glass-panel space-y-4">
        <h2 className="font-display text-lg font-semibold text-ink-600">修改密码</h2>
        <Field label="当前密码">
          <input
            required
            type="password"
            className="input-base"
            value={oldPwd}
            onChange={(e) => setOldPwd(e.target.value)}
            autoComplete="current-password"
          />
        </Field>
        <Field label="新密码">
          <input
            required
            type="password"
            className="input-base"
            minLength={6}
            value={newPwd}
            onChange={(e) => setNewPwd(e.target.value)}
            autoComplete="new-password"
          />
        </Field>
        <button type="submit" className="neon-button">
          <KeyRound size={16} /> 更新密码
        </button>
      </form>
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
