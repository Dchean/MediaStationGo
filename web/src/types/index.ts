// Domain types mirrored from the Go backend (internal/model).

export interface User {
  id: string
  username: string
  role: 'admin' | 'user'
  email?: string
  avatar_url?: string
  force_password_reset: boolean
  last_login_at?: string
  created_at: string
  updated_at: string
}

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
  probed: number
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
  enabled: boolean
  last_run_at?: string
  created_at: string
  updated_at: string
}

export interface DownloadTask {
  id: string
  user_id: string
  source: string
  url: string
  save_path: string
  status: string
  progress: number
  created_at: string
  updated_at: string
}

export interface QBitTorrent {
  hash: string
  name: string
  state: string
  progress: number
  dlspeed: number
  upspeed: number
  num_seeds: number
  num_leechs: number
  size: number
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
