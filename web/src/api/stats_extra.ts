import { api } from './client'
import type { Hardware, Library, Media } from '../types'

// statsExtraAPI exposes the admin dashboard surfaces beyond /stats.
export const statsExtraAPI = {
  overview: () =>
    api
      .get<{
        libraries: number
        media_count: number
        users_count: number
        total_size: number
        total_seconds: number
        generated_at: string
      }>('/stats/overview')
      .then((r) => r.data),

  trend: (days = 14) =>
    api
      .get<{ trend: { day: string; count: number }[]; days: number }>('/stats/trend', {
        params: { days },
      })
      .then((r) => r.data),

  topContent: (limit = 10) =>
    api
      .get<{
        items: { media: Media; play_count: number; last_played: string }[]
      }>('/stats/top-content', { params: { limit } })
      .then((r) => r.data),

  libraries: () =>
    api
      .get<{
        libraries: { library: Library; item_count: number; total_size: number }[]
      }>('/stats/libraries')
      .then((r) => r.data),

  monitor: () => api.get<Hardware>('/stats/monitor').then((r) => r.data),
}
