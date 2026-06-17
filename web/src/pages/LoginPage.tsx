import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import toast from 'react-hot-toast'
import { Eye, EyeOff, ArrowRight, ShieldCheck, Lock, User } from 'lucide-react'
import { AppFooter } from '../components/AppFooter'
import { authAPI } from '../api/auth'
import { useAuthStore } from '../stores/auth'

export function LoginPage() {
  const navigate = useNavigate()
  const setSession = useAuthStore((s) => s.setSession)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPw, setShowPw] = useState(false)
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
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '登录失败'
      toast.error(msg)
    } finally { setLoading(false) }
  }

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-gray-50/50 overflow-hidden px-4">
      {/* ── Editorial Soft Geometric Background Decor ── */}
      <div className="pointer-events-none absolute inset-0 z-0">
        <div className="absolute -right-20 -top-20 h-[600px] w-[600px] rounded-full bg-brand-100/30 blur-[130px]" />
        <div className="absolute -bottom-40 -left-20 h-[500px] w-[500px] rounded-full bg-sage-100/30 blur-[110px]" />
        <div className="absolute inset-0 bg-[radial-gradient(#e5e7eb_1px,transparent_1px)] [background-size:20px_20px] opacity-40" />
      </div>

      {/* ── Main Container ── */}
      <motion.div
        initial={{ opacity: 0, y: 30 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.7, ease: [0.16, 1, 0.3, 1] }}
        className="relative z-10 w-full max-w-[420px] px-2"
      >
        {/* High-Contrast Swiss White Panel */}
        <div className="rounded-3xl border border-gray-200/90 bg-white p-8 sm:p-10 shadow-[0_25px_60px_rgba(17,24,39,0.04),0_1px_3px_rgba(17,24,39,0.01)]">
          
          {/* Logo & Headline */}
          <div className="flex flex-col items-center text-center pb-8">
            <motion.img
              initial={{ scale: 0.8, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              transition={{ delay: 0.15, type: 'spring', stiffness: 200 }}
              src="/brand/mgo-emby-icon.png"
              alt="MediaStationGo"
              className="mb-4 h-14 w-14 rounded-2xl object-contain shadow-sm"
            />
            
            <motion.h1
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.25 }}
              className="font-display text-2xl font-extrabold tracking-tight text-gray-900"
            >
              MediaStationGo
            </motion.h1>
            
            <motion.p
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ delay: 0.35 }}
              className="mt-2 text-xs font-bold uppercase tracking-widest text-[#c9954a]"
            >
              家庭媒体私有站舱
            </motion.p>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Username Input with Integrated Icon */}
            <motion.div
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: 0.4 }}
            >
              <label className="block mb-2 text-xs font-bold uppercase tracking-widest text-gray-500">
                用户名
              </label>
              <div className="relative">
                <span className="absolute left-4 top-1/2 -translate-y-1/2 text-gray-500">
                  <User size={16} />
                </span>
                <input
                  type="text"
                  className="w-full rounded-xl border border-gray-200 bg-gray-50/50 px-11 py-3.5 text-sm text-gray-900 placeholder-gray-500 outline-none transition-all duration-300 focus:border-brand-500 focus:bg-white focus:ring-4 focus:ring-brand-100/50"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  autoFocus
                  required
                  placeholder="请输入您的账号"
                />
              </div>
            </motion.div>

            {/* Password Input with Integrated Icon */}
            <motion.div
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: 0.45 }}
            >
              <label className="block mb-2 text-xs font-bold uppercase tracking-widest text-gray-500">
                安全密码
              </label>
              <div className="relative">
                <span className="absolute left-4 top-1/2 -translate-y-1/2 text-gray-500">
                  <Lock size={16} />
                </span>
                <input
                  type={showPw ? 'text' : 'password'}
                  className="w-full rounded-xl border border-gray-200 bg-gray-50/50 px-11 py-3.5 text-sm text-gray-900 placeholder-gray-500 outline-none transition-all duration-300 focus:border-brand-500 focus:bg-white focus:ring-4 focus:ring-brand-100/50 pr-12"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                  placeholder="••••••••"
                />
                <button
                  type="button"
                  onClick={() => setShowPw(!showPw)}
                  className="absolute right-4 top-1/2 -translate-y-1/2 text-gray-500 transition-colors hover:text-gray-900"
                  tabIndex={-1}
                >
                  {showPw ? <EyeOff size={16} /> : <Eye size={16} />}
                </button>
              </div>
            </motion.div>

            {/* Submit Button */}
            <motion.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.5 }}
              className="pt-2"
            >
              <button
                type="submit"
                disabled={loading}
                className="w-full flex items-center justify-center gap-2 rounded-xl bg-[#111827] py-3.5 text-sm font-bold text-white transition-all duration-300 hover:bg-[#1f2937] hover:-translate-y-0.5 active:translate-y-0 active:scale-[0.98] disabled:opacity-50"
              >
                {loading ? (
                  <span className="flex items-center gap-2">
                    <svg className="h-4 w-4 animate-spin text-white" viewBox="0 0 24 24" fill="none">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                    正在进入系统舱...
                  </span>
                ) : (
                  <span className="flex items-center gap-2">
                    立即开启观影之旅 <ArrowRight size={16} />
                  </span>
                )}
              </button>
            </motion.div>

            {/* Quick Helper Credentials */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ delay: 0.55 }}
              className="flex items-center justify-center gap-1.5 rounded-xl bg-gray-50 border border-gray-100 px-4 py-2.5 text-xs text-gray-500"
            >
              <ShieldCheck size={14} className="text-brand-500" />
              <span>默认访问凭证: </span>
              <code className="rounded-lg bg-gray-200/50 px-1.5 py-0.5 font-mono text-[#111827] font-semibold">
                admin / admin123
              </code>
            </motion.div>
          </form>
        </div>
      </motion.div>

      {/* Footer */}
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ delay: 0.6 }}
        className="relative z-10 mt-10 text-gray-500 hover:text-gray-600 transition-colors"
      >
        <AppFooter />
      </motion.div>
    </div>
  )
}
