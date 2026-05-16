import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  Activity,
  Bell,
  Cast,
  Clock,
  CloudDownload,
  Compass,
  Copy,
  Film,
  FolderTree,
  GalleryHorizontalEnd,
  Globe,
  HardDrive,
  Heart,
  Home,
  KeyRound,
  KeySquare,
  Link2,
  ListChecks,
  ListMusic,
  LogOut,
  MessageSquare,
  Rss,
  Search,
  Server,
  Settings,
  Sliders,
  Sparkles,
  Cloud,
  Trash2,
  UserCog,
  Wrench,
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
          <SidebarLink to="/discover" icon={<Compass size={18} />} label="发现" />
          <SidebarLink to="/search" icon={<Search size={18} />} label="搜索" />
          <SidebarLink to="/ai" icon={<Sparkles size={18} />} label="AI 助手" />
          <SidebarLink to="/favourites" icon={<Heart size={18} />} label="收藏" />
          <SidebarLink to="/playlists" icon={<ListMusic size={18} />} label="播放列表" />
          <SidebarLink to="/history" icon={<Clock size={18} />} label="观看历史" />
          <SidebarLink to="/poster-wall" icon={<GalleryHorizontalEnd size={18} />} label="海报墙" />

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
          <SidebarLink to="/dlna" icon={<Cast size={18} />} label="DLNA 投屏" />
          <SidebarLink to="/sites" icon={<Globe size={18} />} label="站点管理" />
          <SidebarLink to="/site-search" icon={<Search size={18} />} label="站点搜索" />

          <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
            账号
          </div>
          <SidebarLink to="/profile" icon={<UserIcon size={18} />} label="个人资料" />
          <SidebarLink to="/play-profiles" icon={<UserCog size={18} />} label="观影 Profile" />

          {user?.role === 'admin' && (
            <>
              <div className="mt-6 px-2 text-xs uppercase tracking-wider text-slate-500">
                管理
              </div>
              <SidebarLink to="/tasks" icon={<ListChecks size={18} />} label="实时任务" />
              <SidebarLink to="/stats" icon={<Activity size={18} />} label="运行状态" />
              <SidebarLink to="/storage" icon={<HardDrive size={18} />} label="存储" />
              <SidebarLink to="/files" icon={<FolderTree size={18} />} label="文件浏览" />
              <SidebarLink to="/duplicates" icon={<Copy size={18} />} label="重复文件" />
              <SidebarLink to="/scheduler" icon={<Clock size={18} />} label="定时任务" />
              <SidebarLink to="/api-configs" icon={<KeyRound size={18} />} label="API 配置" />
              <SidebarLink to="/strm" icon={<Link2 size={18} />} label="STRM 管理" />
              <SidebarLink to="/tools" icon={<Wrench size={18} />} label="运维工具" />
              <SidebarLink to="/notify-channels" icon={<Bell size={18} />} label="通知渠道" />
              <SidebarLink to="/download-clients" icon={<Server size={18} />} label="下载器" />
              <SidebarLink to="/storage-config" icon={<Cloud size={18} />} label="外部存储" />
              <SidebarLink to="/license" icon={<KeySquare size={18} />} label="许可证" />
              <SidebarLink to="/assistant" icon={<MessageSquare size={18} />} label="AI 对话" />
              <SidebarLink to="/settings" icon={<Sliders size={18} />} label="系统设置" />
              <SidebarLink to="/recycle" icon={<Trash2 size={18} />} label="回收站" />
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
