import { api } from './client'
import type { Media } from '../types'

export const recycleAPI = {
  list: () => api.get<{ items: Media[] }>('/recycle').then((r) => r.data.items),

  softDelete: (id: string) => api.delete(`/media/${id}`).then((r) => r.data),

  restore: (id: string) => api.post(`/media/${id}/restore`).then((r) => r.data),

  restoreMany: (ids: string[]) =>
    api.post<{ applied: number; errors?: string[] }>('/recycle/restore', { media_ids: ids }).then((r) => r.data),

  purge: (id: string) => api.delete(`/media/${id}/purge`).then((r) => r.data),

  purgeMany: (ids: string[]) =>
    api.post<{ applied: number; errors?: string[] }>('/recycle/purge', { media_ids: ids }).then((r) => r.data),

  exportNFO: (id: string) =>
    api.post<{ path: string }>(`/media/${id}/nfo`).then((r) => r.data),

  exportLibraryNFO: (id: string) =>
    api.post<{ written: number }>(`/libraries/${id}/nfo`).then((r) => r.data),
}
