import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import {
  Activity, Bell, Cast, Clock, CloudDownload, Compass, Copy, Film,
  FolderTree, Globe, HardDrive, Heart, Home, GalleryHorizontalEnd,
  Link2, ListChecks, ListMusic, LogOut, MessageSquare, Rss, Search,
  Server, Settings, Sliders, Sparkles, Cloud, Trash2, UserCog, Wrench,
  Library as LibraryIcon, User as UserIcon, ChevronDown, Menu, X
} from 'lucide-react'
import clsx from 'clsx'
import { AppFooter } from './AppFooter'
import { libraryAPI } from '../api/library'
import { useAuthStore } from '../stores/auth'
import type { Library } from '../types'

export function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const [libraries, setLibraries] = useState<Library[]>([])
  const [isSidebarOpen, setIsSidebarOpen] = useState(true)
  const [isMobileDrawerOpen, setIsMobileDrawerOpen] = useState(false)
  const [isProfileOpen, setIsProfileOpen] = useState(false)
  const [searchFocused, setSearchFocused] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')

  // Automatically fetch libraries
  useEffect(() => {
    libraryAPI.list().then(setLibraries).catch(() => undefined)
  }, [])

  // Auto-collapse sidebar on smaller tablet screens, and auto-hide drawer on path change
  useEffect(() => {
    const handleResize = () => {
      if (window.innerWidth < 1024) {
        setIsSidebarOpen(false)
      } else {
        setIsSidebarOpen(true)
      }
    }
    handleResize()
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  useEffect(() => {
    setIsMobileDrawerOpen(false)
  }, [location.pathname])

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (searchQuery.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`)
    }
  }

  const sidebarContent = (
    <div className="flex h-full flex-col bg-white border-r border-gray-200/80">
      {/* Brand Logo & Brand Title */}
      <div className="flex h-20 items-center justify-between px-6 border-b border-gray-100">
        <Link to="/" className="flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-[#111827] to-[#1f2937] shadow-sm">
            <Film className="h-5 w-5 text-[#c9954a]" />
          </div>
          {(isSidebarOpen || isMobileDrawerOpen) && (
            <motion.span 
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              className="font-display text-lg font-extrabold tracking-tight text-[#111827]"
            >
              MediaStation
            </motion.span>
          )}
        </Link>
        
        {/* Toggle Collapse Button for Large Screen */}
        <button 
          onClick={() => setIsSidebarOpen(!isSidebarOpen)} 
          className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-900 transition-colors hidden lg:block"
        >
          <Menu size={18} />
        </button>

        {/* Mobile Drawer Close Button */}
        <button 
          onClick={() => setIsMobileDrawerOpen(false)} 
          className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-900 transition-colors block lg:hidden"
        >
          <X size={18} />
        </button>
      </div>

      {/* Navigation List */}
      <nav className="flex-1 overflow-y-auto px-4 py-6 space-y-6 scrollbar-hide">
        {/* Navigation Group: Main */}
        <div>
          <SectionHeader label="影音中心" visible={isSidebarOpen || isMobileDrawerOpen} />
          <div className="space-y-1">
            <SidebarLink to="/" icon={<Home size={18} />} label="系统首页" end collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/discover" icon={<Compass size={18} />} label="精彩发现" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/search" icon={<Search size={18} />} label="智能搜索" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/favourites" icon={<Heart size={18} />} label="我的收藏" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/playlists" icon={<ListMusic size={18} />} label="播放列表" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/history" icon={<Clock size={18} />} label="观看历史" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/poster-wall" icon={<GalleryHorizontalEnd size={18} />} label="影音海报墙" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/ai" icon={<Sparkles size={18} />} label="AI 影视助理" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
          </div>
        </div>

        {/* Navigation Group: Libraries */}
        <div>
          <SectionHeader label="媒体片库" visible={isSidebarOpen || isMobileDrawerOpen} />
          <div className="space-y-1">
            {libraries.length === 0 && (isSidebarOpen || isMobileDrawerOpen) && (
              <div className="px-4 py-2 text-xs text-gray-400 italic">暂无媒体片库</div>
            )}
            {libraries.map((lib) => (
              <SidebarLink 
                key={lib.id} 
                to={`/library/${lib.id}`} 
                icon={<LibraryIcon size={18} />} 
                label={lib.name} 
                collapsed={!isSidebarOpen && !isMobileDrawerOpen} 
              />
            ))}
          </div>
        </div>

        {/* Navigation Group: Automation */}
        <div>
          <SectionHeader label="自动化运维" visible={isSidebarOpen || isMobileDrawerOpen} />
          <div className="space-y-1">
            <SidebarLink to="/downloads" icon={<CloudDownload size={18} />} label="下载中心" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/subscriptions" icon={<Rss size={18} />} label="RSS 订阅" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/dlna" icon={<Cast size={18} />} label="DLNA 串流" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/site-search" icon={<Search size={18} />} label="站点检索" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
          </div>
        </div>

        {/* Navigation Group: Account Profile */}
        <div>
          <SectionHeader label="个人资料" visible={isSidebarOpen || isMobileDrawerOpen} />
          <div className="space-y-1">
            <SidebarLink to="/profile" icon={<UserIcon size={18} />} label="账号信息" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            <SidebarLink to="/play-profiles" icon={<UserCog size={18} />} label="观影 Profile" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
          </div>
        </div>

        {/* Navigation Group: Admin Dashboard */}
        {user?.role === 'admin' && (
          <div>
            <SectionHeader label="系统后台" visible={isSidebarOpen || isMobileDrawerOpen} />
            <div className="space-y-1">
              <SidebarLink to="/admin" icon={<Settings size={18} />} label="后台主页" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/tasks" icon={<ListChecks size={18} />} label="实时任务" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/stats" icon={<Activity size={18} />} label="运行监控" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/sites" icon={<Globe size={18} />} label="站点管理" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/notify-channels" icon={<Bell size={18} />} label="通知配置" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/download-clients" icon={<Server size={18} />} label="下载客户端" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/scheduler" icon={<Clock size={18} />} label="定时机制" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/storage" icon={<HardDrive size={18} />} label="存储分析" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/storage-config" icon={<Cloud size={18} />} label="外部挂载" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/files" icon={<FolderTree size={18} />} label="文件管家" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/duplicates" icon={<Copy size={18} />} label="排重清理" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/strm" icon={<Link2 size={18} />} label="STRM 关联" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/tools" icon={<Wrench size={18} />} label="运维工具" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/assistant" icon={<MessageSquare size={18} />} label="AI 智能助教" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/settings" icon={<Sliders size={18} />} label="系统参数" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
              <SidebarLink to="/recycle" icon={<Trash2 size={18} />} label="回收站" collapsed={!isSidebarOpen && !isMobileDrawerOpen} />
            </div>
          </div>
        )}
      </nav>

      {/* Sidebar Logout Action */}
      <div className="border-t border-gray-100 p-4 bg-gray-50/50">
        <button
          onClick={() => { logout(); navigate('/login') }}
          className={clsx(
            "flex items-center gap-3.5 rounded-xl px-4 py-3 text-sm font-semibold transition-all duration-300 w-full group/logout",
            (isSidebarOpen || isMobileDrawerOpen) ? "justify-start text-gray-500 hover:bg-red-50 hover:text-red-600" : "justify-center text-gray-400 hover:text-red-600"
          )}
          title={`安全登出 (${user?.username})`}
        >
          <LogOut size={18} className="transition-transform group-hover/logout:-translate-x-0.5" />
          {(isSidebarOpen || isMobileDrawerOpen) && <span>安全退出</span>}
        </button>
      </div>
    </div>
  )

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#f9fafb] text-gray-900 font-body select-none">
      
      {/* 1. Desktop Persistent Sidebar */}
      <aside className={clsx(
        "hidden lg:flex flex-col h-full shrink-0 transition-all duration-300 ease-out",
        isSidebarOpen ? "w-64" : "w-20"
      )}>
        {sidebarContent}
      </aside>

      {/* 2. Mobile & Tablet Sidebar Drawer (Overlay) */}
      <AnimatePresence>
        {isMobileDrawerOpen && (
          <div className="fixed inset-0 z-50 flex lg:hidden">
            {/* Backdrop sheet */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsMobileDrawerOpen(false)}
              className="fixed inset-0 bg-black/15 backdrop-blur-sm"
            />
            {/* Drawer body container */}
            <motion.div
              initial={{ x: '-100%' }}
              animate={{ x: 0 }}
              exit={{ x: '-100%' }}
              transition={{ type: 'spring', damping: 25, stiffness: 220 }}
              className="relative flex w-64 max-w-xs flex-col h-full z-10 shadow-xl"
            >
              {sidebarContent}
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {/* 3. Main Workspace Container */}
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
        
        {/* Top Header Bar */}
        <header className="flex h-20 shrink-0 items-center justify-between px-6 md:px-8 bg-white/80 border-b border-gray-200/60 backdrop-blur-md z-30">
          
          {/* Header Left Area: Hamburger and Search */}
          <div className="flex items-center gap-4 flex-1 max-w-lg">
            {/* Hamburger button shown on Mobile & Tablet */}
            <button 
              onClick={() => setIsMobileDrawerOpen(true)}
              className="rounded-xl border border-gray-200 p-2.5 text-gray-500 hover:bg-gray-100 lg:hidden"
            >
              <Menu size={18} />
            </button>

            {/* Premium Integrated Search Form */}
            <form onSubmit={handleSearchSubmit} className="relative w-full hidden sm:block">
              <span className={clsx(
                "absolute left-4 top-1/2 -translate-y-1/2 transition-colors duration-200",
                searchFocused ? "text-brand-600" : "text-gray-400"
              )}>
                <Search size={16} />
              </span>
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onFocus={() => setSearchFocused(true)}
                onBlur={() => setSearchFocused(false)}
                placeholder="搜索电影、电视剧、演员、种子站点..."
                className="w-full rounded-full border border-gray-200 bg-gray-50/50 py-2.5 pl-11 pr-12 text-sm text-gray-900 placeholder-gray-400 outline-none transition-all duration-300 focus:border-brand-500 focus:bg-white focus:ring-4 focus:ring-brand-100/40"
              />
              <div className="absolute right-4 top-1/2 -translate-y-1/2 pointer-events-none">
                <span className="rounded-md border border-gray-200 bg-white px-1.5 py-0.5 text-[9px] font-bold text-gray-400 uppercase tracking-wider">
                  Enter
                </span>
              </div>
            </form>
          </div>

          {/* Header Right Area: Actions, Notifications, Profile Dropdown */}
          <div className="flex items-center gap-4 shrink-0">
            {/* Quick search button shown ONLY on Mobile screens */}
            <Link 
              to="/search" 
              className="rounded-xl border border-gray-200 p-2.5 text-gray-400 hover:bg-gray-100 hover:text-gray-900 sm:hidden"
            >
              <Search size={18} />
            </Link>

            {/* Quick Discover shortcut button */}
            <Link 
              to="/discover" 
              className="hidden md:flex items-center gap-2 rounded-xl border border-gray-200 px-4 py-2.5 text-xs font-bold text-gray-500 hover:bg-gray-50 hover:text-gray-900 transition-all"
            >
              <Sparkles size={14} className="text-brand-500" />
              <span>发现新片</span>
            </Link>

            {/* Notification alert bubble */}
            <button className="relative rounded-xl border border-gray-200 p-2.5 text-gray-400 hover:bg-gray-100 hover:text-gray-900 transition-all">
              <Bell size={18} />
              <span className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-brand-500 ring-2 ring-white animate-pulse" />
            </button>

            {/* Horizontal divider lines */}
            <span className="h-6 w-px bg-gray-200" />

            {/* Elegant user dropdown list */}
            <div className="relative">
              <button 
                onClick={() => setIsProfileOpen(!isProfileOpen)}
                className="flex items-center gap-2.5 rounded-full border border-gray-200 p-1 pr-3 hover:bg-gray-50 transition-all"
              >
                <div className="flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-br from-[#111827] to-[#1f2937] text-white font-display text-xs font-bold shadow-sm">
                  {user?.username?.slice(0, 2).toUpperCase() || "US"}
                </div>
                <div className="text-left hidden md:block">
                  <p className="text-xs font-bold text-gray-900 leading-none">{user?.username}</p>
                  <p className="text-[9px] text-gray-500 font-bold uppercase tracking-wider mt-0.5 leading-none">{user?.role}</p>
                </div>
                <ChevronDown size={14} className="text-gray-400" />
              </button>

              <AnimatePresence>
                {isProfileOpen && (
                  <>
                    {/* Backdrop cover for closing drawer click */}
                    <div className="fixed inset-0 z-10" onClick={() => setIsProfileOpen(false)} />
                    <motion.div
                      initial={{ opacity: 0, y: 10, scale: 0.95 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 10, scale: 0.95 }}
                      transition={{ duration: 0.15 }}
                      className="absolute right-0 mt-3 w-56 origin-top-right rounded-2xl border border-gray-200 bg-white p-2 shadow-xl z-20"
                    >
                      <Link
                        to="/profile"
                        onClick={() => setIsProfileOpen(false)}
                        className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-gray-950 transition-colors"
                      >
                        <UserIcon size={16} />
                        <span>个人基本信息</span>
                      </Link>
                      <Link
                        to="/play-profiles"
                        onClick={() => setIsProfileOpen(false)}
                        className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-gray-950 transition-colors"
                      >
                        <UserCog size={16} />
                        <span>观影 Profile 切换</span>
                      </Link>
                      {user?.role === 'admin' && (
                        <Link
                          to="/admin"
                          onClick={() => setIsProfileOpen(false)}
                          className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-gray-950 transition-colors"
                        >
                          <Settings size={16} />
                          <span>管理主控制台</span>
                        </Link>
                      )}
                      <div className="my-1.5 border-t border-gray-100" />
                      <button
                        onClick={() => {
                          setIsProfileOpen(false);
                          logout();
                          navigate('/login');
                        }}
                        className="flex w-full items-center gap-3 rounded-xl px-3 py-2 text-sm text-red-600 hover:bg-red-50 transition-colors"
                      >
                        <LogOut size={16} />
                        <span>安全登出系统</span>
                      </button>
                    </motion.div>
                  </>
                )}
              </AnimatePresence>
            </div>
          </div>
        </header>

        {/* ── Dynamic Content Workspace Scroll Area ── */}
        <main className="flex-1 overflow-y-auto px-4 py-6 md:px-8 md:py-10">
          <div className="max-w-7xl mx-auto">
            <AnimatePresence mode="wait">
              <motion.div
                key={location.pathname}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -6 }}
                transition={{ duration: 0.25, ease: 'easeOut' }}
              >
                <Outlet />
              </motion.div>
            </AnimatePresence>
          </div>
        </main>
        
        {/* Absolute Footer Frame */}
        <AppFooter className="border-t border-gray-200/50 bg-white py-5 text-center text-xs text-gray-400" />
      </div>
    </div>
  )
}

function SectionHeader({ label, visible }: { label: string; visible: boolean }) {
  if (!visible) return <div className="h-5" />;
  return (
    <div className="px-4 mb-2 mt-4">
      <span className="text-[10px] font-bold uppercase tracking-[0.25em] text-gray-400">
        {label}
      </span>
    </div>
  )
}

interface SidebarLinkProps {
  to: string;
  icon: React.ReactNode;
  label: string;
  end?: boolean;
  collapsed?: boolean;
}

function SidebarLink({ to, icon, label, end, collapsed }: SidebarLinkProps) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        clsx(
          "flex items-center gap-3.5 rounded-xl px-4 py-3 text-sm font-semibold transition-all duration-300 relative group",
          isActive
            ? "bg-[#111827] text-white shadow-sm"
            : "text-gray-500 hover:bg-gray-50 hover:text-gray-900"
        )
      }
    >
      {({ isActive }) => (
        <>
          <span className={clsx(
            "flex shrink-0 items-center justify-center w-5 h-5 transition-transform duration-300 group-hover:scale-110",
            isActive ? "text-[#c9954a]" : "text-gray-400 group-hover:text-gray-700"
          )}>
            {icon}
          </span>
          {!collapsed && (
            <motion.span
              initial={{ opacity: 0, x: -5 }}
              animate={{ opacity: 1, x: 0 }}
              className="truncate whitespace-nowrap"
            >
              {label}
            </motion.span>
          )}
          {collapsed && (
            <div className="absolute left-full ml-3 px-2.5 py-1.5 rounded-lg bg-gray-900 text-white text-xs font-semibold opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto transition-opacity shadow-lg z-50 whitespace-nowrap">
              {label}
            </div>
          )}
        </>
      )}
    </NavLink>
  )
}