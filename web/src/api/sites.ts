import { api } from './client'

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

export const sitesAPI = {
  list: () => api.get<{ items: Site[] }>('/sites').then((r) => r.data.items),

  get: (id: string) => api.get<Site>(`/sites/${id}`).then((r) => r.data),

  create: (input: CreateSiteInput) =>
    api.post<Site>('/sites', input).then((r) => r.data),

  update: (id: string, patch: Partial<CreateSiteInput>) =>
    api.put<Site>(`/sites/${id}`, patch).then((r) => r.data),

  remove: (id: string) => api.delete(`/sites/${id}`).then((r) => r.data),

  test: (id: string) =>
    api
      .post<{ success: boolean; message: string }>(`/sites/${id}/test`)
      .then((r) => r.data),

  search: (keyword: string) =>
    api
      .get<{ items: SiteSearchResult[]; total: number }>('/sites/search', {
        params: { keyword },
      })
      .then((r) => r.data),
}
