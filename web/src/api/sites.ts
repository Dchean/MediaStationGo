import { api } from './client'

// ─── TypeScript interfaces ──────────────────────────────────────────────

export interface Site {
  id: string
  name: string
  base_url: string
  site_type: string
  auth_type: string
  cookie?: string
  api_key?: string
  auth_header?: string
  user_agent?: string
  rss_url?: string
  timeout: number
  priority: number
  use_proxy: boolean
  enabled: boolean
  login_status: string
  downloader?: string
  created_at: string
  updated_at: string
}

export interface SiteSearchResult {
  site_name: string
  site_id: string
  title: string
  torrent_url: string
  download_url: string
  size: number
  seeders: number
  leechers: number
  free: boolean
}

export interface CreateSiteInput {
  name: string
  base_url: string
  site_type?: string
  auth_type?: string
  cookie?: string
  api_key?: string
  auth_header?: string
  user_agent?: string
  rss_url?: string
  timeout?: number
  priority?: number
  use_proxy?: boolean
  enabled?: boolean
  downloader?: string
}

// ─── API client ─────────────────────────────────────────────────────────

export const sitesAPI = {
  // List all sites
  list: () => api.get('/sites').then((r) => r.data),

  // Get single site with decrypted fields
  get: (id: string | number) => api.get(`/sites/${id}`).then((r) => r.data),

  // Create a new site
  create: (data: Record<string, unknown>) =>
    api.post('/sites', data).then((r) => r.data),

  // Update existing site
  update: (id: string | number, data: Record<string, unknown>) =>
    api.put(`/sites/${id}`, data).then((r) => r.data),

  // Delete a site
  remove: (id: string | number) =>
    api.delete(`/sites/${id}`).then((r) => r.data),

  // Test site connectivity
  test: (id: string | number) =>
    api.post(`/sites/${id}/test`).then((r) => r.data),

  // Get supported site types
  types: () => api.get('/sites/types').then((r) => r.data),

  // Get supported auth types
  authTypes: () => api.get('/sites/auth-types').then((r) => r.data),

  // Search across all sites
  search: (keyword: string) =>
    api
      .get('/sites/search', { params: { keyword } })
      .then((r) => r.data),
}
