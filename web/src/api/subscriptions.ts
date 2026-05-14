import { api } from './client'
import type { Subscription } from '../types'

export const subscriptionsAPI = {
  list: () =>
    api.get<{ items: Subscription[] }>('/subscriptions').then((r) => r.data.items),

  create: (input: { name: string; feed_url: string; filter?: string; enabled?: boolean }) =>
    api.post<Subscription>('/subscriptions', input).then((r) => r.data),

  remove: (id: string) => api.delete(`/subscriptions/${id}`).then((r) => r.data),

  runNow: (id: string) =>
    api.post<{ queued: number }>(`/subscriptions/${id}/run`).then((r) => r.data),
}
