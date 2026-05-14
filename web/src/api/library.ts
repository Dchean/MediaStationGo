import { api } from './client'
import type { Library, Media, ScanResult } from '../types'

export interface MediaPage {
  items: Media[]
  total: number
  page: number
  page_size: number
}

export const libraryAPI = {
  list: () => api.get<Library[]>('/libraries').then((r) => r.data),

  create: (name: string, path: string, type: string) =>
    api.post<Library>('/libraries', { name, path, type }).then((r) => r.data),

  remove: (id: string) => api.delete(`/libraries/${id}`).then((r) => r.data),

  scan: (id: string) =>
    api.post<ScanResult>(`/libraries/${id}/scan`).then((r) => r.data),

  scrape: (id: string) =>
    api.post(`/libraries/${id}/scrape`).then((r) => r.data),

  listMedia: (id: string, page = 1, pageSize = 50) =>
    api
      .get<MediaPage>(`/libraries/${id}/media`, {
        params: { page, page_size: pageSize },
      })
      .then((r) => r.data),
}

export const mediaAPI = {
  search: (q: string, limit = 50) =>
    api.get<{ items: Media[] }>('/media', { params: { q, limit } }).then((r) => r.data),

  get: (id: string) => api.get<Media>(`/media/${id}`).then((r) => r.data),
}
