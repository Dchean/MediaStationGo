// Domain types mirrored from the Go backend (internal/model).

export interface User {
  id: string
  username: string
  role: 'admin' | 'user'
  tier: 'free' | 'plus'
  nickname?: string
  email?: string
  avatar_url?: string
  force_password_reset: boolean
  is_active: boolean
  last_login_at?: string
  created_at: string
  updated_at: string
}

// Token pair
export interface TokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

// User permission (19 granular permissions)
export interface UserPermission {
  id: string
  user_id: string
  // Default enabled (6)
  can_view_dashboard: boolean
  can_play_media: boolean
  can_cast: boolean
  can_external_player: boolean
  can_favorite: boolean
  can_view_history: boolean
  // Default disabled (13)
  can_edit_media: boolean
  can_rescrape: boolean
  can_use_ai: boolean
  can_capture_frames: boolean
  can_manage_downloads: boolean
  can_view_discover: boolean
  can_manage_subscriptions: boolean
  can_manage_sites: boolean
  can_use_ai_assistant: boolean
  can_manage_users: boolean
  can_manage_files: boolean
  can_manage_strm: boolean
  can_access_settings: boolean
  created_at: string
  updated_at: string
}

// API config
export interface ApiConfig {
  id: string
  provider: string
  api_key?: string
  base_url?: string
  extra?: string
  enabled: boolean
  description?: string
  last_tested_at?: string
  test_result?: string
  updated_at: string
}

// API provider
export interface ApiProvider {
  id: string
  name: string
  description: string
  has_api_key: boolean
  has_base_url: boolean
}

// Refresh token response
export interface RefreshTokenResponse {
  token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

// Login response
export interface LoginResponse {
  user: User
  tokens: TokenPair
}

// Library
export interface Library {
  id: string
  name: string
  path: string
  type: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface Media {
  id: string
  library_id: string
  series_id?: string
  title: string
  original_name?: string
  path: string
  size_bytes: number
  duration_sec: number
  width: number
  height: number
  video_codec?: string
  audio_codec?: string
  container?: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  rating: number
  year: number
  season_num: number
  episode_num: number
  scrape_status: string
  tmdb_id: number
  bangumi_id: number
  languages?: string
  countries?: string
  genres?: string
  nsfw: boolean
  created_at: string
  updated_at: string
}

export interface Playlist {
  id: string
  user_id: string
  name: string
  is_public: boolean
  created_at: string
  updated_at: string
}

export interface ScanResult {
  library_id: string
  visited: number
  added: number
  updated?: number
  probed: number
  local_metadata?: number
  removed?: number
}

export interface Setting {
  key: string
  value: string
  updated_at: string
}

export interface AccessLog {
  id: string
  user_id: string
  action: string
  target: string
  ip: string
  detail: string
  created_at: string
}

export interface Subscription {
  id: string
  user_id: string
  name: string
  feed_url: string
  filter: string
  media_type?: string
  media_category?: string
  save_path?: string
  search_mode?: string
  imdb_id?: string
  source?: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  resolution?: string
  quality?: string
  effects?: string
  release_groups?: string
  exclude_words?: string
  wash_enabled?: boolean
  wash_priority?: string
  priority?: number
  enabled: boolean
  last_run_at?: string
  created_at: string
  updated_at: string
}

export interface DownloadTask {
  id: string
  source: string
  title: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  save_path: string
  status: string
  progress: number
  state?: string
  dlspeed?: number
  upspeed?: number
  num_seeds?: number
  num_leechs?: number
  size?: number
  downloaded?: number
  created_at: string
  updated_at: string
}

export interface QBitTorrent {
  hash: string
  name: string
  title: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  state: string
  progress: number
  dlspeed: number
  upspeed: number
  num_seeds: number
  num_leechs: number
  size: number
  downloaded: number
  save_path: string
}

export interface Hardware {
  cpu_percent: number
  memory_used: number
  memory_total: number
  disk_used: number
  disk_total: number
  go_version: string
  goroutines: number
}

export interface StatsSnapshot {
  libraries: number
  media_count: number
  users_count: number
  total_size_bytes: number
  total_seconds: number
  recently_added: Media[]
  hardware: Hardware
  generated_at: string
}

// SSE Event types
export interface SSEEvent {
  type: string
  payload: unknown
}

// Permission check result
export interface PermissionCheckResult {
  permissions: Record<string, boolean>
  role: string
  tier: string
  is_super: boolean
}

// Download Client
export interface DownloadClient {
  id: string
  name: string
  type: 'qbittorrent' | 'transmission' | 'aria2'
  host: string
  username: string
  is_default: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

// Download Client type info
export interface DownloadClientTypeInfo {
  type: string
  name: string
  description: string
}

// Notify Channel
export interface NotifyChannel {
  id: string
  name: string
  type: 'telegram' | 'wechat' | 'bark' | 'webhook' | 'email'
  enabled: boolean
  events: string
  config: Record<string, string>
  created_at: string
  updated_at: string
}

// Notify Provider type info
export interface NotifyProviderInfo {
  type: string
  name: string
  description: string
}

// ─── Play Profiles ──────────────────────────────────────────────────────────

export interface PlayProfile {
  id: string
  user_id: string
  name: string
  is_default: boolean
  content_rating_limit?: string
  allow_adult: boolean
  require_pin: boolean
  preferred_subtitle_lang?: string
  preferred_audio_lang?: string
  autoplay_next: boolean
  skip_intro: boolean
  allowed_library_ids: string[]
  total_watch_time: number
  last_active_at?: string
  created_at: string
  updated_at: string
}

// Scheduler task config
export interface SchedulerTaskConfig {
  id: string
  name: string
  description: string
  enabled: boolean
  interval: number
  last_run_at?: string
  next_run_at?: string
}

// Scheduler status
export interface SchedulerStatus {
  running: boolean
  started_at?: string
  task_count: number
  tasks: SchedulerTaskConfig[]
}

// Site configuration
export interface Site {
  id: string
  name: string
  type: string          // nexusphp / gazelle / unit3d / mteam / discuz / custom_rss
  url: string
  auth_type: string     // cookie / api_key / auth_header
  cookie?: string       // decrypted only in detail view
  api_key?: string      // decrypted only in detail view
  auth_header?: string  // decrypted only in detail view

