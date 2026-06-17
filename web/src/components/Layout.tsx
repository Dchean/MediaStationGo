import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import toast from 'react-hot-toast'
import {
  Activity, Bell, Clock, CloudDownload, Compass,
  Cast, Globe, HardDrive, Heart, Home, Image, KeySquare,
  ListMusic, LogOut, Rss, Search, Trash2,
  Settings, Sliders, Sparkles, UserCog,
  Library as LibraryIcon, User as UserIcon, ChevronDown, Menu, X
} from 'lucide-react'
import clsx from 'clsx'
import { AppFooter } from './AppFooter'
import { useAuthStore } from '../stores/auth'
import { usePermissionStore } from '../stores/permissions'
import { usePlayProfileStore } from '../stores/playProfile'
import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import { playProfilesAPI } from '../api/play_profiles'
import { requestPIN } from './PinDialog'
import type { Media, PlayProfile } from '../types'
import { groupSeries, seriesCardLink } from '../utils/groupSeries'

export function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const permissions = usePermissionStore((s) => s.permissions)
  const isSuper = usePermissionStore((s) => s.isSuper)
  const isPermissionLoading = usePermissionStore((s) => s.isLoading)
  const fetchPermissions = usePermissionStore((s) => s.fetchPermissions)
  const activeProfileId = usePlayProfileStore((s) => s.activeProfileId)
  const setActiveProfile = usePlayProfileStore((s) => s.setActiveProfile)
  const [isSidebarOpen, setIsSidebarOpen] = useState(true)
  const [isMobileDrawerOpen, setIsMobileDrawerOpen] = useState(false)
  const [isProfileOpen, setIsProfileOpen] = useState(false)
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({ media: true })
  const [profiles, setProfiles] = useState<PlayProfile[]>([])
  const [searchFocused, setSearchFocused] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchItems, setSearchItems] = useState<Media[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchTotal, setSearchTotal] = useState(0)
  const [searchError, setSearchError] = useState('')
  const searchSeq = useRef(0)
  const searchCards = useMemo(() => groupSeries(searchItems).slice(0, 8), [searchItems])

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

  useEffect(() => {
    if (user && !isPermissionLoading && Object.keys(permissions ?? {}).length === 0) {
      fetchPermissions().catch(() => undefined)
    }
  }, [fetchPermissions, isPermissionLoading, permissions, user])

  useEffect(() => {
    if (location.pathname === '/search') {
      const query = new URLSearchParams(location.search).get('q') ?? ''
      setSearchQuery(query)
    }
  }, [location.pathname, location.search])

  useEffect(() => {
    const query = searchQuery.trim()
    const seq = ++searchSeq.current
    if (!searchFocused || !query) {
      setSearchItems([])
      setSearchTotal(0)
      setSearchError('')
      setSearchLoading(false)
      return
    }

    setSearchLoading(true)
    setSearchError('')
    const timer = window.setTimeout(() => {
      mediaAPI
        .search(query, 24)
        .then((data) => {
          if (seq !== searchSeq.current) return
          setSearchItems(data.items ?? [])
          setSearchTotal(data.total ?? (data.items ?? []).length)
        })
        .catch(() => {
          if (seq !== searchSeq.current) return
          setSearchItems([])
          setSearchTotal(0)
          setSearchError('搜索失败，请稍后再试')
        })
        .finally(() => {
          if (seq === searchSeq.current) setSearchLoading(false)
        })
    }, 220)

    return () => window.clearTimeout(timer)
  }, [searchFocused, searchQuery])

  useEffect(() => {
    if (!user) {
      setProfiles([])
      setActiveProfile(null)
      return
    }
    playProfilesAPI
      .list()
      .then((rows) => {
        setProfiles(rows)
        const active = rows.find((p) => p.id === activeProfileId)
        if (!active) {
          const defaultProfile = rows.find((p) => p.is_default && !p.require_pin)
          setActiveProfile(defaultProfile?.id ?? null)
        }
      })
      .catch(() => undefined)
  }, [activeProfileId, setActiveProfile, user])

  const isAdmin = user?.role === 'admin'
  const can = (key: string) => isAdmin || isSuper || (permissions ?? {})[key] === true
  const activeProfile = profiles.find((p) => p.id === activeProfileId) ?? null
  const sidebarExpanded = isSidebarOpen || isMobileDrawerOpen
  const isRouteIn = (paths: string[]) =>
    paths.some((path) => (path === '/' ? location.pathname === '/' : location.pathname.startsWith(path)))
  const toggleGroup = (key: string) =>
    setOpenGroups((current) => ({ ...current, [key]: !current[key] }))

  useEffect(() => {
    const groupPaths: Record<string, string[]> = {
      media: ['/', '/libraries', '/library', '/poster-wall', '/discover', '/search', '/dlna', '/ai'],
      personal: ['/favourites', '/playlists', '/playlist', '/history', '/profile', '/play-profiles'],
      downloads: ['/downloads', '/download-clients', '/subscriptions', '/site-search'],
      tools: ['/storage', '/storage-config', '/files', '/strm', '/duplicates', '/tasks', '/scheduler', '/recycle', '/stats'],
      system: ['/admin', '/sites', '/notify-channels', '/license', '/settings', '/assistant'],
    }
    const active = Object.entries(groupPaths).find(([, paths]) => isRouteIn(paths))?.[0]
    if (active) {
      setOpenGroups((current) => (current[active] ? current : { ...current, [active]: true }))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname])

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (searchQuery.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`)
      setSearchFocused(false)
    }
  }

  const handleProfileSwitch = async (profile: PlayProfile) => {
    if (activeProfileId === profile.id) {
      setIsProfileOpen(false)
      return
    }
    try {
      let pinToken: string | null = null
      if (profile.require_pin) {
        const pin = await requestPIN({ profileName: profile.name })
        if (!pin) return
        const verified = await playProfilesAPI.verifyPin(profile.id, pin)
        pinToken = verified.token
      }
      setActiveProfile(profile.id, pinToken)
      setIsProfileOpen(false)
      toast.success(`已切换到「${profile.name}」`)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'PIN 验证失败'
      toast.error(msg)
    }
  }

  const sidebarContent = (
    <div className="flex h-full flex-col bg-white border-r border-gray-200/80">
      {/* Brand Logo & Brand Title */}
      <div className="flex h-20 items-center justify-between px-6 border-b border-gray-100">
        <Link to="/" className="flex items-center gap-3">
          <img
            src="/brand/mgo-emby-icon.png"
            alt="MediaStationGo"
            className="h-10 w-10 shrink-0 rounded-xl object-contain shadow-sm"
          />
          {(isSidebarOpen || isMobileDrawerOpen) && (
            <motion.span 
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              className="font-display text-lg font-extrabold tracking-tight text-[#111827]"
            >
              MediaStationGo
            </motion.span>
          )}
        </Link>
        
        {/* Toggle Collapse Button for Large Screen */}
        <button 
          onClick={() => setIsSidebarOpen(!isSidebarOpen)} 
          className="rounded-xl p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 transition-colors hidden lg:block"
        >
          <Menu size={18} />
        </button>

        {/* Mobile Drawer Close Button */}
        <button 
          onClick={() => setIsMobileDrawerOpen(false)} 
          className="rounded-xl p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 transition-colors block lg:hidden"
        >
          <X size={18} />
        </button>
      </div>

      {/* Navigation List */}
      <nav className="flex-1 overflow-y-auto px-4 py-5 space-y-2 scrollbar-hide">
        <SidebarGroup
          id="media"
          icon={<Home size={18} />}
          label="影音中心"
          collapsed={!sidebarExpanded}
          open={openGroups.media ?? true}
          active={isRouteIn(['/', '/libraries', '/library', '/poster-wall', '/discover', '/search', '/dlna', '/ai'])}
          onToggle={toggleGroup}
        >
          <SidebarLink to="/" icon={<Home size={16} />} label="系统首页" end child />
          <SidebarLink to="/libraries" icon={<LibraryIcon size={16} />} label="媒体库" child />
          <SidebarLink to="/poster-wall" icon={<Image size={16} />} label="海报墙" child />
          {can('can_view_discover') && <SidebarLink to="/discover" icon={<Compass size={16} />} label="精彩发现" child />}
          {can('can_use_ai') && <SidebarLink to="/search" icon={<Search size={16} />} label="智能搜索" child />}
          {can('can_cast') && <SidebarLink to="/dlna" icon={<Cast size={16} />} label="DLNA 投屏" child />}
          {can('can_use_ai_assistant') && <SidebarLink to="/ai" icon={<Sparkles size={16} />} label="AI 助理" child />}
        </SidebarGroup>

        <SidebarGroup
          id="personal"
          icon={<UserIcon size={18} />}
          label="个人空间"
          collapsed={!sidebarExpanded}
          open={openGroups.personal ?? false}
          active={isRouteIn(['/favourites', '/playlists', '/playlist', '/history', '/profile', '/play-profiles'])}
          onToggle={toggleGroup}
        >
          <SidebarLink to="/favourites" icon={<Heart size={16} />} label="我的收藏" child />
          <SidebarLink to="/playlists" icon={<ListMusic size={16} />} label="播放列表" child />
          <SidebarLink to="/history" icon={<Clock size={16} />} label="观看历史" child />
          <SidebarLink to="/profile" icon={<UserIcon size={16} />} label="账号信息" child />
          <SidebarLink to="/play-profiles" icon={<UserCog size={16} />} label="观影 Profile" child />
        </SidebarGroup>

        {(can('can_manage_downloads') || can('can_manage_subscriptions') || can('can_manage_sites')) && (
          <SidebarGroup
            id="downloads"
            icon={<CloudDownload size={18} />}
            label="下载订阅"
            collapsed={!sidebarExpanded}
            open={openGroups.downloads ?? false}
            active={isRouteIn(['/downloads', '/download-clients', '/subscriptions', '/site-search'])}
            onToggle={toggleGroup}
          >
            {can('can_manage_downloads') && <SidebarLink to="/downloads" icon={<CloudDownload size={16} />} label="下载中心" child />}
            {can('can_manage_subscriptions') && <SidebarLink to="/subscriptions" icon={<Rss size={16} />} label="订阅管理" child />}
            {can('can_manage_sites') && <SidebarLink to="/site-search" icon={<Search size={16} />} label="站点检索" child />}
            {isAdmin && <SidebarLink to="/download-clients" icon={<Sliders size={16} />} label="下载器管理" child />}
          </SidebarGroup>
        )}

        {isAdmin && (
          <>
            <SidebarGroup
              id="tools"
              icon={<HardDrive size={18} />}
              label="存储工具"
              collapsed={!sidebarExpanded}
              open={openGroups.tools ?? false}
              active={isRouteIn(['/storage', '/storage-config', '/files', '/strm', '/duplicates', '/tasks', '/scheduler', '/recycle', '/stats'])}
              onToggle={toggleGroup}
            >
              <SidebarLink to="/storage" icon={<HardDrive size={16} />} label="存储与文件" child />
              <SidebarLink to="/storage-config" icon={<CloudDownload size={16} />} label="外部存储" child />
              <SidebarLink to="/files" icon={<LibraryIcon size={16} />} label="文件管理" child />
              <SidebarLink to="/strm" icon={<Cast size={16} />} label="STRM 管理" child />
              <SidebarLink to="/duplicates" icon={<Image size={16} />} label="重复文件" child />
              <SidebarLink to="/tasks" icon={<Activity size={16} />} label="任务队列" child />
              <SidebarLink to="/scheduler" icon={<Clock size={16} />} label="计划任务" child />
              <SidebarLink to="/recycle" icon={<Trash2 size={16} />} label="回收站" child />
              <SidebarLink to="/stats" icon={<Activity size={16} />} label="运行状态" child />
            </SidebarGroup>

            <SidebarGroup
              id="system"
              icon={<Settings size={18} />}
              label="系统管理"
              collapsed={!sidebarExpanded}
              open={openGroups.system ?? false}
              active={isRouteIn(['/admin', '/sites', '/notify-channels', '/license', '/settings', '/assistant'])}
              onToggle={toggleGroup}
            >
              <SidebarLink to="/admin" icon={<Settings size={16} />} label="媒体与用户" child />
              <SidebarLink to="/sites" icon={<Globe size={16} />} label="站点管理" child />
              <SidebarLink to="/notify-channels" icon={<Bell size={16} />} label="通知渠道" child />
              <SidebarLink to="/assistant" icon={<Sparkles size={16} />} label="AI 会话" child />
              <SidebarLink to="/license" icon={<KeySquare size={16} />} label="授权许可" child />
              <SidebarLink to="/settings" icon={<Sliders size={16} />} label="系统设置" child />
            </SidebarGroup>
          </>
        )}
      </nav>

      {/* Sidebar Logout Action */}
      <div className="border-t border-gray-100 p-4 bg-gray-50/50">
        <button
          onClick={() => { logout(); navigate('/login') }}
          className={clsx(
            "flex items-center gap-3.5 rounded-xl px-4 py-3 text-sm font-semibold transition-all duration-300 w-full group/logout",
            (isSidebarOpen || isMobileDrawerOpen) ? "justify-start text-gray-500 hover:bg-red-50 hover:text-red-600" : "justify-center text-gray-500 hover:text-red-600"
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
                searchFocused ? "text-brand-600" : "text-gray-500"
              )}>
                <Search size={16} />
              </span>
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onMouseDown={() => setSearchFocused(true)}
                onClick={() => setSearchFocused(true)}
                onFocus={() => setSearchFocused(true)}
                onBlur={() => window.setTimeout(() => setSearchFocused(false), 120)}
                placeholder="搜索电影、电视剧、演员、种子站点..."
                className="w-full rounded-full border border-gray-200 bg-gray-50/50 py-2.5 pl-11 pr-12 text-sm text-gray-900 placeholder-gray-500 outline-none transition-all duration-300 focus:border-brand-500 focus:bg-white focus:ring-4 focus:ring-brand-100/40"
              />
              <div className="absolute right-4 top-1/2 -translate-y-1/2 pointer-events-none">
                <span className="rounded-xl border border-gray-200 bg-white px-1.5 py-0.5 text-[9px] font-bold text-gray-500 uppercase tracking-wider">
                  Enter
                </span>
              </div>
              <AnimatePresence>
                {searchFocused && searchQuery.trim() && (
                  <motion.div
                    initial={{ opacity: 0, y: 8, scale: 0.98 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: 6, scale: 0.98 }}
                    transition={{ duration: 0.14 }}
                    onMouseDown={(event) => event.preventDefault()}
                    className="absolute left-0 right-0 top-full z-50 mt-3 overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-2xl"
                  >
                    <div className="max-h-[420px] overflow-y-auto p-2">
                      {searchLoading && (
                        <div className="flex items-center gap-2 px-3 py-4 text-sm text-gray-500">
                          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />
                          搜索中...
                        </div>
                      )}
                      {!searchLoading && searchError && (
                        <div className="px-3 py-4 text-sm text-red-500">{searchError}</div>
                      )}
                      {!searchLoading && !searchError && searchCards.length === 0 && (
                        <div className="px-3 py-4 text-sm text-gray-500">没有找到匹配的本地媒体</div>
                      )}
                      {!searchLoading && !searchError && searchCards.length > 0 && (
                        <div className="space-y-1">
                          {searchCards.map((card) => (
                            <Link
                              key={card.key}
                              to={seriesCardLink(card)}
                              onClick={() => setSearchFocused(false)}
                              className="flex items-center gap-3 rounded-xl px-2.5 py-2 transition-colors hover:bg-gray-50"
                            >
                              <div className="h-14 w-10 shrink-0 overflow-hidden rounded-lg bg-gray-100">
                                {card.rep.poster_url ? (
                                  <img
                                    src={imageURL(card.rep.poster_url)}
                                    alt={card.rep.title}
                                    className="h-full w-full object-cover"
                                  />
                                ) : (
                                  <div className="flex h-full w-full items-center justify-center text-gray-400">
                                    <LibraryIcon size={16} />
                                  </div>
                                )}
                              </div>
                              <div className="min-w-0 flex-1">
                                <div className="truncate text-sm font-semibold text-gray-900">
                                  {card.rep.title || card.rep.original_name || '未命名媒体'}
                                </div>
                                <div className="mt-1 flex items-center gap-2 text-[11px] text-gray-500">
                                  {card.rep.year ? <span>{card.rep.year}</span> : null}
                                  <span>{card.count > 1 ? `${card.count} 集/条目` : '单条媒体'}</span>
                                  {card.rep.width ? <span>{card.rep.width}x{card.rep.height}</span> : null}
                                </div>
                              </div>
                            </Link>
                          ))}
                        </div>
                      )}
                    </div>
                    <Link
                      to={`/search?q=${encodeURIComponent(searchQuery.trim())}`}
                      onClick={() => setSearchFocused(false)}
                      className="flex items-center justify-between border-t border-gray-100 px-4 py-3 text-sm font-semibold text-brand-600 hover:bg-brand-50/60"
                    >
                      <span>查看全部搜索结果</span>
                      <span className="text-xs text-gray-500">
                        {searchTotal > 0 ? `${searchTotal} 个条目` : 'Enter'}
                      </span>
                    </Link>
                  </motion.div>
                )}
              </AnimatePresence>
            </form>
          </div>

          {/* Header Right Area: Actions, Notifications, Profile Dropdown */}
          <div className="flex items-center gap-4 shrink-0">
            {/* Quick search button shown ONLY on Mobile screens */}
            <Link 
              to="/search" 
              className="rounded-xl border border-gray-200 p-2.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 sm:hidden"
            >
              <Search size={18} />
            </Link>

            {/* Quick Discover shortcut button */}
            {can('can_view_discover') && (
              <Link
                to="/discover"
                className="hidden md:flex items-center gap-2 rounded-xl border border-gray-200 px-4 py-2.5 text-xs font-bold text-gray-500 hover:bg-gray-50 hover:text-gray-900 transition-all"
              >
                <Sparkles size={14} className="text-brand-500" />
                <span>发现新片</span>
              </Link>
            )}

            {/* Notification alert bubble */}
            {isAdmin && (
              <Link
                to="/notify-channels"
                title="通知配置"
                aria-label="打开通知配置"
                className="relative rounded-xl border border-gray-200 p-2.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 transition-all"
              >
                <Bell size={18} />
                <span className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-brand-500 ring-2 ring-white animate-pulse" />
              </Link>
            )}

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
                  <p className="text-[9px] text-gray-500 font-bold uppercase tracking-wider mt-0.5 leading-none">
                    {activeProfile ? `Profile: ${activeProfile.name}` : user?.role}
                  </p>
                </div>
                <ChevronDown size={14} className="text-gray-500" />
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
                      <div className="px-3 py-2">
                        <p className="mb-2 text-[10px] font-bold uppercase tracking-wider text-gray-500">
                          当前观影 Profile
                        </p>
                        <div className="space-y-1">
                          <button
                            onClick={() => {
                              setActiveProfile(null)
                              setIsProfileOpen(false)
                            }}
                            className={clsx(
                              'flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors',
                              !activeProfileId ? 'bg-gray-950 text-white' : 'text-gray-600 hover:bg-gray-50',
                            )}
                          >
                            <span>账号默认</span>
                            <span>{!activeProfileId ? '使用中' : ''}</span>
                          </button>
                          {profiles.map((profile) => (
                            <button
                              key={profile.id}
                              onClick={() => handleProfileSwitch(profile)}
                              className={clsx(
                                'flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors',
                                activeProfileId === profile.id ? 'bg-gray-950 text-white' : 'text-gray-600 hover:bg-gray-50',
                              )}
                            >
                              <span className="truncate">{profile.name}</span>
                              <span className="ml-2 shrink-0">{profile.allow_adult ? '成人' : '安全'}</span>
                            </button>
                          ))}
                        </div>
                      </div>
                      <Link
                        to="/play-profiles"
                        onClick={() => setIsProfileOpen(false)}
                        className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-gray-950 transition-colors"
                      >
                        <UserCog size={16} />
                        <span>管理观影 Profile</span>
                      </Link>
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
        <AppFooter className="border-t border-gray-200/50 bg-white py-5 text-center text-xs text-gray-500" />
      </div>
    </div>
  )
}

interface SidebarGroupProps {
  id: string;
  icon: React.ReactNode;
  label: string;
  children: React.ReactNode;
  collapsed?: boolean;
  open?: boolean;
  active?: boolean;
  onToggle: (id: string) => void;
}

function SidebarGroup({ id, icon, label, children, collapsed, open, active, onToggle }: SidebarGroupProps) {
  return (
    <div className="space-y-1">
      <button
        type="button"
        onClick={() => onToggle(id)}
        className={clsx(
          'group relative flex w-full items-center gap-3.5 rounded-xl px-4 py-3 text-sm font-bold transition-all duration-300',
          active ? 'bg-gray-950 text-white shadow-sm' : 'text-gray-500 hover:bg-gray-50 hover:text-gray-900',
          collapsed && 'justify-center px-0',
        )}
      >
        <span className={clsx('flex h-5 w-5 shrink-0 items-center justify-center', active ? 'text-[#c9954a]' : 'text-gray-500 group-hover:text-gray-700')}>
          {icon}
        </span>
        {!collapsed && (
          <>
            <span className="flex-1 truncate text-left">{label}</span>
            <ChevronDown
              size={14}
              className={clsx('transition-transform duration-200', open && 'rotate-180')}
            />
          </>
        )}
        {collapsed && (
          <div className="absolute left-full ml-3 rounded-xl bg-gray-900 px-2.5 py-1.5 text-xs font-semibold text-white opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
            {label}
          </div>
        )}
      </button>
      <AnimatePresence initial={false}>
        {!collapsed && open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="overflow-hidden"
          >
            <div className="space-y-1 pb-1 pl-3">
              {children}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

interface SidebarLinkProps {
  to: string;
  icon: React.ReactNode;
  label: string;
  end?: boolean;
  collapsed?: boolean;
  child?: boolean;
}

function SidebarLink({ to, icon, label, end, collapsed, child }: SidebarLinkProps) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        clsx(
          "flex items-center gap-3.5 rounded-xl px-4 py-3 text-sm font-semibold transition-all duration-300 relative group",
          child && "py-2.5 text-[13px]",
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
            isActive ? "text-[#c9954a]" : "text-gray-500 group-hover:text-gray-700"
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
            <div className="absolute left-full ml-3 px-2.5 py-1.5 rounded-xl bg-gray-900 text-white text-xs font-semibold opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto transition-opacity shadow-lg z-50 whitespace-nowrap">
              {label}
            </div>
          )}
        </>
      )}
    </NavLink>
  )
}
