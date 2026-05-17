import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import toast from 'react-hot-toast'
import { Film } from 'lucide-react'

import { AppFooter } from '../components/AppFooter'
import { authAPI } from '../api/auth'
import { useAuthStore } from '../stores/auth'

// Single-form login screen. The first install seeds admin/admin123 — we
// surface that hint when no JWT exists yet.
export function LoginPage() {
  const navigate = useNavigate()
  const setSession = useAuthStore((s) => s.setSession)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      const data = await authAPI.login(username, password)
      setSession(data.tokens.access_token, data.tokens.refresh_token, data.user)
      toast.success(`欢迎回来, ${data.user.username}`)
      navigate('/')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '登录失败,请检查用户名与密码'
      toast.error(msg)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center px-4">
      {/* 表单居中区域：flex-grow + flex center 使表单在剩余空间内垂直居中 */}
      <div className="flex flex-1 flex-col items-center justify-center">
        <form onSubmit={handleSubmit} className="glass-panel w-full max-w-md">
          <div className="mb-8 flex flex-col items-center gap-2">
            <Film className="h-10 w-10 text-primary-400" />
            <h1 className="font-display text-2xl font-bold tracking-wide text-white">
              MediaStationGo
            </h1>
            <p className="text-sm text-slate-400">登录到你的家庭媒体中心</p>
          </div>

          <label className="mb-4 block">
            <span className="mb-1 block text-sm text-slate-300">用户名</span>
            <input
              className="input-base"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
            />
          </label>

          <label className="mb-6 block">
            <span className="mb-1 block text-sm text-slate-300">密码</span>
            <input
              type="password"
              className="input-base"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </label>

          <button
            type="submit"
            disabled={loading}
            className="neon-button w-full justify-center"
          >
            {loading ? '登录中…' : '登录'}
          </button>

          <p className="mt-6 text-center text-xs text-slate-500">
            首次部署默认账号:<code className="text-primary-400">admin / admin123</code>
          </p>
        </form>
      </div>

      {/* 页脚始终在底部 */}
      <AppFooter className="mb-4 mt-auto" />
    </div>
  )
}