  // 高级设置
  user_agent?: string         // 自定义 User-Agent
  rss_url?: string            // RSS 订阅地址
  timeout?: number            // 请求超时(秒), 默认 15
  priority?: number           // 优先级, 越小越优先, 默认 50
  use_proxy?: boolean         // 是否使用代理
  rate_limit?: boolean        // 是否限制访问频率
  browser_emulation?: boolean // 浏览器仿真(防爬)

  // 状态与统计
  login_status?: string       // unknown / ok / fail
  upload_bytes?: number       // 上传字节统计
  download_bytes?: number     // 下载字节统计

  // 关联下载器
  downloader?: string         // qbittorrent / transmission / aria2

  enabled: boolean
  is_default: boolean
  extra?: string
  last_error?: string
  last_check_at?: string
  created_at: string
  updated_at: string
}

// Site type info
export interface SiteTypeInfo {
  value: string
  name: string
  description: string
}

// Auth type info
export interface AuthTypeInfo {
  value: string
  name: string
  description: string
}

// ─── History helpers ────────────────────────────────────────────────────────

export interface HistoryItem {
  id: string
  user_id: string
  media_id: string
  position_ms: number
  duration_ms: number
  watched_at: string
  completed: boolean
  media?: Media
}

export interface HistoryStats {
  total: number
  completed: number
  watched_ms: number
  watched_hours: number
  last_watched?: string
}

// ─── Discover ───────────────────────────────────────────────────────────────

export interface DiscoverSection {
  key: string
  label: string
}

export interface DiscoverItem {
  TMDbID?: number
  Title?: string
  Overview?: string
  Rating?: number
  Year?: number
  PosterURL?: string
  BackdropURL?: string
  // Match struct (Go) is exported with capitalised JSON keys; the API
  // returns lower-cased aliases below for convenience.
  tmdb_id?: number
  title?: string
  overview?: string
  rating?: number
  year?: number
  poster_url?: string
  backdrop_url?: string
}
