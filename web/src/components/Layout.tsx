import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  Activity,
  CloudDownload,
  Film,
  Heart,
  Home,
  ListMusic,
  LogOut,
  Rss,
  Search,
  Settings,
  Library as LibraryIcon,
  User as UserIcon,
} from 'lucide-react'
import clsx from 'clsx'

import { libraryAPI } from '../api/library'
import { useAuthStore } from '../stores/auth'
import type { Library } from '../types'

// Top-level chrome: a fixed left rail + scrollable content area.
export function Layout() {
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const [libraries, setLibraries] = useState<Library[]>([])

  useEffect(() => {
    libraryAPI.list().then(setLibraries).catch(() => undefined)
  }, [])

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  return (
    <div className="flex min-h-full">
      <aside className="hidden w-64 shrink-0 flex-col border-r border-white/5 bg-surface-900/80 px-4 py-6 backdrop-blur md:flex">
        <Link to="/" className="mb-8 flex items-center gap-2 px-2">
          <Film className="h-6 w-6 text-primary-400" />
          <span className="font-display text-xl font-bold tracking-wide text-white">
            MediaStationGo
          </span>
        </Link>

        <nav className="flex-1 space-y-1 overflow-y-auto pr-1">
          <SidebarLink to="/" icon={<Home size={18} />} label="首页" end />
          <SidebarLink to="/search" icon={<Search size={18} />} label="搜索" />
          <SidebarLink to="/favourites" icon={<Heart size={18} />} label="收藏" />
          <SidebarLink to="/playlists" icon={<ListMusic size={18} />} label="播放列表" />

          <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
            媒体库
          </div>
          {libraries.length === 0 && (
            <div className="px-2 py-1 text-sm text-slate-500">暂无媒体库</div>
          )}
          {libraries.map((lib) => (
            <SidebarLink
              key={lib.id}
              to={`/library/${lib.id}`}
              icon={<LibraryIcon size={18} />}
              label={lib.name}
            />
          ))}

          <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
            自动化
          </div>
          <SidebarLink to="/downloads" icon={<CloudDownload size={18} />} label="下载" />
          <SidebarLink to="/subscriptions" icon={<Rss size={18} />} label="RSS 订阅" />

          <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
            账号
          </div>
          <SidebarLink to="/profile" icon={<UserIcon size={18} />} label="个人资料" />

          {user?.role === 'admin' && (
            <>
              <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
                管理
              </div>
              <SidebarLink to="/stats" icon={<Activity size={18} />} label="运行状态" />
              <SidebarLink to="/admin" icon={<Settings size={18} />} label="管理后台" />
            </>
          )}
        </nav>

        <button
          onClick={handleLogout}
          className="mt-4 flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-slate-400 transition hover:bg-white/5 hover:text-white"
        >
          <LogOut size={16} />
          退出登录 ({user?.username})
        </button>
      </aside>

      <main className="flex-1 overflow-y-auto px-6 py-8">
        <Outlet />
      </main>
    </div>
  )
}

function SidebarLink({
  to,
  icon,
  label,
  end,
}: {
  to: string
  icon: React.ReactNode
  label: string
  end?: boolean
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        clsx(
          'flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition',
          isActive
            ? 'bg-primary-400/10 text-primary-400'
            : 'text-slate-300 hover:bg-white/5 hover:text-white',
        )
      }
    >
      {icon}
      <span className="truncate">{label}</span>
    </NavLink>
  )
}
