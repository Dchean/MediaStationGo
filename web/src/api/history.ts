import { api } from './client'
import type { HistoryItem, HistoryStats, Media } from '../types'

// historyAPI wraps /watch-history and /history. The two share storage on
// the backend; we treat /watch-history as the rich admin/dashboard
// surface and /history as the legacy resume-position write.
export const historyAPI = {
  list: (limit = 50) =>
    api
      .get<HistoryItem[]>('/watch-history', { params: { limit } })
      .then((r) => r.data),

  stats: () => api.get<HistoryStats>('/watch-history/stats').then((r) => r.data),

  continueWatching: (limit = 10) =>
    api
      .get<{ history: HistoryItem; media: Media }[]>('/watch-history/continue', {
        params: { limit },
      })
      .then((r) => r.data),

  clear: (mediaID?: string) =>
    api
      .delete('/watch-history', {
        params: mediaID ? { media_id: mediaID } : {},
      })
      .then((r) => r.data),

  remove: (id: string) => api.delete(`/watch-history/${id}`).then((r) => r.data),
}
