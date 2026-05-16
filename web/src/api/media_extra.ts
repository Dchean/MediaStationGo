import { api } from './client'
import type { Media } from '../types'

// Auxiliary media surfaces used by the home page rails and the admin
// dashboard "library composition" card.
export const mediaExtraAPI = {
  recent: (limit = 12) =>
    api.get<Media[]>('/media/recent', { params: { limit } }).then((r) => r.data),

  stats: () =>
    api
      .get<{
        by_type: { movies: number; tv: number; anime: number; music: number; unscraped: number }
        total: number
        total_size: number
        total_seconds: number
      }>('/media/stats')
      .then((r) => r.data),
}
