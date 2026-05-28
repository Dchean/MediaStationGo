import { api } from './client'
import type { Subscription } from '../types'

export const subscriptionsAPI = {
  list: () =>
    api.get<{ items: Subscription[] }>('/subscriptions').then((r) => r.data.items),

  create: (input: {
    name: string
    feed_url: string
    filter?: string
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
    enabled?: boolean
  }) =>
    api.post<Subscription>('/subscriptions', input).then((r) => r.data),

  update: (id: string, input: Partial<Subscription>) =>
    api.put(`/subscriptions/${id}`, input).then((r) => r.data),

  remove: (id: string) => api.delete(`/subscriptions/${id}`).then((r) => r.data),

  runNow: (id: string) =>
    api.post<{ queued: number }>(`/subscriptions/${id}/run`).then((r) => r.data),
}
